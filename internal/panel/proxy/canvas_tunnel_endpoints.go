package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"proxycontrol/internal/platform/id"
)

// Retarget every reference atomically with a resource endpoint edit. Reuse a
// matching server box, or clone the old box, so unrelated connections attached
// to the old instance keep their endpoints. The scheme token changes too.
func retargetCanvasTunnel(ctx context.Context, tx *sql.Tx, tunnelID, source, target string, skipScheme ...string) error {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT s.id,s.layout_json,s.updated_at FROM canvas_schemes s,json_each(s.layout_json,'$.connections') c WHERE s.execution='server' AND json_extract(c.value,'$.tunnelId')=?`, tunnelID)
	if err != nil {
		return err
	}
	type row struct{ id, raw, stamp string }
	items := []row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.raw, &r.stamp); err != nil {
			rows.Close()
			return err
		}
		items = append(items, r)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range items {
		if len(skipScheme) > 0 && r.id == skipScheme[0] {
			continue
		}
		layout, err := decodeCanvasLayout([]byte(r.raw))
		if err != nil {
			return err
		}
		changed := false
		endpoint := func(local, ref, other string) (string, error) {
			var original CanvasInstance
			for _, i := range layout.Instances {
				if i.InstanceID == local {
					original = i
					if i.Ref == ref {
						return local, nil
					}
					break
				}
			}
			for _, i := range layout.Instances {
				if i.Ref == ref && i.InstanceID != other {
					return i.InstanceID, nil
				}
			}
			newID, err := id.New()
			if err != nil {
				return "", err
			}
			original.InstanceID = newID
			original.Ref = ref
			original.X += 40
			original.Y += 40
			layout.Instances = append(layout.Instances, original)
			return newID, nil
		}
		for n, c := range layout.Connections {
			if c.TunnelID != tunnelID {
				continue
			}
			a, err := endpoint(c.Source, source, c.Target)
			if err != nil {
				return err
			}
			b, err := endpoint(c.Target, target, a)
			if err != nil {
				return err
			}
			if a != c.Source || b != c.Target {
				changed = true
			}
			c.Source = a
			c.Target = b
			layout.Connections[n] = c
		}
		if !changed {
			continue
		}
		if err := layout.validate(); err != nil {
			return err
		}
		raw, err := json.Marshal(layout)
		if err != nil {
			return err
		}
		stamp := timestamp()
		if stamp <= r.stamp {
			stamp = nextCanvasStamp(r.stamp)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE canvas_schemes SET layout_json=?,updated_at=? WHERE id=?", string(raw), stamp, r.id); err != nil {
			return err
		}
	}
	return nil
}
