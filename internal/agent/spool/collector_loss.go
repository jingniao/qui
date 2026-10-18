package spool

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"
)

type CollectorLossCounts struct {
	Allowed             uint64 `json:"allowed"`
	Blocked             uint64 `json:"blocked"`
	Rejected            uint64 `json:"rejected"`
	PersistenceFailures uint64 `json:"persistenceFailures"`
}
type CollectorLossState struct {
	Version              int                 `json:"version"`
	NodeID               string              `json:"nodeID"`
	ProcessID            string              `json:"processID"`
	Generation           uint64              `json:"generation"`
	BaselineAt           time.Time           `json:"baselineAt"`
	ProcessStartedAt     time.Time           `json:"processStartedAt"`
	CheckpointAt         time.Time           `json:"checkpointAt"`
	Current              CollectorLossCounts `json:"current"`
	Total                CollectorLossCounts `json:"total"`
	UnconfirmedProcesses uint64              `json:"unconfirmedProcesses"`
	EndConfirmed         bool                `json:"endConfirmed"`
	Saturated            bool                `json:"saturated"`
}

var ErrCollectorLossState = errors.New("collector loss checkpoint unavailable; preserve original queue")

func validCollectorProcess(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 16 && hex.EncodeToString(b) == value
}

// The diagnostic marker is independent of billing authority. It prevents a
// missing row/table from being silently initialized after feature activation.
func (s *Spool) ensureCollectorLoss(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(spool_metadata)")
	if err != nil {
		return err
	}
	hasMarker := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "collector_loss_enabled" {
			hasMarker = true
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if !hasMarker {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE spool_metadata ADD COLUMN collector_loss_enabled INTEGER NOT NULL DEFAULT 0 CHECK(collector_loss_enabled IN (0,1))"); err != nil {
			return err
		}
	}
	var enabled, present int
	if err := tx.QueryRowContext(ctx, "SELECT collector_loss_enabled FROM spool_metadata WHERE id=1").Scan(&enabled); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='collector_loss_state'").Scan(&present); err != nil {
		return err
	}
	if enabled == 1 {
		if present != 1 {
			return ErrCollectorLossState
		}
		return nil
	}
	if enabled != 0 || present != 0 {
		return ErrCollectorLossState
	}
	if _, err := tx.ExecContext(ctx, "CREATE TABLE collector_loss_state(id INTEGER PRIMARY KEY CHECK(id=1),payload BLOB NOT NULL)"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE spool_metadata SET collector_loss_enabled=1 WHERE id=1"); err != nil {
		return err
	}
	return nil
}
func (s *Spool) readCollectorLoss(ctx context.Context, tx *sql.Tx) (CollectorLossState, error) {
	var state CollectorLossState
	var sealed []byte
	if err := tx.QueryRowContext(ctx, "SELECT CASE WHEN typeof(payload)='blob' AND length(payload) BETWEEN 1 AND 8192 THEN payload ELSE NULL END FROM collector_loss_state WHERE id=1").Scan(&sealed); err != nil {
		return state, err
	}
	if len(sealed) == 0 {
		return state, ErrCollectorLossState
	}
	data, err := s.vault.Open(sealed)
	if err != nil {
		return state, ErrCollectorLossState
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(&struct{}{}) != io.EOF || state.Version != 1 || state.NodeID != s.nodeID || !validCollectorProcess(state.ProcessID) || state.Generation == 0 || state.BaselineAt.IsZero() || state.ProcessStartedAt.IsZero() || state.CheckpointAt.IsZero() {
		return state, ErrCollectorLossState
	}
	if state.Current.Allowed > state.Total.Allowed || state.Current.Blocked > state.Total.Blocked || state.Current.Rejected > state.Total.Rejected || state.Current.PersistenceFailures > state.Total.PersistenceFailures || state.UnconfirmedProcesses >= state.Generation {
		return state, ErrCollectorLossState
	}
	for _, stamp := range []time.Time{state.BaselineAt, state.ProcessStartedAt, state.CheckpointAt} {
		if stamp.Year() < 2000 || stamp.Year() > 9999 {
			return state, ErrCollectorLossState
		}
	}
	return state, nil
}
func (s *Spool) writeCollectorLoss(ctx context.Context, tx *sql.Tx, state CollectorLossState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > 7168 {
		return ErrCollectorLossState
	}
	sealed, err := s.vault.Seal(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO collector_loss_state(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", sealed)
	return err
}

func (s *Spool) BeginCollectorLoss(ctx context.Context, process string, started time.Time) (CollectorLossState, error) {
	var state CollectorLossState
	if !validCollectorProcess(process) || started.IsZero() || started.Year() < 2000 || started.Year() > 9999 {
		return state, ErrCollectorLossState
	}
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return state, err
	}
	defer tx.Rollback()
	// Only a newly created table may legitimately lack its first row.
	var marker int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_info('spool_metadata') WHERE name='collector_loss_enabled'").Scan(&marker); err != nil {
		return state, err
	}
	enabled := 0
	if marker == 1 {
		if err := tx.QueryRowContext(ctx, "SELECT collector_loss_enabled FROM spool_metadata WHERE id=1").Scan(&enabled); err != nil {
			return state, err
		}
	}
	if err := s.ensureCollectorLoss(ctx, tx); err != nil {
		return state, err
	}
	previous, err := s.readCollectorLoss(ctx, tx)
	if errors.Is(err, sql.ErrNoRows) && enabled == 0 {
		state = CollectorLossState{Version: 1, NodeID: s.nodeID, BaselineAt: time.Now().UTC()}
	} else if err != nil {
		return state, err
	} else {
		state = previous
	}
	if state.ProcessID == process {
		if !state.ProcessStartedAt.Equal(started) || state.EndConfirmed {
			return state, ErrCollectorLossState
		}
		return state, tx.Commit()
	}
	if state.Generation == math.MaxUint64 {
		return state, ErrCollectorLossState
	}
	if state.Generation > 0 && !state.EndConfirmed {
		state.UnconfirmedProcesses++
	}
	state.Generation++
	state.ProcessID = process
	state.ProcessStartedAt = started.UTC()
	state.Current = CollectorLossCounts{}
	state.EndConfirmed = false
	state.CheckpointAt = time.Now().UTC()
	if err := s.writeCollectorLoss(ctx, tx, state); err != nil {
		return state, err
	}
	return state, tx.Commit()
}
func lossDelta(total, previous, next uint64) (uint64, bool, error) {
	if next < previous {
		return 0, false, ErrCollectorLossState
	}
	delta := next - previous
	if math.MaxUint64-total < delta {
		return math.MaxUint64, true, nil
	}
	return total + delta, false, nil
}
func (s *Spool) CheckpointCollectorLoss(ctx context.Context, process string, generation uint64, counts CollectorLossCounts, saturated, closed bool) (CollectorLossState, error) {
	tx, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return CollectorLossState{}, err
	}
	defer tx.Rollback()
	state, err := s.readCollectorLoss(ctx, tx)
	if err != nil {
		return state, err
	}
	if state.ProcessID != process || state.Generation != generation {
		return state, ErrCollectorLossState
	}
	if state.EndConfirmed {
		if closed && counts == state.Current && (!saturated || state.Saturated) {
			return state, tx.Commit()
		}
		return state, ErrCollectorLossState
	}
	for _, pair := range []struct {
		total          *uint64
		previous, next uint64
	}{{&state.Total.Allowed, state.Current.Allowed, counts.Allowed}, {&state.Total.Blocked, state.Current.Blocked, counts.Blocked}, {&state.Total.Rejected, state.Current.Rejected, counts.Rejected}, {&state.Total.PersistenceFailures, state.Current.PersistenceFailures, counts.PersistenceFailures}} {
		value, clamped, err := lossDelta(*pair.total, pair.previous, pair.next)
		if err != nil {
			return state, err
		}
		*pair.total = value
		state.Saturated = state.Saturated || clamped
	}
	state.Saturated = state.Saturated || saturated
	state.Current = counts
	state.EndConfirmed = closed
	state.CheckpointAt = time.Now().UTC()
	if err := s.writeCollectorLoss(ctx, tx, state); err != nil {
		return state, err
	}
	return state, tx.Commit()
}
