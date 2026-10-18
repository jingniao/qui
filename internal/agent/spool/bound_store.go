package spool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"sync"
)

var ErrStoreIntegrity = errors.New("spool identity or accounting checkpoint unavailable; restore the matching store and witness")

type storeHead struct {
	Version    int    `json:"version"`
	NodeID     string `json:"node_id"`
	StoreID    string `json:"store_id"`
	Generation uint64 `json:"generation"`
	Digest     string `json:"digest"`
	Budgeted   bool   `json:"budgeted"`
	Witness    string `json:"witness"`
}
type boundStore struct {
	head storeHead
	file os.FileInfo
	mu   *sync.Mutex
}

var boundLocks sync.Map

func storeLock(path string) *sync.Mutex {
	actual, _ := boundLocks.LoadOrStore(path, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// The witness is outside the queue directory, under the node identity. Losing
// or replacing only the queue cannot silently construct a fresh budget ledger.
func OpenBound(ctx context.Context, directory, nodeID string, limit int64, witnessPath string, expected []desiredstate.User) (*Spool, error) {
	directory, dirErr := filepath.Abs(directory)
	if dirErr != nil {
		return nil, dirErr
	}
	witnessPath, err := filepath.Abs(witnessPath)
	if err != nil {
		return nil, err
	}
	relative, relErr := filepath.Rel(directory, witnessPath)
	if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, ErrStoreIntegrity
	}
	if _, err := os.Lstat(witnessPath); err == nil {
		for _, name := range []string{"queue.db", "spool.key"} {
			info, err := os.Lstat(filepath.Join(directory, name))
			if err != nil || !info.Mode().IsRegular() {
				return nil, ErrStoreIntegrity
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrStoreIntegrity
	}
	// A previous successful finite snapshot proves that its ledger must exist,
	// including migration from the older queue format without a witness.
	if len(expected) > 0 {
		info, err := os.Lstat(filepath.Join(directory, "queue.db"))
		if err != nil || !info.Mode().IsRegular() {
			return nil, ErrStoreIntegrity
		}
	}
	s, err := Open(ctx, directory, nodeID, limit)
	if err != nil {
		if _, witnessErr := os.Lstat(witnessPath); witnessErr == nil || len(expected) > 0 {
			return nil, errors.Join(ErrStoreIntegrity, err)
		}
		return nil, err
	}
	fail := func(err error) (*Spool, error) { s.Close(); return nil, err }
	if s.bound != nil {
		if s.bound.head.Witness != witnessPath {
			return fail(ErrStoreIntegrity)
		}
		if err := s.verifyExpected(ctx); err != nil {
			return fail(err)
		}
		return s, nil
	}
	mu := storeLock(witnessPath)
	mu.Lock()
	defer mu.Unlock()
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS spool_store(id INTEGER PRIMARY KEY CHECK(id=1),payload BLOB NOT NULL)"); err != nil {
		return fail(err)
	}
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM spool_store").Scan(&exists); err != nil {
		return fail(err)
	}
	if exists != 0 {
		return fail(ErrStoreIntegrity)
	}
	digest, budgeted, err := criticalDigest(ctx, tx)
	if err != nil {
		return fail(err)
	}
	storeID, err := id.New()
	if err != nil {
		return fail(err)
	}
	head := storeHead{Version: 1, NodeID: nodeID, StoreID: storeID, Digest: digest, Budgeted: budgeted, Witness: witnessPath}
	info, err := os.Lstat(s.path)
	if err != nil || !info.Mode().IsRegular() {
		return fail(ErrStoreIntegrity)
	}
	s.bound = &boundStore{head: head, file: info, mu: mu}
	if _, err := os.Lstat(witnessPath); err == nil {
		return fail(ErrStoreIntegrity)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(ErrStoreIntegrity)
	}
	if err := s.writeHead(ctx, tx, head); err != nil {
		return fail(err)
	}
	if err := tx.Commit(); err != nil {
		return fail(err)
	}
	return s, nil
}

func (s *Spool) attachBoundStore(ctx context.Context) error {
	var exists int
	if err := s.database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='spool_store'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	var payload []byte
	if err := s.database.QueryRowContext(ctx, "SELECT payload FROM spool_store WHERE id=1").Scan(&payload); err != nil {
		return ErrStoreIntegrity
	}
	head, err := s.decodeHead(payload)
	if err != nil {
		return err
	}
	info, err := os.Lstat(s.path)
	if err != nil || !info.Mode().IsRegular() {
		return ErrStoreIntegrity
	}
	s.bound = &boundStore{head: head, file: info, mu: storeLock(head.Witness)}
	s.bound.mu.Lock()
	defer s.bound.mu.Unlock()
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.verifyBound(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Spool) decodeHead(payload []byte) (storeHead, error) {
	var head storeHead
	if len(payload) > 8192 {
		return head, ErrStoreIntegrity
	}
	data, err := s.vault.Open(payload)
	if err != nil {
		return head, ErrStoreIntegrity
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&head); err != nil {
		return head, ErrStoreIntegrity
	}
	identifier, err := base64.RawURLEncoding.DecodeString(head.StoreID)
	if decoder.Decode(&struct{}{}) != io.EOF || head.Version != 1 || head.NodeID != s.nodeID || err != nil || len(identifier) != 32 || head.Generation > math.MaxInt64 || len(head.Digest) != 64 || !filepath.IsAbs(head.Witness) {
		return head, ErrStoreIntegrity
	}
	if _, err := hex.DecodeString(head.Digest); err != nil {
		return head, ErrStoreIntegrity
	}
	return head, nil
}
func (s *Spool) verifyBound(ctx context.Context, tx *sql.Tx) error {
	if s.bound == nil {
		return nil
	}
	info, err := os.Lstat(s.path)
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, s.bound.file) {
		return ErrStoreIntegrity
	}
	var payload []byte
	if err := tx.QueryRowContext(ctx, "SELECT payload FROM spool_store WHERE id=1").Scan(&payload); err != nil {
		return ErrStoreIntegrity
	}
	head, err := s.decodeHead(payload)
	if err != nil {
		return err
	}
	if head.StoreID != s.bound.head.StoreID || head.Witness != s.bound.head.Witness {
		return ErrStoreIntegrity
	}
	info, err = os.Lstat(head.Witness)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return ErrStoreIntegrity
	}
	witnessPayload, err := os.ReadFile(head.Witness)
	if err != nil {
		return ErrStoreIntegrity
	}
	witness, err := s.decodeHead(witnessPayload)
	if err != nil || witness != head {
		return ErrStoreIntegrity
	}
	digest, budgeted, err := criticalDigest(ctx, tx)
	if err != nil || digest != head.Digest || budgeted && !head.Budgeted {
		return ErrStoreIntegrity
	}
	s.bound.head = head
	return nil
}
func hashPart(h hash.Hash, data []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(data)))
	h.Write(length[:])
	h.Write(data)
}

