package node

import (
	"context"
	"database/sql"
)

const sequenceMaterialSQL = "CASE WHEN typeof(last_sequence)='integer' THEN last_sequence ELSE -1 END"

func scanSequence(row interface{ Scan(...any) error }) (uint64, error) {
	var value int64
	if err := row.Scan(&value); err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, ErrSequence
	}
	return uint64(value), nil
}
func requireRestoreSequenceMaterialTx(ctx context.Context, tx *sql.Tx) error {
	for _, prefix := range []string{"", "restore_source."} {
		var invalid bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+prefix+"nodes WHERE typeof(id)<>'text' OR length(id) NOT BETWEEN 1 AND 128 OR typeof(last_sequence)<>'integer' OR last_sequence<0 LIMIT 1)").Scan(&invalid); err != nil {
			return err
		}
		if invalid {
			return ErrSequence
		}
	}
	return nil
}
func sequenceRowsChanged(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrSequence
	}
	return nil
}
