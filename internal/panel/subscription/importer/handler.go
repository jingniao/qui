package importer

import (
	"context"
	"database/sql"
	"errors"
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
	key, result, err := h.Store.Save(r.Context(), r.PathValue("id"), in)
	if err != nil {
		h.failure(w, result, err)
		return
	}
	if !h.record(w, r, "保存订阅来源", key) {
		return
	}
	// The ID stays the primary field. A pasted import is analysed by the same
	// pipeline as a fetch, so its bounded result travels with the saved source.
	body := map[string]any{"id": key}
	if result.AttemptedAt != "" {
		body["result"] = result
	}
	httpapi.JSON(w, http.StatusOK, body)
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
	result, err := h.Store.Refresh(r.Context(), r.PathValue("id"))
	if err != nil {
		h.failure(w, result, err)
		return
	}
	if !h.record(w, r, "刷新订阅来源", r.PathValue("id")) {
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}

// failure answers with the panel's own status, code and fixed text, and carries the
// bounded result when the attempt finished, so the page can show counts and reasons
// instead of a bare message. The storage error text is never sent: it can name a
// file, a lock or a row.
func (h *Handler) failure(w http.ResponseWriter, result Result, err error) {
	status, code, message := failureResponse(result, err)
	if classified(result) {
		httpapi.ErrorDetails(w, status, code, message, map[string]any{"result": result})
		return
	}
	httpapi.Error(w, status, code, message)
}

// classified reports an attempt the store finished and classified. Every business
// result names its outcome, stage, code and the fixed text of that code. An attempt
// that never finished — the request was cancelled or expired while the fetch was
// still running, or a content source was refused before any attempt — has none of
// them, so the answer stays the panel's own message and carries no provisional
// counts, reasons or attempt time.
func classified(result Result) bool {
	return result.Outcome != "" && result.Stage != "" && result.Code != "" && result.Message != ""
}

// failureResponse is the one mapping from a refused or failed attempt to its HTTP
// status, error code and fixed text, shared by the save and the refresh route.
func failureResponse(result Result, err error) (int, string, string) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return http.StatusNotFound, "import_source_missing", "订阅来源不存在或已被删除"
	case errors.Is(err, ErrContentSource):
		return http.StatusBadRequest, "import_unavailable", "内容导入来源没有订阅链接，不能刷新"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The task did not commit, so the source keeps its last result.
		return http.StatusServiceUnavailable, "import_unavailable", "刷新已取消，来源状态保持不变"
	case result.Stage == stageCommit:
		// A storage failure keeps the previous status and cache, which the result
		// reports; the message says so instead of naming the storage.
		return http.StatusInternalServerError, "import_commit_failed", result.Message
	case errors.Is(err, ErrStore):
		// The source's own sealed payload cannot be opened: a local failure, never a
		// fetch, and the result already states that nothing changed.
		return http.StatusInternalServerError, "import_source_unreadable", result.Message
	case errors.Is(err, ErrInput):
		if result.Message != "" {
			return http.StatusBadRequest, "import_invalid_input", result.Message
		}
		return http.StatusBadRequest, "import_invalid_input", "导入来源的名称、刷新间隔或内容无效"
	case errors.Is(err, ErrFetch):
		if result.Code == "source_timeout" {
			return http.StatusGatewayTimeout, "import_refresh_failed", result.Message
		}
		return http.StatusBadGateway, "import_refresh_failed", result.Message
	case errors.Is(err, ErrParse):
		// The document was read but nothing in it can be imported.
		return http.StatusUnprocessableEntity, "import_no_entries", result.Message
	}
	return http.StatusInternalServerError, "import_unavailable", "导入操作失败，请检查输入与来源状态"
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
