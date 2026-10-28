package importer

import (
	"net/http"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type Handler struct {
	Store *Store
	Auth  *auth.Handler
	Audit auth.Audit
}

func (h *Handler) Register(mux *http.ServeMux) {
	for _, route := range []struct {
		pattern string
		handler http.HandlerFunc
	}{
		{"GET /api/v1/subscription-sources", h.list},
		{"POST /api/v1/subscription-sources", h.save},
		{"PUT /api/v1/subscription-sources/{id}", h.save},
		{"DELETE /api/v1/subscription-sources/{id}", h.remove},
		{"POST /api/v1/subscription-sources/{id}/refresh", h.refresh},
		{"GET /api/v1/imported-proxies", h.items},
	} {
		mux.Handle(route.pattern, h.Auth.Require(route.handler, true))
	}
}
func (h *Handler) fail(w http.ResponseWriter) {
	httpapi.Error(w, 400, "import_unavailable", "导入操作失败，请检查输入与来源状态")
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.List(r.Context())
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) items(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.Items(r.Context())
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	var in Input
	if httpapi.Decode(w, r, &in) != nil {
		h.fail(w)
		return
	}
	key, err := h.Store.Save(r.Context(), r.PathValue("id"), in)
	if err != nil {
		h.fail(w)
		return
	}
	if !h.record(w, r, "保存订阅来源", key) {
		return
	}
	httpapi.JSON(w, 200, map[string]string{"id": key})
}
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w)
		return
	}
	if !h.record(w, r, "删除订阅来源", r.PathValue("id")) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.Refresh(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w)
		return
	}
	if !h.record(w, r, "刷新订阅来源", r.PathValue("id")) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Only opaque object IDs are audited; source names, URLs and payloads may
// contain credentials and must never enter the audit trail.
func (h *Handler) record(w http.ResponseWriter, r *http.Request, action, key string) bool {
	if h.Audit != nil {
		if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, action, key, "成功", r.RemoteAddr); err != nil {
			h.fail(w)
			return false
		}
	}
	return true
}
