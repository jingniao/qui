package deployment

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

type ChainConfiguration interface {
	ChainSnapshots(context.Context, string, proxy.AccessReader) ([]desiredstate.State, error)
}

func (h *Handler) batches(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.Batches(r.Context(), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *Handler) batch(w http.ResponseWriter, r *http.Request) {
	batch, err := h.Repository.Batch(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Error(w, 404, "not_found", "Deployment batch not found")
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, batch)
}

func (h *Handler) createBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChainID string `json:"chainId"`
		Summary string `json:"summary"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.ChainID == "" || strings.TrimSpace(body.Summary) == "" || len(body.Summary) > 512 {
		httpapi.Error(w, 400, "invalid_request", "Chain and revision summary required")
		return
	}
	planner, ok := h.Proxy.(ChainConfiguration)
	if !ok || h.Users == nil {
		httpapi.Error(w, 503, "unavailable", "Chain configuration unavailable")
		return
	}
	states, err := planner.ChainSnapshots(r.Context(), body.ChainID, h.Users)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	batch, err := h.Repository.CreateBatch(r.Context(), body.ChainID, body.Summary, auth.Actor(r.Context()).Username, states)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "创建代理链发布批次", batch.ID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 201, batch)
}

func (h *Handler) deployBatch(w http.ResponseWriter, r *http.Request) {
	h.changeBatch(w, r, false)
}

func (h *Handler) rollbackBatch(w http.ResponseWriter, r *http.Request) {
	h.changeBatch(w, r, true)
}

func (h *Handler) changeBatch(w http.ResponseWriter, r *http.Request, rollback bool) {
	batch, err := h.Repository.Batch(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Error(w, 404, "not_found", "Deployment batch not found")
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	for _, item := range batch.Items {
		n, err := h.Nodes.Get(r.Context(), item.NodeID)
		if err != nil || !n.Enabled {
			httpapi.Error(w, 409, "node_unavailable", "Batch node missing or disabled")
			return
		}
	}
	operation := "发布代理链批次"
	if batch.ChainID == "" {
		operation = "发布关联节点批次"
	}
	if rollback {
		err = h.Repository.RollbackBatch(r.Context(), batch.ID)
		operation = "回滚代理链批次"
		if batch.ChainID == "" {
			operation = "回滚关联节点批次"
		}
	} else {
		err = h.Repository.StartBatch(r.Context(), batch.ID)
	}
	if err != nil {
		httpapi.Error(w, 409, "deployment_conflict", err.Error())
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, operation, batch.ID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	batch, err = h.Repository.Batch(r.Context(), batch.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, batch)
}

func (h *Handler) rollbackNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Revision int64 `json:"revision"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil || body.Revision < 1 {
		httpapi.Error(w, 400, "invalid_request", "Successful revision required")
		return
	}
	n, err := h.Nodes.Get(r.Context(), r.PathValue("id"))
	if err != nil || !n.Enabled {
		httpapi.Error(w, 404, "not_found", "Node not found or disabled")
		return
	}
	command, err := h.Repository.RollbackNode(r.Context(), n.ID, body.Revision)
	if err != nil {
		httpapi.Error(w, 409, "deployment_conflict", err.Error())
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "回滚节点配置", n.ID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"commandId": command.CommandId, "revision": command.Revision, "status": "pending"})
}
