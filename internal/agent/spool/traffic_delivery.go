package spool

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

type TrafficDeliveryState struct {
	QueueID   string
	Issued    int64
	Confirmed int64
	Active    bool
}

func (s *Spool) TrafficDeliverySupported() bool {
	return s.bound != nil && s.bound.head.Version == 2
}

func OpenBound(ctx context.Context, directory, nodeID string, limit int64, witness string, expected []desiredstate.User) (*Spool, error) {
	return openBound(ctx, directory, nodeID, limit, witness, expected)
}

// PrepareTrafficDelivery requires the caller to have raised the authenticated
// spool floor after proving every retained rollback image can read format 7.
func (s *Spool) PrepareTrafficDelivery(ctx context.Context) error {
	unlock := s.accountingLock()
	defer unlock()
	tx, err := s.database.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.verifyBound(ctx, tx); err != nil {
		return err
	}
	if s.bound.head.Version == 2 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE traffic_delivery_state(id INTEGER PRIMARY KEY CHECK(id=1),queue_id TEXT NOT NULL,issued INTEGER NOT NULL CHECK(issued>=0),active INTEGER NOT NULL CHECK(active IN (0,1)));
 ALTER TABLE traffic_batches ADD COLUMN delivery_seq INTEGER NOT NULL DEFAULT 0;
 CREATE UNIQUE INDEX traffic_delivery_sequence ON traffic_batches(delivery_seq) WHERE delivery_seq>0;`); err != nil {
		return err
	}
	queue, err := id.New()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO traffic_delivery_state VALUES(1,?,0,0)", queue); err != nil {
		return err
	}
	// Only after the original v1 witness verified may the new tables enter v2.
	s.bound.head.Version = 2
	if err = s.commitAccounting(ctx, tx); err != nil {
		s.bound.head.Version = 1
		return err
	}
	return nil
}

func (s *Spool) TrafficDeliveryState(ctx context.Context) (TrafficDeliveryState, error) {
	unlock := s.accountingLock()
	defer unlock()
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TrafficDeliveryState{}, err
	}
	defer tx.Rollback()
	if err = s.verifyBound(ctx, tx); err != nil {
		return TrafficDeliveryState{}, err
	}
	return trafficDeliveryStateTx(ctx, tx)
}
func trafficDeliveryStateTx(ctx context.Context, tx *sql.Tx) (TrafficDeliveryState, error) {
	var d TrafficDeliveryState
	err := tx.QueryRowContext(ctx, `SELECT queue_id,issued,active,coalesce((SELECT min(delivery_seq)-1 FROM traffic_batches WHERE delivery_seq>0),issued) FROM traffic_delivery_state WHERE id=1`).Scan(&d.QueueID, &d.Issued, &d.Active, &d.Confirmed)
	if err == nil && (d.Confirmed < 0 || d.Confirmed > d.Issued) {
		err = ErrStoreIntegrity
	}
	return d, err
}
func (s *Spool) EnableTrafficDelivery(ctx context.Context) error {
	unlock := s.accountingLock()
	defer unlock()
	tx, err := s.database.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.verifyBound(ctx, tx); err != nil {
		return err
	}
	d, err := trafficDeliveryStateTx(ctx, tx)
	if err != nil {
		return err
	}
	if d.Active {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM traffic_batches ORDER BY rowid")
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, value)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, value := range ids {
		if d.Issued == math.MaxInt64 {
			return ErrStoreIntegrity
		}
		d.Issued++
		if _, err = tx.ExecContext(ctx, "UPDATE traffic_batches SET delivery_seq=? WHERE id=?", d.Issued, value); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE traffic_delivery_state SET issued=?,active=1 WHERE id=1", d.Issued); err != nil {
		return err
	}
	return s.commitAccounting(ctx, tx)
}
func (s *Spool) TrafficEnvelope(ctx context.Context, batchID string) (traffic.Delivery, error) {
	unlock := s.accountingLock()
	defer unlock()
	tx, err := s.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return traffic.Delivery{}, err
	}
	defer tx.Rollback()
	if err = s.verifyBound(ctx, tx); err != nil {
		return traffic.Delivery{}, err
	}
	var d traffic.Delivery
	err = tx.QueryRowContext(ctx, "SELECT s.queue_id,b.delivery_seq FROM traffic_delivery_state s JOIN traffic_batches b ON b.id=? WHERE s.id=1 AND s.active=1", batchID).Scan(&d.QueueID, &d.Sequence)
	if err != nil {
		return d, err
	}
	return d, d.Validate()
}
func (s *Spool) numberTrafficTx(ctx context.Context, tx *sql.Tx, batchID string) error {
	if s.bound == nil || s.bound.head.Version < 2 {
		return nil
	}
	d, err := trafficDeliveryStateTx(ctx, tx)
	if err != nil {
		return err
	}
	if !d.Active {
		return nil
	}
	if d.Issued == math.MaxInt64 {
		return ErrStoreIntegrity
	}
	if _, err = tx.ExecContext(ctx, "UPDATE traffic_batches SET delivery_seq=? WHERE id=?", d.Issued+1, batchID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE traffic_delivery_state SET issued=issued+1 WHERE id=1")
	return err
}

func criticalDigestVersion(ctx context.Context, tx *sql.Tx, version int) (string, bool, error) {
	base, budgeted, err := criticalDigest(ctx, tx)
	if err != nil || version == 1 {
		return base, budgeted, err
	}
	h := sha256.New()
	hashPart(h, []byte("proxy-agent-spool-critical-v2"))
	hashPart(h, []byte(base))
	var queue string
	var issued int64
	var active int
	if err = tx.QueryRowContext(ctx, "SELECT queue_id,issued,active FROM traffic_delivery_state WHERE id=1").Scan(&queue, &issued, &active); err != nil {
		return "", false, err
	}
	if queue == "" || issued < 0 || (active != 0 && active != 1) {
		return "", false, ErrStoreIntegrity
	}
	hashPart(h, []byte(fmt.Sprintf("%s:%d:%d", queue, issued, active)))
	rows, err := tx.QueryContext(ctx, "SELECT id,delivery_seq,payload FROM traffic_batches ORDER BY id")
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var batch string
		var seq int64
		var payload []byte
		if err = rows.Scan(&batch, &seq, &payload); err != nil {
			return "", false, err
		}
		if seq < 0 || seq > issued || active == 1 && seq == 0 || len(payload) == 0 || len(payload) > maxQueuedBatchBytes {
			return "", false, ErrStoreIntegrity
		}
		hashPart(h, []byte(batch))
		hashPart(h, []byte(fmt.Sprint(seq)))
		hashPart(h, payload)
	}
	if err = rows.Err(); err != nil {
		return "", false, err
	}
	return hex.EncodeToString(h.Sum(nil)), budgeted, nil
}
