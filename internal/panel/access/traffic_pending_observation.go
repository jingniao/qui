package access

import (
	"context"
	"proxycontrol/pkg/contracts/traffic"
)

// PendingTrafficObservation compares authenticated original input with already
// verified signed node cursors. Coverage cannot substitute for the normal debit
// pipeline or prove that statistics/user/budget transactions completed.
type PendingTrafficObservation struct {
	BatchID                     string `json:"batchId"`
	BatchSHA256                 string `json:"batchSha256"`
	Relation                    string `json:"relation"`
	TotalCounters               int    `json:"totalCounters"`
	CoveredCounters             int    `json:"coveredCounters"`
	MissingCounters             int    `json:"missingCounters"`
	UnknownIdentityCounters     int    `json:"unknownIdentityCounters"`
	ConflictingIdentityCounters int    `json:"conflictingIdentityCounters"`
	BehindCounters              int    `json:"behindCounters"`
	OriginalPipelineRequired    bool   `json:"originalPipelineRequired"`
}

func (r *Repository) ObservePendingTraffic(ctx context.Context, nodeID string, cursors []traffic.RecoveryCursor) ([]PendingTrafficObservation, error) {
	var present int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='traffic_pending_batches'").Scan(&present); err != nil {
		return nil, err
	}
	if present == 0 {
		return []PendingTrafficObservation{{Relation: "historical_pending_input_journal_unavailable", OriginalPipelineRequired: true}}, nil
	}
	type cursorKey struct{ epoch, credential string }
	observed := map[cursorKey]traffic.RecoveryCursor{}
	for _, cursor := range cursors {
		key := cursorKey{cursor.Epoch, cursor.CredentialID}
		if _, exists := observed[key]; exists {
			return nil, ErrInvalid
		}
		observed[key] = cursor
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT batch_id,digest,CASE WHEN length(encrypted_batch)<=1049600 THEN encrypted_batch END FROM traffic_pending_batches WHERE node_id=? ORDER BY batch_id", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []PendingTrafficObservation{}
	var count, total int64
	for rows.Next() {
		count++
		var id, digest string
		var sealed []byte
		if err := rows.Scan(&id, &digest, &sealed); err != nil {
			return nil, err
		}
		total += int64(len(sealed))
		if count > 10000 || total > 64<<20 {
			return nil, ErrInvalid
		}
		batch, err := decodePendingTraffic(r.Vault, nodeID, id, digest, sealed)
		if err != nil {
			return nil, err
		}
		result := PendingTrafficObservation{BatchID: id, BatchSHA256: digest, TotalCounters: len(batch.Counters), Relation: "signed_cursors_cover_pending_input_pipeline_required", OriginalPipelineRequired: true}
		for _, counter := range batch.Counters {
			cursor, exists := observed[cursorKey{counter.CoreEpoch, counter.CredentialID}]
			switch {
			case !exists:
				result.MissingCounters++
			case !cursor.IdentityKnown:
				result.UnknownIdentityCounters++
			case cursor.UserID != counter.UserID || cursor.DeviceID != counter.DeviceID || cursor.Revision != counter.Revision:
				result.ConflictingIdentityCounters++
			case cursor.Upload < counter.UploadBytes || cursor.Download < counter.DownloadBytes:
				result.BehindCounters++
			default:
				result.CoveredCounters++
			}
		}
		if result.CoveredCounters != result.TotalCounters {
			result.Relation = "pending_input_has_unresolved_signed_counters"
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