// Bound each encrypted cell before the SQLite driver copies it into Go. This
// leaves every accepted byte and v1 hash order unchanged; invalid cells fail
// closed rather than contributing an empty/truncated value to the digest.
const criticalPayloadSQL = "CASE WHEN typeof(payload)='blob' AND length(payload) BETWEEN 1 AND ? THEN payload ELSE NULL END"
const maxCriticalStateCipherBytes = 8192

func criticalTextSQL(column string, maximum int) string {
	// Columns and sizes are fixed below, never supplied by an API or file.
	return fmt.Sprintf("CASE WHEN typeof(%s)='text' THEN substr(%s,1,%d) ELSE NULL END", column, column, maximum+1)
}
func criticalIdentifiers(values ...string) bool {
	for _, value := range values {
		if len(value) == 0 || len(value) > 128 {
			return false
		}
	}
	return true
}

func criticalDigest(ctx context.Context, tx *sql.Tx) (string, bool, error) {
	h := sha256.New()
	h.Write([]byte("proxy-agent-spool-critical-v1"))
	budgeted := false
	for _, table := range []string{"budget_state", "traffic_cursors"} {
		hashPart(h, []byte(table))
		query := "SELECT " + criticalTextSQL("id", 128) + "," + criticalTextSQL("user_id", 128) + "," + criticalPayloadSQL + " FROM budget_state ORDER BY id"
		if table == "traffic_cursors" {
			query = "SELECT " + criticalTextSQL("epoch", 128) + "," + criticalTextSQL("credential_id", 128) + "," + criticalPayloadSQL + " FROM traffic_cursors ORDER BY epoch,credential_id"
		}
		rows, err := tx.QueryContext(ctx, query, maxCriticalStateCipherBytes)
		if err != nil {
			return "", false, err
		}
		for rows.Next() {
			var a, b string
			var payload []byte
			if err := rows.Scan(&a, &b, &payload); err != nil {
				rows.Close()
				return "", false, err
			}
			if len(payload) == 0 || !criticalIdentifiers(a, b) {
				rows.Close()
				return "", false, ErrStoreIntegrity
			}
			hashPart(h, []byte(a))
			hashPart(h, []byte(b))
			hashPart(h, payload)
			if table == "budget_state" {
				budgeted = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", false, err
		}
	}
	// The final-usage, runtime-source and budget-retirement tables are gone. Their
	// names stay in the digest, with no rows, so a v1 checkpoint written by an
	// older agent over empty tables still verifies after an upgrade.
	for _, table := range []string{"final_usage_queue", "runtime_source_metadata", "runtime_sources", "budget_retirements"} {
		hashPart(h, []byte(table))
	}
	return hex.EncodeToString(h.Sum(nil)), budgeted, nil
}

// Save the independent checkpoint first. A failure or crash before SQL commit
// leaves a mismatch, which is safer than treating an older ledger as current.
func (s *Spool) writeHead(ctx context.Context, tx *sql.Tx, head storeHead) error {
	data, err := json.Marshal(head)
	if err != nil {
		return err
	}
	sealed, err := s.vault.Seal(data)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO spool_store(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", sealed); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(head.Witness), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(head.Witness), ".spool-witness-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(sealed); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), head.Witness)
}
func (s *Spool) commitAccounting(ctx context.Context, tx *sql.Tx) error {
	if err := s.checkDiskCapacity(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.bound == nil {
		return tx.Commit()
	}
	digest, budgeted, err := criticalDigest(ctx, tx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	head := s.bound.head
	if head.Generation == math.MaxInt64 {
		return ErrStoreIntegrity
	}
	head.Generation++
	head.Digest = digest
	head.Budgeted = head.Budgeted || budgeted
	// Once the independent checkpoint is published, ordinary cancellation
	// cannot roll back its SQL partner. Callers begin this transaction without
	// automatic context rollback; real I/O/commit failures still retain a gap.
	if err := s.writeHead(context.WithoutCancel(ctx), tx, head); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.bound.head = head
	return nil
}
func (s *Spool) accountingLock() func() {
	if s.bound == nil {
		return func() {}
	}
	s.bound.mu.Lock()
	return s.bound.mu.Unlock
}
func (s *Spool) verifyExpected(ctx context.Context) error {
	unlock := s.accountingLock()
	defer unlock()
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return s.verifyBound(ctx, tx)
}
