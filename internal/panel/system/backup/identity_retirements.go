package backup

import (
	"context"
	"database/sql"
	"encoding/hex"
)

func validateIdentityRetirements(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0134_node_identity_recovery.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	var invalid int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_identity_retirements r LEFT JOIN nodes n ON n.id=r.node_id WHERE n.id IS NULL OR length(r.fingerprint)<>64").Scan(&invalid); err != nil || invalid != 0 {
		return ErrArchive
	}
	rows, err := database.QueryContext(ctx, "SELECT fingerprint FROM node_identity_retirements")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var fingerprint string
		if err := rows.Scan(&fingerprint); err != nil {
			return ErrArchive
		}
		if _, err := hex.DecodeString(fingerprint); err != nil {
			return ErrArchive
		}
	}
	if err := rows.Err(); err != nil {
		return ErrArchive
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM node_identities i JOIN node_identity_retirements r ON r.fingerprint=i.fingerprint").Scan(&invalid); err != nil || invalid != 0 {
		return ErrArchive
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM (SELECT node_id FROM node_identity_recoveries WHERE status='issued' GROUP BY node_id HAVING count(*)>1)").Scan(&invalid); err != nil || invalid != 0 {
		return ErrArchive
	}
	return nil
}
