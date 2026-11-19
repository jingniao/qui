package access

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

type trafficDeliveryError struct{ code, message string }

func (e *trafficDeliveryError) Error() string                    { return e.code + ": " + e.message }
func (e *trafficDeliveryError) RejectionReason() (string, int64) { return e.code, 0 }

var ErrTrafficRetired = &trafficDeliveryError{"traffic_delivery_retired", "traffic delivery is sealed; retain the original node queue and restore matching state"}
var ErrTrafficDelivery = &trafficDeliveryError{"traffic_delivery_conflict", "queue identity or completion progress does not match"}

func (s *Service) NegotiateTrafficDelivery(ctx context.Context, node, queue, fingerprint string, confirmed, issued uint64) error {
	if err := (traffic.Delivery{QueueID: queue, Sequence: 1}).Validate(); err != nil {
		return err
	}
	if confirmed > issued || issued > math.MaxInt64 {
		return ErrTrafficDelivery
	}
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var authenticated int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM node_identities WHERE node_id=? AND fingerprint=? AND revoked_at IS NULL", node, fingerprint).Scan(&authenticated); err != nil {
		return err
	}
	if authenticated != 1 {
		return ErrTrafficDelivery
	}
	var existing string
	var c, a int64
	err = tx.QueryRowContext(ctx, "SELECT queue_id,completed_seq,confirmed_seq FROM traffic_delivery_queues WHERE node_id=? AND closed_at=''", node).Scan(&existing, &c, &a)
	if errors.Is(err, sql.ErrNoRows) {
		// A consumed recovery code explicitly closes the prior live queue.
		// The recovered host may retain that matching spool or start a new one.
		var closed, recovery string
		err = tx.QueryRowContext(ctx, "SELECT closed_at,recovery_id,completed_seq,confirmed_seq FROM traffic_delivery_queues WHERE node_id=? AND queue_id=?", node, queue).Scan(&closed, &recovery, &c, &a)
		if err == nil {
			var latest string
			if err = tx.QueryRowContext(ctx, "SELECT id FROM node_identity_recoveries WHERE node_id=? AND status='consumed' ORDER BY consumed_at DESC,created_at DESC,id DESC LIMIT 1", node).Scan(&latest); err != nil {
				return err
			}
			if closed == "" || recovery != latest || int64(issued) < c || int64(confirmed) < a {
				return ErrTrafficDelivery
			}
			_, err = tx.ExecContext(ctx, "UPDATE traffic_delivery_queues SET closed_at='',recovery_id='',updated_at=? WHERE node_id=? AND queue_id=?", timestamp(time.Now()), node, queue)
		} else if errors.Is(err, sql.ErrNoRows) {
			if confirmed != 0 {
				return ErrTrafficDelivery
			}
			var pending int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM traffic_pending_batches WHERE node_id=? AND delivery_seq=0", node).Scan(&pending); err != nil {
				return err
			}
			if pending > 0 {
				return ErrTrafficDelivery
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO traffic_delivery_queues(node_id,queue_id,updated_at) VALUES(?,?,?)", node, queue, timestamp(time.Now()))
		}
	} else if err == nil {
		if existing != queue || int64(issued) < c || int64(confirmed) < a {
			return ErrTrafficDelivery
		}
	}
	if err != nil {
		return err
	}
	if err = confirmTrafficDeliveryTx(ctx, tx, node, queue, confirmed); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RequireTrafficProtocol(ctx context.Context, node string, supported bool) error {
	if supported {
		return nil
	}
	var count int
	if err := s.Repository.Database.QueryRowContext(ctx, "SELECT count(*) FROM traffic_delivery_queues WHERE node_id=?", node).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrTrafficDelivery
	}
	return nil
}

func checkTrafficDeliveryTx(ctx context.Context, tx *sql.Tx, node string, d traffic.Delivery) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if d.QueueID == "" {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM traffic_delivery_queues WHERE node_id=?", node).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return ErrTrafficDelivery
		}
		return nil
	}
	var retired int64
	err := tx.QueryRowContext(ctx, "SELECT retired_seq FROM traffic_delivery_queues WHERE node_id=? AND queue_id=?", node, d.QueueID).Scan(&retired)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTrafficDelivery
	}
	if err != nil {
		return err
	}
	if d.Sequence <= retired {
		return ErrTrafficRetired
	}
	return nil
}

