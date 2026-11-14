package proxy

import "context"

// CanvasEntryNames returns display names only for entries created by a canvas.
// Binding ownership remains authoritative even when a scheme is detached.
// Reused inbounds retain the subscription's ordinary naming convention.
func (r *Repository) CanvasEntryNames(ctx context.Context) (map[string]string, error) {
	rows, err := r.Database.QueryContext(ctx, `SELECT i.id,i.name
		FROM canvas_entry_bindings b JOIN inbounds i ON i.id=b.inbound_id
		WHERE b.mode='create'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}
