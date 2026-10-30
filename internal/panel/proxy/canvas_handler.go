package proxy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

// registerCanvas serves the relay canvas drawings. They are Owner-only: a
// Viewer reads the dashboard and node status, never a topology draft.
func (h *Handler) registerCanvas(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/canvas-schemes":               h.canvasSchemes,
		"GET /api/v1/canvas-schemes/{id}":          h.canvasScheme,
		"POST /api/v1/canvas-schemes":              h.saveCanvasScheme,
		"PUT /api/v1/canvas-schemes/{id}":          h.saveCanvasScheme,
		"DELETE /api/v1/canvas-schemes/{id}":       h.deleteCanvasScheme,
		"POST /api/v1/canvas-schemes/{id}/publish": h.publishCanvasScheme,
		"POST /api/v1/canvas-schemes/{id}/plan":    h.planCanvasScheme,
	} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
}

func (h *Handler) canvasSchemes(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Service.Repository.CanvasSchemes(r.Context(), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *Handler) canvasScheme(w http.ResponseWriter, r *http.Request) {
	scheme, err := h.Service.Repository.CanvasScheme(r.Context(), r.PathValue("id"))
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	httpapi.JSON(w, 200, scheme)
}

func (h *Handler) saveCanvasScheme(w http.ResponseWriter, r *http.Request) {
	var input CanvasSchemeInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	scheme, err := h.Service.SaveCanvasScheme(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	action, status := "创建中转方案", 201
	if r.Method == http.MethodPut {
		action, status = "修改中转方案", 200
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, action, scheme.Name, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, status, scheme)
}

func (h *Handler) deleteCanvasScheme(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Repository.DeleteCanvasScheme(r.Context(), r.PathValue("id")); err != nil {
		h.failCanvas(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除中转方案", r.PathValue("id"), "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}

// publishCanvasScheme translates the drawing into the proxy objects it
// describes and publishes the nodes through the deployment batches, in
// exit → relay → entry order. The result lists each node's revision and status,
// which is what the canvas shows.
func (h *Handler) publishCanvasScheme(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.PublishCanvasScheme(r.Context(), r.PathValue("id"), auth.Actor(r.Context()).Username, h.Users)
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "发布中转方案", r.PathValue("id"), "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, result)
}

// failCanvas maps the canvas errors the generic handler cannot express: a
// scheme another canvas already changed is a conflict, not a bad request.
func (h *Handler) failCanvas(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpapi.Error(w, 404, "not_found", err.Error())
	case errors.Is(err, ErrStaleScheme):
		httpapi.Error(w, 409, "stale_scheme", "方案已在其他地方修改，请刷新")
	case errors.Is(err, ErrDuplicateSchemeName):
		httpapi.Error(w, 409, "duplicate_scheme_name", "方案名称已被其他方案使用")
	case errors.Is(err, errCanvasLayout):
		h.internal(w, err)
	default:
		httpapi.Error(w, 400, "invalid_request", err.Error())
	}
}

// Planning is Owner-only and CSRF-protected just like publication, but has no audit write.
func (h *Handler) planCanvasScheme(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Layout *CanvasLayout `json:"layout"`
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, canvasMaxLayoutBytes))
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	if len(bytes.TrimSpace(data)) != 0 {
		r.Body = io.NopCloser(bytes.NewReader(data))
		if err := httpapi.Decode(w, r, &input); err != nil {
			h.failCanvas(w, err)
			return
		}
	}
	result, err := h.Service.PlanCanvasScheme(r.Context(), r.PathValue("id"), input.Layout)
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	httpapi.JSON(w, 200, result)
}
