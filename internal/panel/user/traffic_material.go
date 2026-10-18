package user

import (
	"context"
	"database/sql"
	"errors"
)

var ErrTrafficMaterial = errors.New("原账户消费游标或计数无法核对，保留原记录，不默认零或确认入账")

type originalTrafficCursor struct {
	Owner, Device    string
	Upload, Download int64
}

func readOriginalTrafficCursor(ctx context.Context, tx *sql.Tx, nodeID, epoch, credentialID string) (originalTrafficCursor, error) {
	var item originalTrafficCursor
	err := tx.QueryRowContext(ctx, `SELECT
CASE WHEN typeof(user_id)='text' AND length(user_id) BETWEEN 1 AND 128 THEN user_id ELSE '' END,
CASE WHEN typeof(device_id)='text' AND length(device_id) BETWEEN 1 AND 128 THEN device_id ELSE '' END,
CASE WHEN typeof(upload_bytes)='integer' THEN upload_bytes ELSE -1 END,
CASE WHEN typeof(download_bytes)='integer' THEN download_bytes ELSE -1 END
FROM user_traffic_cursors WHERE node_id=? AND epoch=? AND credential_id=?`, nodeID, epoch, credentialID).Scan(&item.Owner, &item.Device, &item.Upload, &item.Download)
	if err != nil {
		return item, err
	}
	if item.Owner == "" || item.Device == "" || item.Upload < 0 || item.Download < 0 {
		return originalTrafficCursor{}, ErrTrafficMaterial
	}
	return item, nil
}

func requireOneTrafficUpdate(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrTrafficMaterial
	}
	return nil
}
