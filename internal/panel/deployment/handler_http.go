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
	mux.Handle("POST /api/v1/nodes/{id}/publish", h.Auth.Require(http.HandlerFunc(h.publishNode), true))
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	nodeID := strings.TrimSpace(r.URL.Query().Get("nodeId"))
	if nodeID != "" && len(nodeID) > 128 {
		httpapi.Error(w, 400, "invalid_request", "Invalid nodeId")
		return
	}
	var items []Revision
	var total int
	if nodeID != "" {
		items, total, err = h.Repository.ListForNode(r.Context(), nodeID, page, size)
	} else {
		items, total, err = h.Repository.List(r.Context(), page, size)
	}
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
	// A user credential that exists only inside a revision cannot survive: the
	// proxy renderer and every automatic quota revision read user credentials
	// from the users and inbounds business data, so the next publication would
	// silently drop it. Raw revisions therefore carry internal credentials only.
	for _, credential := range body.State.Credentials {
		if !credential.Internal {
			httpapi.Error(w, 400, "unmanaged_user_credentials", "Raw revisions cannot carry user credentials; create devices under Users and publish the node's inbounds or the user access draft instead")
			return
		}
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
	if err := h.deployRevision(r.Context(), id); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "发布配置版本", strconv.FormatInt(id, 10), "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"revision": id, "status": "pending"})
}

// publishNode creates the node's draft from its business data and publishes it
// in one step. The pages that only ever created a draft now reach the node
// without a second call, while the draft they used to leave behind is still
// what gets published, so a refused deploy names it in the error and the Owner
// can retry it from the node's configuration history.
func (h *Handler) publishNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	var body struct {
		Summary string `json:"summary"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Summary) == "" || len(body.Summary) > 512 {
		httpapi.Error(w, 400, "invalid_request", "Revision summary required")
		return
	}
	actor := auth.Actor(r.Context()).Username
	revision, err := h.createProxyRevision(r.Context(), nodeID, body.Summary, actor)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.deployRevision(r.Context(), revision.ID); err != nil {
		h.fail(w, withRevision(err, revision.ID))
		return
	}
	if err := h.Audit.Record(r.Context(), actor, "发布节点配置", nodeID, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"revision": revision.ID, "status": "pending"})
}

// deployRevision hands a stored revision to the node's Agent. The revision
// endpoint and the one-step publication share it, so both answer a refused
// dependency or a busy node identically.
func (h *Handler) deployRevision(ctx context.Context, id int64) error {
	state, err := h.Repository.State(ctx, id)
	if err != nil {
		return &apiFailure{Status: 404, Code: "not_found", Message: "Revision not found"}
	}
	if err := h.checkRelayTargets(ctx, state); err != nil {
		return &apiFailure{Status: 409, Code: "dependency_not_ready", Message: err.Error()}
	}
	if err := h.Repository.Deploy(ctx, id); err != nil {
		return &apiFailure{Status: 409, Code: "deployment_conflict", Message: err.Error()}
	}
	return nil
}

// apiFailure is a failure a shared helper reports with the status and error code
// of the endpoint that used to own the logic. Details travel inside the error
// body, the same way a refused node deletion reports its blockers.
type apiFailure struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (f *apiFailure) Error() string { return f.Message }

// withRevision names the draft a refused one-step publication left behind, so
// the caller can offer to retry it instead of saving the configuration again.
func withRevision(err error, revision int64) error {
	var failure *apiFailure
	if !errors.As(err, &failure) {
		return err
	}
	details := map[string]any{"revision": revision}
	for key, value := range failure.Details {
		details[key] = value
	}
	return &apiFailure{Status: failure.Status, Code: failure.Code, Message: failure.Message, Details: details}
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	var failure *apiFailure
	if errors.As(err, &failure) {
		if len(failure.Details) == 0 {
			httpapi.Error(w, failure.Status, failure.Code, failure.Message)
		} else {
			httpapi.ErrorDetails(w, failure.Status, failure.Code, failure.Message, failure.Details)
		}
		return
	}
	if errors.Is(err, ErrDependency) {
		httpapi.Error(w, 409, "dependency_not_ready", err.Error())
		return
	}
	h.Auth.Logger.Error("deployment failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "Deployment operation failed")
}
