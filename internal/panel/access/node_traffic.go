package access

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"

	"proxycontrol/pkg/contracts/traffic"
)

// Node cursors are independent of user cursors; peer epochs are never retired.
func ingestNodeCounter(ctx context.Context, tx *sql.Tx, nodeID string, c traffic.Counter) error {
	if c.Kind != "node" || !strings.HasSuffix(c.CoreEpoch, ".node") || c.UploadBytes > 1<<50 || c.DownloadBytes > 1<<50 {
		return ErrInvalid
	}
	var up, down, rev int64
	var core, observed string
	err := tx.QueryRowContext(ctx, "SELECT upload_bytes,download_bytes,revision,core,observed_at FROM node_traffic_cursors WHERE node_id=? AND epoch=? AND credential_id=?", nodeID, c.CoreEpoch, c.CredentialID).Scan(&up, &down, &rev, &core, &observed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if core != "" && (core != c.Core || rev != c.Revision) {
		return ErrConflict
	}
	stamp := timestamp(c.ObservedAt)
	if c.UploadBytes < up || c.DownloadBytes < down {
		if c.UploadBytes <= up && c.DownloadBytes <= down && stamp <= observed {
			return nil
		}
		return ErrConflict
	}
	du, dd := c.UploadBytes-up, c.DownloadBytes-down
	if du != 0 || dd != 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,?,?,?,?,?)
 ON CONFLICT(node_id,core,epoch,observed_at) DO UPDATE SET upload_bytes=core_traffic_samples.upload_bytes+excluded.upload_bytes,download_bytes=core_traffic_samples.download_bytes+excluded.download_bytes
 WHERE core_traffic_samples.upload_bytes<=?-excluded.upload_bytes AND core_traffic_samples.download_bytes<=?-excluded.download_bytes`, nodeID, c.Core, c.CoreEpoch, stamp, du, dd, math.MaxInt64, math.MaxInt64)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return ErrConflict
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_traffic_cursors(node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(node_id,epoch,credential_id) DO UPDATE SET upload_bytes=excluded.upload_bytes,download_bytes=excluded.download_bytes,observed_at=excluded.observed_at`, nodeID, c.CoreEpoch, c.CredentialID, c.Core, c.Revision, c.UploadBytes, c.DownloadBytes, stamp)
	return err
}
