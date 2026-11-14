package node

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

// serverOrderKey is the system_settings row holding the Owner's order of the
// server list (v1.5.5 item 3). An absent row means "no saved order": the page
// then keeps the created_at order.
const serverOrderKey = "server_order"

// maxServerOrder bounds one saved order; the panel targets ten servers or so.
const maxServerOrder = 1000

var (
	ErrServerOrderInvalid  = errors.New("invalid server order: IDs must be distinct live servers, at most 1000")
	ErrServerOrderConflict = errors.New("server order changed; refresh before saving")
)

// ServerOrder is the saved order. IDs may still name servers deleted since the
// save; the page skips them and the next save drops them.
type ServerOrder struct {
	IDs       []string `json:"ids"`
	Revision  int64    `json:"revision"`
	UpdatedAt string   `json:"updatedAt"`
	UpdatedBy string   `json:"updatedBy"`
}

type ServerOrderInput struct {
	IDs              []string `json:"ids"`
	ExpectedRevision int64    `json:"expectedRevision"`
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readServerOrder(ctx context.Context, q rowQuerier) (ServerOrder, error) {
	order := ServerOrder{IDs: []string{}}
	var encoded string
	err := q.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", serverOrderKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return order, nil
	}
	if err != nil {
		return order, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&order) != nil || decoder.Decode(&struct{}{}) != io.EOF || order.Revision < 1 || order.IDs == nil || len(order.IDs) > maxServerOrder {
		return ServerOrder{IDs: []string{}}, ErrServerOrderInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, order.UpdatedAt); err != nil {
		return ServerOrder{IDs: []string{}}, ErrServerOrderInvalid
	}
	return order, nil
}

func (r *Repository) ReadServerOrder(ctx context.Context) (ServerOrder, error) {
	return readServerOrder(ctx, r.Database)
}

// SaveServerOrder replaces the order under one revision: an unchanged save keeps
// the revision and writes no audit row, a stale expectedRevision is a conflict.
func (r *Repository) SaveServerOrder(ctx context.Context, input ServerOrderInput, actor, ip string, audit DeletionAudit) (ServerOrder, error) {
	if input.IDs == nil || len(input.IDs) > maxServerOrder || input.ExpectedRevision < 0 || actor == "" {
		return ServerOrder{}, ErrServerOrderInvalid
	}
	seen := make(map[string]bool, len(input.IDs))
	for _, id := range input.IDs {
		if id == "" || seen[id] {
			return ServerOrder{}, ErrServerOrderInvalid
		}
		seen[id] = true
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return ServerOrder{}, err
	}
	defer tx.Rollback()
	if len(input.IDs) > 0 {
		var live int
		args := make([]any, len(input.IDs))
		for i, id := range input.IDs {
			args[i] = id
		}
		query := "SELECT count(*) FROM nodes WHERE deleted_at IS NULL AND id IN (?" + strings.Repeat(",?", len(args)-1) + ")"
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&live); err != nil {
			return ServerOrder{}, err
		}
		if live != len(input.IDs) {
			return ServerOrder{}, ErrServerOrderInvalid
		}
	}
	current, err := readServerOrder(ctx, tx)
	if err != nil {
		return current, err
	}
	if equalIDs(current.IDs, input.IDs) {
		return current, nil
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return current, ErrServerOrderConflict
	}
	next := ServerOrder{IDs: input.IDs, Revision: current.Revision + 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), UpdatedBy: actor}
	data, err := json.Marshal(next)
	if err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", serverOrderKey, string(data)); err != nil {
		return next, err
	}
	if err := audit.RecordTx(ctx, tx, actor, "调整服务器顺序", serverOrderKey, "成功", ip); err != nil {
		return next, err
	}
	return next, tx.Commit()
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (h *Handler) serverOrder(w http.ResponseWriter, r *http.Request) {
	order, err := h.Repository.ReadServerOrder(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, order)
}

func (h *Handler) putServerOrder(w http.ResponseWriter, r *http.Request) {
	var input ServerOrderInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	order, err := h.Repository.SaveServerOrder(r.Context(), input, auth.Actor(r.Context()).Username, r.RemoteAddr, h.Audit)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, order)
}
