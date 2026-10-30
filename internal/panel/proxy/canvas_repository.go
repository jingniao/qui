package proxy

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"proxycontrol/internal/platform/id"
	"time"
)

// CanvasScheme is one stored relay drawing.
type CanvasScheme struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Execution        string       `json:"execution"`
	Layout           CanvasLayout `json:"layout"`
	CreatedAt        string       `json:"createdAt"`
	UpdatedAt        string       `json:"updatedAt"`
	PublishedAt      string       `json:"publishedAt"`
	PublishedBatchID string       `json:"publishedBatchId"`
}

// CanvasSchemeInput is the body of a create or an update. UpdatedAt is the
// optimistic-concurrency token: an update must send back the value the last
// read returned.
type CanvasSchemeInput struct {
	Name      string       `json:"name"`
	Execution string       `json:"execution"`
	Layout    CanvasLayout `json:"layout"`
	UpdatedAt string       `json:"updatedAt"`
}

const canvasSchemeColumns = "id,name,execution,layout_json,created_at,updated_at,published_at,published_batch_id"

// nextCanvasStamp keeps updated_at strictly increasing. Two saves inside one
// clock tick would otherwise return the same token and let a stale canvas win.
func nextCanvasStamp(current string) string {
	parsed, err := time.Parse("2006-01-02T15:04:05.000000000Z", current)
	if err != nil {
		return timestamp()
	}
	return parsed.Add(time.Nanosecond).UTC().Format("2006-01-02T15:04:05.000000000Z")
}

// errCanvasLayout marks a stored row that is not the typed layout. That is a
// server-side corruption, not a bad request, so it is reported as one.
var errCanvasLayout = errors.New("stored canvas layout is invalid")

// decodeCanvasLayout reads stored JSON strictly: a row is only trusted when it
// is exactly the typed layout, never arbitrary JSON.
func decodeCanvasLayout(data []byte) (CanvasLayout, error) {
	var layout CanvasLayout
	if len(data) == 0 || len(data) > canvasMaxLayoutBytes {
		return layout, errCanvasLayout
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&layout); err != nil {
		return CanvasLayout{}, fmt.Errorf("%w: %v", errCanvasLayout, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return CanvasLayout{}, errCanvasLayout
	}
	return layout, nil
}

func scanCanvasScheme(row interface{ Scan(...any) error }) (CanvasScheme, error) {
	var scheme CanvasScheme
	var stored string
	if err := row.Scan(&scheme.ID, &scheme.Name, &scheme.Execution, &stored, &scheme.CreatedAt, &scheme.UpdatedAt, &scheme.PublishedAt, &scheme.PublishedBatchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CanvasScheme{}, ErrNotFound
		}
		return CanvasScheme{}, err
	}
	layout, err := decodeCanvasLayout([]byte(stored))
	if err != nil {
		return CanvasScheme{}, err
	}
	scheme.Layout = layout
	return scheme, nil
}

func (r *Repository) CanvasScheme(ctx context.Context, schemeID string) (CanvasScheme, error) {
	scheme, err := scanCanvasScheme(r.Database.QueryRowContext(ctx, "SELECT "+canvasSchemeColumns+" FROM canvas_schemes WHERE id=?", schemeID))
	if err == nil {
		scheme.Layout, err = r.resolveCanvasLayout(ctx, scheme.Execution, scheme.Layout)
	}
	return scheme, err
}

func (r *Repository) CanvasSchemes(ctx context.Context, page, size int) ([]CanvasScheme, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM canvas_schemes").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT "+canvasSchemeColumns+" FROM canvas_schemes ORDER BY created_at,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []CanvasScheme{}
	for rows.Next() {
		item, err := scanCanvasScheme(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	for n := range items {
		var e error
		items[n].Layout, e = r.resolveCanvasLayout(ctx, items[n].Execution, items[n].Layout)
		if e != nil {
			return nil, 0, e
		}
	}
	return items, total, nil
}

// SaveCanvasScheme creates or updates one scheme. An update is conditional on
// the stored updated_at, so two canvases open on the same scheme cannot
// overwrite each other silently.
func (r *Repository) SaveCanvasScheme(ctx context.Context, schemeID string, input CanvasSchemeInput) (CanvasScheme, error) {
	if err := r.ReconcileCanvasTunnels(ctx); err != nil {
		return CanvasScheme{}, err
	}
	input.Layout.Connections = append([]CanvasConnection(nil), input.Layout.Connections...)
	data, err := json.Marshal(input.Layout)
	if err != nil {
		return CanvasScheme{}, err
	}
	if len(data) > canvasMaxLayoutBytes {
		return CanvasScheme{}, errors.New("canvas layout exceeds the size limit")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return CanvasScheme{}, err
	}
	defer tx.Rollback()
	stamp := timestamp()
	creating := schemeID == ""
	if creating {
		schemeID, err = id.New()
		if err != nil {
			return CanvasScheme{}, err
		}
	}
	if err := saveInlineTunnels(ctx, tx, schemeID, &input); err != nil {
		return CanvasScheme{}, err
	}
	data, err = json.Marshal(input.Layout)
	if err != nil {
		return CanvasScheme{}, err
	}
	if len(data) > canvasMaxLayoutBytes {
		return CanvasScheme{}, errors.New("canvas layout exceeds size limit")
	}
	if creating {
		if _, err := tx.ExecContext(ctx, "INSERT INTO canvas_schemes(id,name,execution,layout_json,created_at,updated_at) VALUES(?,?,?,?,?,?)", schemeID, input.Name, input.Execution, string(data), stamp, stamp); err != nil {
			return CanvasScheme{}, err
		}
	} else {
		var current string
		err := tx.QueryRowContext(ctx, "SELECT updated_at FROM canvas_schemes WHERE id=?", schemeID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return CanvasScheme{}, ErrNotFound
		}
		if err != nil {
			return CanvasScheme{}, err
		}
		if input.UpdatedAt == "" || input.UpdatedAt != current {
			return CanvasScheme{}, ErrStaleScheme
		}
		if stamp <= current {
			stamp = nextCanvasStamp(current)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE canvas_schemes SET name=?,execution=?,layout_json=?,updated_at=? WHERE id=? AND updated_at=?", input.Name, input.Execution, string(data), stamp, schemeID, current); err != nil {
			return CanvasScheme{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return CanvasScheme{}, err
	}
	return r.CanvasScheme(ctx, schemeID)
}

// DeleteCanvasScheme removes the drawing only. Every proxy object a scheme may
// describe stays exactly as it is.
func (r *Repository) DeleteCanvasScheme(ctx context.Context, schemeID string) error {
	if err := r.ReconcileCanvasTunnels(ctx); err != nil {
		return err
	}
	result, err := r.Database.ExecContext(ctx, "DELETE FROM canvas_schemes WHERE id=?", schemeID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// canvasNodePresent reports whether a live node still exists. A logically
// deleted node keeps its row, so deleted_at decides.
func (r *Repository) canvasNodePresent(ctx context.Context, nodeID string) (bool, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM nodes WHERE id=? AND deleted_at IS NULL", nodeID).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

// canvasInboundPresent reports whether an inbound still exists on a live node.
func (r *Repository) canvasInboundPresent(ctx context.Context, inboundID string) (bool, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM inbounds i JOIN nodes n ON n.id=i.node_id WHERE i.id=? AND n.deleted_at IS NULL", inboundID).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}
