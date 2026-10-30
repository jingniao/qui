package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
)

// PrepareClientRestoreTx owns the canvas write during recovery. Current client
// publications win; a backup-only publication cannot restore external access.
func (r *Repository) PrepareClientRestoreTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "UPDATE canvas_schemes SET client_published_layout=COALESCE((SELECT c.client_published_layout FROM restore_source.canvas_schemes c WHERE c.id=canvas_schemes.id AND c.execution='client'),'')")
	if err != nil {
		return err
	}
	// Recovery copies raw historical JSON; canonicalize both layouts before
	// committing, while preserving the empty-string unpublished sentinel.
	rows, err := tx.QueryContext(ctx, "SELECT id,layout_json,client_published_layout FROM canvas_schemes")
	if err != nil {
		return err
	}
	type restoredLayout struct{ id, draft, published string }
	var layouts []restoredLayout
	for rows.Next() {
		var item restoredLayout
		if err := rows.Scan(&item.id, &item.draft, &item.published); err != nil {
			rows.Close()
			return err
		}
		for _, raw := range []*string{&item.draft, &item.published} {
			if *raw == "" {
				continue
			}
			layout, err := decodeCanvasLayout([]byte(*raw))
			if err != nil {
				rows.Close()
				return err
			}
			data, err := json.Marshal(layout)
			if err != nil {
				rows.Close()
				return err
			}
			*raw = string(data)
		}
		layouts = append(layouts, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range layouts {
		if _, err := tx.ExecContext(ctx, "UPDATE canvas_schemes SET layout_json=?,client_published_layout=? WHERE id=?", item.draft, item.published, item.id); err != nil {
			return err
		}
	}
	return nil
}

// PublishClientCanvas snapshots only the version the Owner reviewed.
func (r *Repository) PublishClientCanvas(ctx context.Context, scheme CanvasScheme) error {
	data, err := json.Marshal(scheme.Layout)
	if err != nil {
		return err
	}
	result, err := r.Database.ExecContext(ctx, "UPDATE canvas_schemes SET client_published_layout=?,published_at=?,published_batch_id='' WHERE id=? AND updated_at=?", string(data), timestamp(), scheme.ID, scheme.UpdatedAt)
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
