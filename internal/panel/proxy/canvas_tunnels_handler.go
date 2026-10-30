package proxy

import (
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) registerCanvasTunnels(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/v1/canvas-tunnels":                h.canvasTunnels,
		"POST /api/v1/canvas-tunnels":               h.saveCanvasTunnel,
		"PUT /api/v1/canvas-tunnels/{id}":           h.saveCanvasTunnel,
		"DELETE /api/v1/canvas-tunnels/{id}":        h.deleteCanvasTunnel,
		"GET /api/v1/canvas-tunnels/legacy-objects": h.legacyCanvasObjects,
		"POST /api/v1/canvas-tunnels/{id}/adopt":    h.adoptCanvasObjects,
	} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
}
func (h *Handler) canvasTunnels(w http.ResponseWriter, r *http.Request) {
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
	items, err := h.Service.Repository.CanvasTunnels(r.Context())
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (h *Handler) tunnelAudit(w http.ResponseWriter, r *http.Request, action, target string) bool {
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, action, target, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return false
	}
	return true
}
func (h *Handler) saveCanvasTunnel(w http.ResponseWriter, r *http.Request) {
	var input CanvasTunnelInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	t, err := h.Service.SaveCanvasTunnel(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	status := 200
	action := "修改共享隧道"
	if r.Method == "POST" {
		status = 201
		action = "创建共享隧道"
	}
	if !h.tunnelAudit(w, r, action, t.ID) {
		return
	}
	httpapi.JSON(w, status, t)
}
func (h *Handler) deleteCanvasTunnel(w http.ResponseWriter, r *http.Request) {
	err := h.Service.DeleteCanvasTunnel(r.Context(), r.PathValue("id"))
	if err != nil {
		var inUse *TunnelInUseError
		if errors.As(err, &inUse) {
			httpapi.ErrorDetails(w, 409, "tunnel_in_use", err.Error(), map[string]any{"schemes": inUse.Schemes})
			return
		}
		h.failCanvas(w, err)
		return
	}
	if !h.tunnelAudit(w, r, "删除共享隧道", r.PathValue("id")) {
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) legacyCanvasObjects(w http.ResponseWriter, r *http.Request) {
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
	items, err := h.Service.Repository.LegacyCanvasObjects(r.Context())
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": len(items)})
}
func (h *Handler) adoptCanvasObjects(w http.ResponseWriter, r *http.Request) {
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
	var input struct {
		Objects []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"objects"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	objects := []CanvasTunnelObject{}
	for _, o := range input.Objects {
		objects = append(objects, CanvasTunnelObject{Kind: o.Kind, ID: o.ID})
	}
	if err := h.Service.Repository.AdoptCanvasObjects(r.Context(), r.PathValue("id"), objects); err != nil {
		if errors.Is(err, ErrCanvasObjectOwned) {
			httpapi.Error(w, 409, "object_owned", err.Error())
		} else {
			h.failCanvas(w, err)
		}
		return
	}
	if !h.tunnelAudit(w, r, "认领旧画布对象", r.PathValue("id")) {
		return
	}
	w.WriteHeader(204)
}
