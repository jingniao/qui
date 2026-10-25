// Package spool persists encrypted, immutable telemetry batches until acknowledged.
// It uses its own SQLite schema; panel migrations must never run against this DB.
package spool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/access"
	"strings"
)

const DefaultLimit = int64(100 << 20)

// The spool counts only the two live queues. The final-usage, runtime-source and
// budget-retirement tables of an older store are ignored, never read or written.
const queuedBytesSQL = "(SELECT coalesce(sum(length(payload)),0) FROM batches)+(SELECT coalesce(sum(length(payload)),0) FROM traffic_batches)"
const queuedRowsSQL = "(SELECT count(*) FROM batches)+(SELECT count(*) FROM traffic_batches)"

var ErrFull = errors.New("telemetry spool capacity exhausted; blocked batches are retained")

// Invalid includes authentication failure (possibly the wrong spool key), typed
// payload failure and metadata mismatch. It does not authorize dropping a row.
var ErrQueuedBatchInvalid = errors.New("queued batch cannot be authenticated or matched to its metadata")

func invalidQueuedBatch(err error) error { return errors.Join(ErrQueuedBatchInvalid, err) }

// AES-GCM payload includes a nonce and authentication tag. Keep spare bounded
// envelope room while the plaintext remains subject to its protocol limit.
const maxQueuedBatchBytes = access.MaxBatchBytes + 1024

type Spool struct {
	database *sql.DB
	vault    *vault.Vault
	nodeID   string
	limit    int64
	path     string
	bound    *boundStore
}
type Result struct {
	BatchIDs              []string
	DroppedAllowedRollups int64
}
type Stats struct {
	DatabaseBytes         int64
	DatabaseLimit         int64
	DiskBytes             int64
	DiskLimit             int64
	Batches               int64
	Rollups               int64
	Bytes                 int64
	DroppedAllowedRollups int64
	// TrafficQuarantined counts the traffic batches a deterministic rejection
	// moved behind the delivery head for a cool-down. They are retained, not
	// deleted, so they stay inside Batches and Bytes and are never evicted.
	TrafficQuarantined int64
}

