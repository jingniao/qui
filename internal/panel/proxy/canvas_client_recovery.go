package proxy

import (
	"context"
	"database/sql"
)

// PrepareClientRestoreTx owns the canvas write during recovery. Current client
// publications win; a backup-only publication cannot restore external access.
func (r *Repository) PrepareClientRestoreTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "UPDATE canvas_schemes SET client_published_layout=COALESCE((SELECT c.client_published_layout FROM restore_source.canvas_schemes c WHERE c.id=canvas_schemes.id AND c.execution='client'),'')")
	return err
}

// PublishClientCanvas snapshots only the version the Owner reviewed.
func (r *Repository) PublishClientCanvas(ctx context.Context, scheme CanvasScheme) error {
	result, err := r.Database.ExecContext(ctx, "UPDATE canvas_schemes SET client_published_layout=layout_json,published_at=?,published_batch_id='' WHERE id=? AND updated_at=?", timestamp(), scheme.ID, scheme.UpdatedAt)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrStaleScheme
	}
	return nil
}
