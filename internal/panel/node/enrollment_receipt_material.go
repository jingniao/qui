package node

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func ValidateEnrollmentReceiptMaterial(ctx context.Context, q replacementReader) error {
	_, err := readEnrollmentReceiptMaterial(ctx, q, "")
	return err
}
func readEnrollmentReceiptMaterial(ctx context.Context, q replacementReader, prefix string) ([]enrollmentReceipt, error) {
	if prefix != "" && prefix != "restore_source." {
		return nil, ErrToken
	}
	var present, applied int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"sqlite_master WHERE type='table' AND name='node_enrollment_receipts'").Scan(&present); err != nil {
		return nil, err
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"schema_migrations WHERE version='0068_node_enrollment_receipts.sql'").Scan(&applied); err != nil {
		return nil, err
	}
	if present == 0 && applied == 0 {
		return nil, nil
	}
	if present != 1 || applied != 1 {
		return nil, ErrToken
	}
	checks := []string{}
	for _, field := range strings.Split(enrollmentReceiptColumns, ",") {
		limit := "128"
		switch field {
		case "certificate_pem", "ca_pem":
			limit = "32768"
		case "operating_system":
			limit = "256"
		}
		checks = append(checks, "typeof("+field+")!='text' OR length("+field+")>"+limit)
	}
	var count, invalid, total int64
	if err := q.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(CASE WHEN "+strings.Join(checks, " OR ")+" THEN 1 ELSE 0 END),0),coalesce(sum(length(certificate_pem)+length(ca_pem)),0) FROM "+prefix+"node_enrollment_receipts").Scan(&count, &invalid, &total); err != nil {
		return nil, err
	}
	if count > 10000 || invalid != 0 || total > 64<<20 {
		return nil, ErrToken
	}
	rows, err := q.QueryContext(ctx, "SELECT "+enrollmentReceiptColumns+" FROM "+prefix+"node_enrollment_receipts ORDER BY token_hash LIMIT 10001")
	if err != nil {
		return nil, err
	}
	items := make([]enrollmentReceipt, 0, count)
	nodes := map[string]bool{}
	previous := ""
	for rows.Next() {
		item, err := scanEnrollmentReceipt(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if len(items) >= 10000 || nodes[item.NodeID] || item.TokenHash <= previous {
			rows.Close()
			return nil, ErrToken
		}
		nodes[item.NodeID] = true
		previous = item.TokenHash
		items = append(items, item)
	}
	readErr, closeErr := rows.Err(), rows.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if int64(len(items)) != count {
		return nil, ErrToken
	}
	for _, item := range items {
		var exists int
		if err := q.QueryRowContext(ctx, "SELECT count(*) FROM "+prefix+"nodes WHERE id=?", item.NodeID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists != 1 {
			return nil, ErrToken
		}
		// A retained receipt does not recreate missing tokens or revert later
		// identities. A surviving original token must retain its original facts.
		var tokenNode string
		var consumed sql.NullString
		err := q.QueryRowContext(ctx, "SELECT node_id,consumed_at FROM "+prefix+"node_enrollment_tokens WHERE token_hash=?", item.TokenHash).Scan(&tokenNode, &consumed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && (tokenNode != item.NodeID || !consumed.Valid || consumed.String != item.CreatedAt) {
			return nil, ErrToken
		}
	}
	return items, nil
}
func prepareEnrollmentReceiptRestore(ctx context.Context, tx *sql.Tx) (func() error, error) {
	current, err := readEnrollmentReceiptMaterial(ctx, tx, "")
	if err != nil {
		return nil, err
	}
	source, err := readEnrollmentReceiptMaterial(ctx, tx, "restore_source.")
	if err != nil {
		return nil, err
	}
	byToken := map[string]enrollmentReceipt{}
	byNode := map[string]string{}
	var total int64
	for _, item := range current {
		byToken[item.TokenHash] = item
		byNode[item.NodeID] = item.TokenHash
		total += int64(len(item.CertificatePEM) + len(item.CAPEM))
	}
	additions := []enrollmentReceipt{}
	for _, item := range source {
		if original, ok := byToken[item.TokenHash]; ok {
			if original != item {
				return nil, ErrRecoveryDecisionConflict
			}
			continue
		}
		if byNode[item.NodeID] != "" {
			return nil, ErrRecoveryDecisionConflict
		}
		byToken[item.TokenHash] = item
		byNode[item.NodeID] = item.TokenHash
		total += int64(len(item.CertificatePEM) + len(item.CAPEM))
		additions = append(additions, item)
	}
	if len(byToken) > 10000 || total > 64<<20 {
		return nil, ErrToken
	}
	return func() error {
		for _, item := range additions {
			if _, err := tx.ExecContext(ctx, "INSERT INTO node_enrollment_receipts("+enrollmentReceiptColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", item.TokenHash, item.NodeID, item.RequestDigest, item.PublicKeyDigest, item.Hostname, item.OS, item.AgentVersion, item.CertificatePEM, item.Fingerprint, item.CAPEM, item.ExpiresAt, item.CreatedAt); err != nil {
				return err
			}
		}
		return ValidateEnrollmentReceiptMaterial(ctx, tx)
	}, nil
}
