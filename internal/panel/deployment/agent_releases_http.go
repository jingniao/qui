package deployment

import (
	"context"
	"net/http"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type AgentReleaseAudit interface {
	Record(ctx context.Context, operator, action, target, result, ip string) error
}

type AgentReleaseHandler struct {
	Releases *AgentReleases
	Auth     *auth.Handler
	Audit    AgentReleaseAudit
}

func (h *AgentReleaseHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/agent-releases", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/agent-releases/{version}/import", h.Auth.Require(http.HandlerFunc(h.importRelease), true))
}

func (h *AgentReleaseHandler) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	refresh := r.URL.Query().Get("refresh") == "1"
	items, err := h.Releases.List(r.Context(), refresh)
	if err != nil {
		httpapi.Error(w, 502, "agent_release_lookup_failed", "Official release information is unavailable")
		return
	}
	httpapi.JSON(w, 200, items)
}

func (h *AgentReleaseHandler) importRelease(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	version := r.PathValue("version")
	if !versionFormalPattern.MatchString(version) {
		httpapi.Error(w, 400, "invalid_request", "Invalid version format")
		return
	}

	actor := "unknown"
	if cur := auth.Actor(r.Context()); cur != nil && cur.Username != "" {
		actor = cur.Username
	}

	entries, err := h.Releases.Import(r.Context(), version, actor, r.RemoteAddr)
	if err != nil {
		httpapi.Error(w, 502, "agent_release_import_failed", "Agent release import failed")
		return
	}

	if h.Audit != nil {
		_ = h.Audit.Record(r.Context(), actor, "导入 Agent 版本", version, "成功", r.RemoteAddr)
	}

	httpapi.JSON(w, 200, entries)
}