func Open(ctx context.Context, directory, nodeID string, limit int64) (*Spool, error) {
	if nodeID == "" || len(nodeID) > 128 {
		return nil, errors.New("spool requires node identity")
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 512 || limit > 1<<30 {
		return nil, errors.New("invalid spool capacity")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(directory, "spool.key")
	if _, err := os.Stat(filepath.Join(directory, "queue.db")); err == nil {
		if _, keyErr := os.Stat(keyPath); errors.Is(keyErr, os.ErrNotExist) {
			return nil, errors.New("spool encryption key missing; restore the original key")
		}
	}
	key, err := vault.Open(keyPath)
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(filepath.Join(directory, "queue.db"))
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{}
	for _, pragma := range []string{"busy_timeout(5000)", "synchronous(FULL)", "wal_autocheckpoint(256)"} {
		query.Add("_pragma", pragma)
	}
	uri.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	closeError := func(err error) (*Spool, error) { database.Close(); return nil, err }
	// Set the rollback journal mode before enabling incremental auto-vacuum.
	// Enabling auto-vacuum first leaves a rollback journal that a second
	// concurrent handle cannot reopen on Windows (SQLITE_IOERR_DELETE).
	if err := configureDiskCapacity(ctx, database, limit); err != nil {
		return closeError(err)
	}
	if _, err := database.ExecContext(ctx, "PRAGMA auto_vacuum=INCREMENTAL"); err != nil {
		return closeError(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS spool_metadata(id INTEGER PRIMARY KEY CHECK(id=1),schema_version INTEGER NOT NULL,node_id TEXT NOT NULL,dropped_allowed INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS batches(id TEXT PRIMARY KEY,blocked INTEGER NOT NULL CHECK(blocked IN (0,1)),records INTEGER NOT NULL,payload BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS batch_eviction ON batches(blocked);`); err != nil {
		return closeError(err)
	}
	if _, err := database.ExecContext(ctx, "INSERT OR IGNORE INTO spool_metadata(id,schema_version,node_id) VALUES(1,1,?)", nodeID); err != nil {
		return closeError(err)
	}
	var owner string
	var version int
	if err := database.QueryRowContext(ctx, "SELECT node_id,schema_version FROM spool_metadata WHERE id=1").Scan(&owner, &version); err != nil {
		return closeError(err)
	}
	if owner != nodeID || version != 1 {
		return closeError(errors.New("spool identity or schema mismatch"))
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS traffic_batches(id TEXT PRIMARY KEY,payload BLOB NOT NULL,quarantined INTEGER NOT NULL DEFAULT 0 CHECK(quarantined IN (0,1)),rejections INTEGER NOT NULL DEFAULT 0 CHECK(rejections>=0),quarantined_at INTEGER NOT NULL DEFAULT 0 CHECK(quarantined_at>=0));CREATE TABLE IF NOT EXISTS traffic_cursors(epoch TEXT NOT NULL,credential_id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(epoch,credential_id));`); err != nil {
		return closeError(err)
	}
	// A store written before traffic quarantine existed gains its columns in
	// place. traffic_batches is outside the authenticated v1 checkpoint, so the
	// stored digest still verifies after the upgrade.
	if err := attachTrafficQuarantine(ctx, database); err != nil {
		return closeError(err)
	}
	// Quarantine is a cool-down owned by this process: every start, including an
	// upgrade, retries the held-back batches right away.
	if err := releaseTrafficQuarantine(ctx, database); err != nil {
		return closeError(err)
	}
	// budget_state is empty: the agent no longer keeps a local budget ledger. It
	// stays in the schema because the authenticated v1 checkpoint hashes its name
	// and rows, so an existing store keeps verifying. See bound_store.go.
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS budget_state(id TEXT PRIMARY KEY,user_id TEXT NOT NULL,payload BLOB NOT NULL);CREATE UNIQUE INDEX IF NOT EXISTS one_user_budget ON budget_state(user_id);`); err != nil {
		return closeError(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return closeError(err)
	}
	result := &Spool{database: database, vault: key, nodeID: nodeID, limit: limit, path: path}
	if err := result.attachBoundStore(ctx); err != nil {
		return closeError(err)
	}
	return result, nil
}

func (s *Spool) Close() error { return s.database.Close() }

func (s *Spool) Enqueue(ctx context.Context, records []access.Rollup) (Result, error) {
	result := Result{BatchIDs: []string{}}
	if len(records) == 0 {
		return result, nil
	}
	allowed, blocked := []access.Rollup{}, []access.Rollup{}
	for _, record := range records {
		if record.NodeID != s.nodeID {
			return result, errors.New("spool record node mismatch")
		}
		if err := record.Normalize(); err != nil {
			return result, err
		}
		if record.Allowed {
			allowed = append(allowed, record)
		} else {
			blocked = append(blocked, record)
		}
	}
	type queued struct {
		id      string
		blocked bool
		count   int
		data    []byte
	}
	queue := []queued{}
	var added int64
	for priority, items := range [][]access.Rollup{allowed, blocked} {
		for offset := 0; offset < len(items); offset += access.MaxBatchRecords {
			end := min(offset+access.MaxBatchRecords, len(items))
			batchID, err := id.New()
			if err != nil {
				return result, err
			}
			batch := access.Batch{ID: batchID, NodeID: s.nodeID, Records: items[offset:end]}
			if err := batch.Validate(); err != nil {
				return result, err
			}
			data, err := json.Marshal(batch)
			if err != nil {
				return result, err
			}
			if len(data) > access.MaxBatchBytes {
				return result, errors.New("access batch exceeds protocol payload budget")
			}
			sealed, err := s.vault.Seal(data)
			if err != nil {
				return result, err
			}
			added += int64(len(sealed))
			if added > s.limit {
				return result, ErrFull
			}
			queue = append(queue, queued{id: batchID, blocked: priority == 1, count: len(batch.Records), data: sealed})
		}
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var used int64
	if err := tx.QueryRowContext(ctx, "SELECT "+queuedBytesSQL).Scan(&used); err != nil {
		return result, err
	}
	for used+added > s.limit {
		size, count, err := s.evictAllowedTx(ctx, tx)
		if errors.Is(err, sql.ErrNoRows) {
			return Result{}, ErrFull
		}
		if err != nil {
			return Result{}, err
		}
		if used < size || math.MaxInt64-result.DroppedAllowedRollups < count {
			return Result{}, ErrQueuedBatchInvalid
		}
		used -= size
		result.DroppedAllowedRollups += count
	}
	for _, batch := range queue {
		if _, err := tx.ExecContext(ctx, "INSERT INTO batches(id,blocked,records,payload) VALUES(?,?,?,?)", batch.id, batch.blocked, batch.count, batch.data); err != nil {
			return Result{}, err
		}
		result.BatchIDs = append(result.BatchIDs, batch.id)
	}
	if err := addEvictionCount(ctx, tx, result.DroppedAllowedRollups); err != nil {
		return Result{}, err
	}
	if err := s.checkDiskCapacity(); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (s *Spool) Pending(ctx context.Context) (*access.Batch, error) {
	var batchID string
	var sealed []byte
	var blocked bool
	var records int
	err := s.database.QueryRowContext(ctx, "SELECT id,blocked,records,CASE WHEN typeof(payload)='blob' AND length(payload) BETWEEN 1 AND ? THEN payload ELSE NULL END FROM batches ORDER BY rowid LIMIT 1", maxQueuedBatchBytes).Scan(&batchID, &blocked, &records, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(sealed) == 0 || records < 1 || records > access.MaxBatchRecords {
		return nil, invalidQueuedBatch(errors.New("access queue payload or metadata exceeds bounds; preserve original batch"))
	}
	data, err := s.vault.Open(sealed)
	if err != nil {
		return nil, invalidQueuedBatch(err)
	}
	batch, err := access.DecodeBatch(data)
	if err != nil {
		return nil, invalidQueuedBatch(err)
	}
	if batch.ID != batchID || batch.NodeID != s.nodeID {
		return nil, invalidQueuedBatch(errors.New("spool payload identity mismatch"))
	}
	if err := batch.Validate(); err != nil {
		return nil, invalidQueuedBatch(err)
	}
	if len(batch.Records) != records {
		return nil, invalidQueuedBatch(errors.New("access queue count does not match authenticated payload"))
	}
	for _, record := range batch.Records {
		if record.Allowed == blocked {
			return nil, invalidQueuedBatch(errors.New("access queue priority does not match authenticated payload"))
		}
	}
	return &batch, nil
}

// Ack is idempotent. An eviction may already have removed an allowed batch;
// acknowledging it is safe and never acknowledges a different batch.
func (s *Spool) Ack(ctx context.Context, batchID string, sent ...[]byte) error {
	if batchID == "" || len(batchID) > 128 {
		return errors.New("invalid spool acknowledgement")
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.validateQueuedAckTx(ctx, tx, "access", batchID, sent); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM batches WHERE id=?", batchID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = s.database.ExecContext(ctx, "PRAGMA incremental_vacuum(128)")
	return err
}

func (s *Spool) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	diskBytes, err := s.diskUsage()
	if err != nil {
		return stats, err
	}
	stats.DiskBytes = diskBytes
	stats.DiskLimit = physicalLimit(s.limit)
	var pageSize, pageCount, maxPages int64
	if err := s.database.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return stats, err
	}
	if err := s.database.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return stats, err
	}
	if err := s.database.QueryRowContext(ctx, "PRAGMA max_page_count").Scan(&maxPages); err != nil {
		return stats, err
	}
	stats.DatabaseBytes = pageSize * pageCount
	stats.DatabaseLimit = pageSize * maxPages
	if err := s.database.QueryRowContext(ctx, "SELECT "+queuedRowsSQL+",(SELECT coalesce(sum(records),0) FROM batches),"+queuedBytesSQL).Scan(&stats.Batches, &stats.Rollups, &stats.Bytes); err != nil {
		return stats, err
	}
	if err := s.database.QueryRowContext(ctx, "SELECT count(*) FROM traffic_batches WHERE quarantined=1").Scan(&stats.TrafficQuarantined); err != nil {
		return stats, err
	}
	err = s.database.QueryRowContext(ctx, "SELECT dropped_allowed FROM spool_metadata WHERE id=1").Scan(&stats.DroppedAllowedRollups)
	return stats, err
}
