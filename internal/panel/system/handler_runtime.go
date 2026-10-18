package system

import (
	"context"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

type RuntimeProvider func(context.Context) (any, error)

func (h *Handler) runtimePolicy(w http.ResponseWriter, r *http.Request) {
	value, err := h.Repository.ReadRuntimeSettings(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}
func (h *Handler) saveRuntimePolicy(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit unavailable")
		return
	}
	var input RuntimeInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	value, changed, err := h.Repository.SaveRuntimeSettings(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	if changed && h.BaseChanged != nil {
		h.BaseChanged()
	}
	httpapi.JSON(w, 200, value)
}
func (h *Handler) runtime(w http.ResponseWriter, r *http.Request) {
	if h.Runtime == nil {
		httpapi.Error(w, 503, "runtime_unavailable", "Runtime information unavailable")
		return
	}
	value, err := h.Runtime(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}
