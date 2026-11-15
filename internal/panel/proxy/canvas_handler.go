package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	"strings"
	"time"
)

// registerCanvas serves the relay canvas drawings. They are Owner-only: a
// Viewer reads the dashboard and node status, never a topology draft.
func (h *Handler) registerCanvas(mux *http.ServeMux) {
	h.registerCanvasTunnels(mux)
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/relay-schemes/legacy-resources":           h.canvasLegacyResources,
		"POST /api/v1/relay-schemes/legacy-resources/reattach": h.reattachCanvasLegacy,
		"POST /api/v1/relay-schemes/quick/plan":                h.planQuickCanvas,
		"POST /api/v1/relay-schemes/quick":                     h.quickCanvas,
		"GET /api/v1/canvas-schemes":                           h.canvasSchemes,
		"GET /api/v1/canvas-schemes/{id}":                      h.canvasScheme,
		"GET /api/v1/canvas-schemes/{id}/status":               h.canvasSchemeStatus,
		"POST /api/v1/canvas-schemes":                          h.saveCanvasScheme,
		"PUT /api/v1/canvas-schemes/{id}":                      h.saveCanvasScheme,
		"DELETE /api/v1/canvas-schemes/{id}":                   h.deleteCanvasScheme,
		"POST /api/v1/canvas-schemes/{id}/publish":             h.publishCanvasScheme,
		"POST /api/v1/canvas-schemes/{id}/plan":                h.planCanvasScheme,
		"POST /api/v1/canvas-schemes/{id}/remove-plan":         h.planRemoveCanvasScheme,
		"POST /api/v1/canvas-schemes/{id}/remove":              h.removeCanvasScheme,
		"GET /api/v1/canvas-schemes/{id}/remove-status":        h.canvasRemovalStatus,
	} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
}

func (h *Handler) planRemoveCanvasScheme(w http.ResponseWriter, r *http.Request) {
	var input CanvasRemoveInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.failCanvas(w, err)
		return
	}
	result, err := h.Service.PlanRemoveCanvasScheme(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}

func (h *Handler) removeCanvasScheme(w http.ResponseWriter, r *http.Request) {
	var input CanvasRemoveInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "撤除请求格式无效，请提交正确的参数")
		return
	}
	id, actor, remote := r.PathValue("id"), auth.Actor(r.Context()).Username, r.RemoteAddr
	if h.Service.CanvasRemovalStatus(id).State == "removing" {
		httpapi.JSON(w, http.StatusAccepted, map[string]string{"schemeId": id, "status": "removing"})
		return
	}
	// Reject stale input without waiting for another scheme's canvasMu. The
	// executing removal still re-plans under that lock before mutating anything.
	if strings.TrimSpace(input.UpdatedAt) == "" {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "缺少 updatedAt 字段")
		return
	}
	scheme, err := h.Service.Repository.CanvasScheme(r.Context(), id)
	if err != nil {
		failure := canvasRemovalError(err)
		status := http.StatusBadRequest
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		httpapi.Error(w, status, failure.Code, failure.Message)
		return
	}
	if scheme.UpdatedAt != input.UpdatedAt {
		h.failCanvas(w, ErrStaleScheme)
		return
	}
	run, duplicate, err := h.Service.startCanvasRemoval(r.Context(), id, input, actor, h.Users, func(ctx context.Context, result CanvasRemoveResult, cause error) error {
		outcome := "成功"
		if cause != nil {
			outcome = "失败：" + canvasRemovalError(cause).Message
		}
		return h.Audit.Record(ctx, actor, "停用并移除中转方案", id, outcome, remote)
	})
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	if duplicate {
		httpapi.JSON(w, http.StatusAccepted, map[string]string{"schemeId": id, "status": "removing"})
		return
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-run.done:
	case <-timer.C:
		httpapi.JSON(w, http.StatusAccepted, map[string]string{"schemeId": id, "status": "removing"})
		return
	case <-r.Context().Done():
		return
	}
	result, err := run.result, run.err
	if err != nil {
		var conflict *CanvasRemoveConflict
		if errors.As(err, &conflict) {
			httpapi.ErrorDetails(w, http.StatusConflict, "canvas_remove_conflict", conflict.Error(), map[string]any{"details": conflict.Plan})
		} else {
			failure := canvasRemovalError(err)
			status := http.StatusBadRequest
			switch failure.Code {
			case "stale_scheme", "dependency_not_ready", "remove_apply_failed":
				status = http.StatusConflict
			case "not_found":
				status = http.StatusNotFound
			}
			httpapi.Error(w, status, failure.Code, failure.Message)
		}
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}

func (h *Handler) canvasRemovalStatus(w http.ResponseWriter, r *http.Request) {
	httpapi.JSON(w, http.StatusOK, h.Service.CanvasRemovalStatus(r.PathValue("id")))
}

func (h *Handler) canvasSchemes(w http.ResponseWriter, r *http.Request) {
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
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
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
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
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
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
	case errors.Is(err, ErrRelayDependency):
		httpapi.Error(w, 409, "dependency_not_ready", err.Error())
	case errors.Is(err, ErrRemovalApply):
		httpapi.Error(w, 409, "remove_apply_failed", err.Error())
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