// The transport identity is durable before the original pipeline can resume.
// Existing legacy receipts can acquire a binding, but never a completion fact.
func (r *Repository) prepareTrafficDelivery(ctx context.Context, b traffic.Batch) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkTrafficDeliveryTx(ctx, tx, b.NodeID, b.Delivery); err != nil {
		return err
	}
	if b.Delivery.QueueID == "" {
		return nil
	}
	digest, err := trafficDigest(b)
	if err != nil {
		return err
	}
	for _, table := range []string{"traffic_batches", "traffic_pending_batches"} {
		var batch, stored, queue string
		var seq int64
		err = tx.QueryRowContext(ctx, "SELECT batch_id,digest,queue_id,delivery_seq FROM "+table+" WHERE node_id=? AND (batch_id=? OR (queue_id=? AND delivery_seq=?)) LIMIT 1", b.NodeID, b.ID, b.Delivery.QueueID, b.Delivery.Sequence).Scan(&batch, &stored, &queue, &seq)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if batch != b.ID || stored != digest || (queue != "" && (queue != b.Delivery.QueueID || seq != b.Delivery.Sequence)) {
			return ErrTrafficDelivery
		}
		if queue == "" {
			if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET queue_id=?,delivery_seq=? WHERE node_id=? AND batch_id=?", b.Delivery.QueueID, b.Delivery.Sequence, b.NodeID, b.ID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func advanceTrafficCompletionTx(ctx context.Context, tx *sql.Tx, node, queue string) error {
	var completed int64
	err := tx.QueryRowContext(ctx, "SELECT completed_seq FROM traffic_delivery_queues WHERE node_id=? AND queue_id=?", node, queue).Scan(&completed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Bounded passes avoid holding the writer for a large repaired backlog.
	rows, err := tx.QueryContext(ctx, "SELECT delivery_seq,accounting_completed_at FROM traffic_batches WHERE node_id=? AND queue_id=? AND delivery_seq>? ORDER BY delivery_seq LIMIT 500", node, queue, completed)
	if err != nil {
		return err
	}
	for rows.Next() {
		var seq int64
		var at string
		if err = rows.Scan(&seq, &at); err != nil {
			rows.Close()
			return err
		}
		if seq != completed+1 || at == "" {
			break
		}
		completed = seq
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE traffic_delivery_queues SET completed_seq=?,updated_at=? WHERE node_id=? AND queue_id=?", completed, timestamp(time.Now()), node, queue)
	return err
}

func (s *Service) ConfirmTrafficDelivery(ctx context.Context, node, queue string, confirmed uint64) error {
	if confirmed > math.MaxInt64 {
		return ErrTrafficDelivery
	}
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = confirmTrafficDeliveryTx(ctx, tx, node, queue, confirmed); err != nil {
		return err
	}
	return tx.Commit()
}

func confirmTrafficDeliveryTx(ctx context.Context, tx *sql.Tx, node, queue string, confirmed uint64) error {
	var stored string
	var c, a, r, candidate int64
	var at string
	err := tx.QueryRowContext(ctx, "SELECT queue_id,completed_seq,confirmed_seq,retired_seq,candidate_seq,candidate_at FROM traffic_delivery_queues WHERE node_id=? AND closed_at=''", node).Scan(&stored, &c, &a, &r, &candidate, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTrafficDelivery
	}
	if err != nil {
		return err
	}
	if queue != stored {
		return ErrTrafficDelivery
	}
	for int64(confirmed) > c {
		previous := c
		if err = advanceTrafficCompletionTx(ctx, tx, node, queue); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, "SELECT completed_seq FROM traffic_delivery_queues WHERE node_id=? AND queue_id=?", node, queue).Scan(&c); err != nil {
			return err
		}
		if c == previous {
			break
		}
	}
	if queue != stored || int64(confirmed) < a || int64(confirmed) > c {
		return ErrTrafficDelivery
	}
	now := timestamp(time.Now())
	if candidate == r && int64(confirmed) > r {
		candidate = int64(confirmed)
		at = now
	}
	_, err = tx.ExecContext(ctx, "UPDATE traffic_delivery_queues SET confirmed_seq=?,candidate_seq=?,candidate_at=?,updated_at=? WHERE node_id=? AND queue_id=?", int64(confirmed), candidate, at, now, node, queue)
	if err != nil {
		return err
	}
	return nil
}

func (r *Repository) retireTrafficReceipts(ctx context.Context, now time.Time) (bool, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var node, queue string
	var retired, candidate int64
	err = tx.QueryRowContext(ctx, "SELECT node_id,queue_id,retired_seq,candidate_seq FROM traffic_delivery_queues WHERE candidate_seq>retired_seq AND candidate_at<=? ORDER BY candidate_at,node_id LIMIT 1", timestamp(now.Add(-24*time.Hour))).Scan(&node, &queue, &retired, &candidate)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	end := candidate
	if end-retired > 500 {
		end = retired + 500
	}
	var count, pending int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM traffic_batches WHERE node_id=? AND queue_id=? AND delivery_seq>? AND delivery_seq<=? AND accounting_completed_at<>''", node, queue, retired, end).Scan(&count); err != nil {
		return false, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM traffic_pending_batches WHERE node_id=? AND queue_id=? AND delivery_seq<=?", node, queue, end).Scan(&pending); err != nil {
		return false, err
	}
	if int64(count) != end-retired || pending > 0 {
		return false, ErrTrafficDelivery
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM traffic_batches WHERE node_id=? AND queue_id=? AND delivery_seq>? AND delivery_seq<=?", node, queue, retired, end); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE traffic_delivery_queues SET retired_seq=?,candidate_seq=CASE WHEN candidate_seq=? THEN confirmed_seq ELSE candidate_seq END,candidate_at=CASE WHEN candidate_seq=? THEN ? ELSE candidate_at END,updated_at=? WHERE node_id=? AND queue_id=?`, end, end, end, timestamp(now), timestamp(now), node, queue); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE traffic_storage_job SET retired_receipts=retired_receipts+?,updated_at=? WHERE id=1", count, timestamp(now)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *Repository) AdvanceTrafficStorage(ctx context.Context, now time.Time) (bool, error) {
	pending, err := r.compactCoreTraffic(ctx, now)
	if err == nil {
		var more bool
		more, err = r.retireTrafficReceipts(ctx, now)
		pending = pending || more
	}
	code := ""
	if err != nil {
		code = "traffic_storage_failed"
	}
	// Preserve the original error even if persisting the diagnostic also fails.
	_, saveErr := r.Database.ExecContext(ctx, "UPDATE traffic_storage_job SET error_code=?,last_success_at=CASE WHEN ?='' THEN ? ELSE last_success_at END WHERE id=1", code, code, timestamp(now))
	return pending, errors.Join(err, saveErr)
}
