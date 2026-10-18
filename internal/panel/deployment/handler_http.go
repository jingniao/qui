package deployment

import (
	"context"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
	"strings"
)

type Handler struct {
	Repository *Repository
	Nodes      *node.Repository
	Auth       *auth.Handler
	Audit      auth.Audit
	Users      UserAccess
	Proxy      ProxyConfiguration
	Policies   interface {
		ForNode(context.Context, string) ([]desiredstate.Policy, error)
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/runtime-topology", h.Auth.Require(http.HandlerFunc(h.publishedTopology), true))
	mux.Handle("POST /api/v1/nodes/{id}/restart-core", h.Auth.Require(http.HandlerFunc(h.restartNode), true))
	mux.Handle("GET /api/v1/nodes/{id}/restart-core", h.Auth.Require(http.HandlerFunc(h.restartTasks), true))
	mux.Handle("GET /api/v1/revisions", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("GET /api/v1/revisions/{id}/diff", h.Auth.Require(http.HandlerFunc(h.diff), true))
	mux.Handle("POST /api/v1/revisions", h.Auth.Require(http.HandlerFunc(h.create), true))
	mux.Handle("POST /api/v1/revisions/user-access", h.Auth.Require(http.HandlerFunc(h.userAccess), true))
	mux.Handle("POST /api/v1/revisions/proxy", h.Auth.Require(http.HandlerFunc(h.proxyRevision), true))
	mux.Handle("POST /api/v1/revisions/{id}/deploy", h.Auth.Require(http.HandlerFunc(h.deploy), true))
	mux.Handle("GET /api/v1/deployment-batches", h.Auth.Require(http.HandlerFunc(h.batches), true))
	mux.Handle("GET /api/v1/deployment-batches/{id}", h.Auth.Require(http.HandlerFunc(h.batch), true))
	mux.Handle("POST /api/v1/deployment-batches", h.Auth.Require(http.HandlerFunc(h.createBatch), true))
	mux.Handle("POST /api/v1/deployment-batches/relay", h.Auth.Require(http.HandlerFunc(h.createRelayBatch), true))
	mux.Handle("POST /api/v1/deployment-batches/{id}/deploy", h.Auth.Require(http.HandlerFunc(h.deployBatch), true))
	mux.Handle("POST /api/v1/deployment-batches/{id}/rollback", h.Auth.Require(http.HandlerFunc(h.rollbackBatch), true))
	mux.Handle("POST /api/v1/nodes/{id}/rollback", h.Auth.Require(http.HandlerFunc(h.rollbackNode), true))
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.List(r.Context(), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Summary string             `json:"summary"`
		State   desiredstate.State `json:"state"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Summary) == "" || len(body.Summary) > 512 {
		httpapi.Error(w, 400, "invalid_request", "Revision summary required")
		return
	}
	body.State.Revision = 1
	if err := body.State.Validate(); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	n, err := h.Nodes.Get(r.Context(), body.State.NodeID)
	if err != nil || !n.Enabled {
		httpapi.Error(w, 400, "invalid_request", "Target node missing or disabled")
		return
	}
	revision, err := h.Repository.Create(r.Context(), body.State, auth.Actor(r.Context()).Username, body.Summary)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "创建配置版本", strconv.FormatInt(revision.ID, 10), "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 201, revision)
}
func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		httpapi.Error(w, 400, "invalid_request", "Invalid revision ID")
		return
	}
	state, err := h.Repository.State(r.Context(), id)
	if err != nil {
		httpapi.Error(w, 404, "not_found", "Revision not found")
		return
	}
	if err := h.checkRelayTargets(r.Context(), state); err != nil {
		httpapi.Error(w, 409, "dependency_not_ready", err.Error())
		return
	}
	if err := h.Repository.Deploy(r.Context(), id); err != nil {
		httpapi.Error(w, 409, "deployment_conflict", err.Error())
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "发布配置版本", strconv.FormatInt(id, 10), "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"revision": id, "status": "pending"})
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrDependency) {
		httpapi.Error(w, 409, "dependency_not_ready", err.Error())
		return
	}
	h.Auth.Logger.Error("deployment failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "Deployment operation failed")
}
