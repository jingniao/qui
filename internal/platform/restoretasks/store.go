// Package restoretasks persists authenticated, secret-free local restore progress.
package restoretasks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/id"
	"sort"
	"strings"
	"time"
)

var ErrStore = errors.New("restore progress record authentication or state is invalid")
var ErrPublishedUnconfirmed = errors.New("restore progress file published but directory durability unconfirmed")

type Record struct {
	ID               string `json:"id"`
	BackupID         string `json:"backupId"`
	ArchiveSHA256    string `json:"archiveSha256"`
	Status           string `json:"status"`
	LastStage        string `json:"lastStage"`
	Sequence         int64  `json:"sequence"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	ErrorCode        string `json:"errorCode"`
	RuntimeEnteredAt string `json:"runtimeEnteredAt,omitempty"`
}
type envelope struct {
	Record json.RawMessage `json:"record"`
	MAC    string          `json:"mac"`
}
type Store struct {
	Directory string
	Key       []byte
}

// Callers serialize mutations with the panel lifetime lock. Listing is read-only.
func (s Store) Begin(ctx context.Context, backupID, archiveSHA string) (Record, error) {
	identifier, err := id.New()
	if err != nil {
		return Record{}, err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	record := Record{ID: identifier, BackupID: backupID, ArchiveSHA256: archiveSHA, Status: "active", LastStage: "preparing", Sequence: 1, CreatedAt: stamp, UpdatedAt: stamp}
	return record, s.write(ctx, record, true)
}

func (s Store) Advance(ctx context.Context, identifier, stage, status, code string) (Record, error) {
	record, err := s.read(identifier)
	if err != nil {
		return Record{}, err
	}
	if record.Status != "active" || record.Sequence >= 1<<62 {
		return Record{}, ErrStore
	}
	original := record
	rank := map[string]int{"preparing": 0, "installing": 1, "committed": 2, "cleaned": 3, "serving": 4, "stopped": 5}
	if _, known := rank[stage]; !known {
		return Record{}, ErrStore
	}
	if rank[stage] < rank[record.LastStage] || rank[stage] > rank[record.LastStage]+1 {
		return Record{}, ErrStore
	}
	if stage == record.LastStage && status == record.Status && code == record.ErrorCode {
		return record, nil
	}
	now := time.Now().UTC()
	previous, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if err != nil || now.Before(previous) {
		return Record{}, ErrStore
	}
	record.LastStage, record.Status, record.ErrorCode = stage, status, code
	record.Sequence++
	record.UpdatedAt = now.Format(time.RFC3339Nano)
	return record, s.write(ctx, record, false, original)
}

func (s Store) ReconcileInterrupted(ctx context.Context) error {
	records, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Status == "active" {
			original := record
			now := time.Now().UTC()
			previous, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
			if err != nil || now.Before(previous) || record.Sequence >= (1<<62)-1 {
				return ErrStore
			}
			record.Status = "interrupted"
			record.ErrorCode = "process_interrupted"
			record.Sequence++
			record.UpdatedAt = now.Format(time.RFC3339Nano)
			if err := s.write(ctx, record, false, original); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Store) List(ctx context.Context) ([]Record, error) {
	if len(s.Key) != 32 {
		return nil, ErrStore
	}
	info, err := os.Lstat(s.Directory)
	if os.IsNotExist(err) {
		return []Record{}, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrStore
	}
	entries, err := os.ReadDir(s.Directory)
	if err != nil {
		return nil, err
	}
	if len(entries) > 10000 {
		return nil, ErrStore
	}
	records := []Record{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(entry.Name(), ".pending-") {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			return nil, ErrStore
		}
		record, err := s.read(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt == records[j].CreatedAt {
			return records[i].ID > records[j].ID
		}
		left, _ := time.Parse(time.RFC3339Nano, records[i].CreatedAt)
		right, _ := time.Parse(time.RFC3339Nano, records[j].CreatedAt)
		return left.After(right)
	})
	return records, nil
}

func validID(value string) bool {
	if len(value) < 16 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func validate(record Record) error {
	hash, err := hex.DecodeString(record.ArchiveSHA256)
	if !validID(record.ID) || !validID(record.BackupID) || err != nil || len(hash) != 32 || record.ArchiveSHA256 != strings.ToLower(record.ArchiveSHA256) || record.Sequence < 1 || record.Sequence >= 1<<62 {
		return ErrStore
	}
	created, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return ErrStore
	}
	updated, err := time.Parse(time.RFC3339Nano, record.UpdatedAt)
	if err != nil || updated.Before(created) {
		return ErrStore
	}
	if record.RuntimeEnteredAt != "" {
		entered, e := time.Parse(time.RFC3339Nano, record.RuntimeEnteredAt)
		if e != nil || entered.Before(created) || entered.After(updated) || (record.LastStage != "serving" && record.LastStage != "stopped") || record.Sequence < 5 {
			return ErrStore
		}
	}
	switch record.LastStage {
	case "preparing", "installing", "committed", "cleaned", "serving", "stopped":
	default:
		return ErrStore
	}
	switch record.Status {
	case "active":
		if record.ErrorCode != "" || record.LastStage == "stopped" {
			return ErrStore
		}
	case "stopped":
		if record.LastStage != "stopped" || record.ErrorCode != "" {
			return ErrStore
		}
	case "failed":
		if record.ErrorCode != "restore_failed" {
			return ErrStore
		}
	case "interrupted":
		if record.ErrorCode != "process_interrupted" {
			return ErrStore
		}
	default:
		return ErrStore
	}
	return nil
}

func (s Store) read(identifier string) (Record, error) {
	if !validID(identifier) || len(s.Key) != 32 {
		return Record{}, ErrStore
	}
	path := filepath.Join(s.Directory, identifier+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8192 {
		return Record{}, ErrStore
	}
	file, err := os.Open(path)
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	opened, statErr := file.Stat()
	if statErr != nil || !os.SameFile(info, opened) {
		return Record{}, ErrStore
	}
	if err := filelock.CheckSingleLink(file); err != nil {
		return Record{}, ErrStore
	}
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil {
		return Record{}, err
	}
	var wrapped envelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wrapped) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return Record{}, ErrStore
	}
	mac := hmac.New(sha256.New, s.Key)
	mac.Write([]byte("proxy-control-restore-progress-v1\x00"))
	mac.Write(wrapped.Record)
	signature, err := hex.DecodeString(wrapped.MAC)
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return Record{}, ErrStore
	}
	var record Record
	decoder = json.NewDecoder(bytes.NewReader(wrapped.Record))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || decoder.Decode(&struct{}{}) != io.EOF || record.ID != identifier || validate(record) != nil {
		return Record{}, ErrStore
	}
	return record, nil
}

func (s Store) write(ctx context.Context, record Record, exclusive bool, expected ...Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(s.Key) != 32 || validate(record) != nil {
		return ErrStore
	}
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(s.Directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrStore
	}
	// Persist the directory entry even when this is its first record. Do not
	// claim the later record committed if the parent cannot be synchronized.
	if err := syncProgressDirectory(filepath.Dir(s.Directory)); err != nil {
		return err
	}
	if exclusive {
		entries, err := os.ReadDir(s.Directory)
		if err != nil || len(entries) >= 10000 {
			return ErrStore
		}
	} else {
		if len(expected) != 1 {
			return ErrStore
		}
		if err := s.requirePredecessor(record, expected[0]); err != nil {
			return err
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, s.Key)
	mac.Write([]byte("proxy-control-restore-progress-v1\x00"))
	mac.Write(data)
	data, err = json.Marshal(envelope{Record: data, MAC: hex.EncodeToString(mac.Sum(nil))})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.Directory, ".pending-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	target := filepath.Join(s.Directory, record.ID+".json")
	if exclusive {
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return ErrStore
		}
	} else {
		if err := s.requirePredecessor(record, expected[0]); err != nil {
			return err
		}
	}
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	if err := syncProgressDirectory(s.Directory); err != nil {
		return errors.Join(ErrPublishedUnconfirmed, err)
	}
	return nil
}
func (s Store) requirePredecessor(next, expected Record) error {
	if validate(expected) != nil || validate(next) != nil || expected.Status != "active" || expected.Sequence >= (1<<62)-1 || next.Sequence != expected.Sequence+1 || next.ID != expected.ID || next.BackupID != expected.BackupID || next.ArchiveSHA256 != expected.ArchiveSHA256 || next.CreatedAt != expected.CreatedAt {
		return ErrStore
	}
	current, err := s.read(next.ID)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrStore
	}
	previous, _ := time.Parse(time.RFC3339Nano, expected.UpdatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, next.UpdatedAt)
	if updated.Before(previous) {
		return ErrStore
	}
	if expected.RuntimeEnteredAt != "" && next.RuntimeEnteredAt != expected.RuntimeEnteredAt {
		return ErrStore
	}
	rank := map[string]int{"preparing": 0, "installing": 1, "committed": 2, "cleaned": 3, "serving": 4, "stopped": 5}
	if rank[next.LastStage] < rank[expected.LastStage] || rank[next.LastStage] > rank[expected.LastStage]+1 {
		return ErrStore
	}
	if expected.RuntimeEnteredAt == "" && next.RuntimeEnteredAt != "" {
		if expected.LastStage != "serving" || next.LastStage != "serving" || next.Status != "active" || next.RuntimeEnteredAt != next.UpdatedAt {
			return ErrStore
		}
	}
	return nil
}
