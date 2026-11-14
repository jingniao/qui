package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// ReconcileImportRefsTx rewrites draft and published identities without publishing resources.
func ReconcileImportRefsTx(ctx context.Context, tx *sql.Tx, resolve func(oldID string) (newID string, exists bool)) ([]string, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id,layout_json,client_published_layout,updated_at FROM canvas_schemes ORDER BY id")
	if err != nil {
		return nil, err
	}
	type storedLayout struct{ id, draft, published, stamp string }
	var layouts []storedLayout
	for rows.Next() {
		var item storedLayout
		if err := rows.Scan(&item.id, &item.draft, &item.published, &item.stamp); err != nil {
			rows.Close()
			return nil, err
		}
		layouts = append(layouts, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	report := []string{}
	for _, item := range layouts {
		changed := false
		for _, snapshot := range []struct {
			column string
			raw    *string
		}{{"layout_json", &item.draft}, {"client_published_layout", &item.published}} {
			if *snapshot.raw == "" {
				continue
			}
			layout, err := decodeCanvasLayout([]byte(*snapshot.raw))
			if err != nil {
				return nil, err
			}
			rewritten := false
			for index := range layout.Instances {
				instance := &layout.Instances[index]
				oldID, imported := strings.CutPrefix(instance.Ref, "import:")
				if !imported {
					continue
				}
				newID, exists := resolve(oldID)
				if !exists {
					report = append(report, fmt.Sprintf("canvas_schemes scheme=%s snapshot=%s instance=%s ref=%s: unresolved reference retained", item.id, snapshot.column, instance.InstanceID, instance.Ref))
					continue
				}
				if newID != oldID {
					instance.Ref = "import:" + newID
					rewritten = true
				}
			}
			if rewritten {
				raw, err := json.Marshal(layout)
				if err != nil {
					return nil, err
				}
				*snapshot.raw = string(raw)
				changed = true
			}
		}
		if changed {
			stamp := timestamp()
			if stamp <= item.stamp {
				stamp = nextCanvasStamp(item.stamp)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE canvas_schemes SET layout_json=?,client_published_layout=?,updated_at=? WHERE id=?", item.draft, item.published, stamp, item.id); err != nil {
				return nil, err
			}
		}
	}
	return report, nil
}
