package db

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// RecoverySchema is attached read-only to an isolated candidate database by
// the offline coordinator. Module repositories alone choose their tables.
const RecoverySchema = "restore_source"

var ErrRecoveryConflict = errors.New("restore evidence is missing, regressed or inconsistent")

func AttachRecoverySource(ctx context.Context, database *sql.DB, path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath, RawQuery: url.Values{"mode": {"ro"}, "immutable": {"1"}}.Encode()}
	_, err = database.ExecContext(ctx, "ATTACH DATABASE ? AS "+RecoverySchema, uri.String())
	return err
}

func AssertRecoveryNone(ctx context.Context, tx *sql.Tx, query string) error {
	var count int64
	if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrRecoveryConflict
	}
	return nil
}

// CopyRecoveryTable is a SQL adapter called only by the table-owning
// repository. Identifier inputs must be constants, never request fields.
func CopyRecoveryTable(ctx context.Context, tx *sql.Tx, table, columns string) error {
	if !recoveryIdentifier(table) {
		return ErrRecoveryConflict
	}
	for _, column := range strings.Split(columns, ",") {
		if !recoveryIdentifier(column) {
			return ErrRecoveryConflict
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO "+table+"("+columns+") SELECT "+columns+" FROM "+RecoverySchema+"."+table)
	return err
}

func PreserveRecoverySequence(ctx context.Context, tx *sql.Tx, table string) error {
	if !recoveryIdentifier(table) {
		return ErrRecoveryConflict
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sqlite_sequence SET seq=max(seq,coalesce((SELECT seq FROM "+RecoverySchema+".sqlite_sequence WHERE name=?),0)) WHERE name=?", table, table); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO sqlite_sequence(name,seq) SELECT name,seq FROM "+RecoverySchema+".sqlite_sequence WHERE name=? AND NOT EXISTS(SELECT 1 FROM sqlite_sequence WHERE name=?)", table, table)
	return err
}

func recoveryIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if !(char >= 'a' && char <= 'z' || char == '_' || index > 0 && char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}
