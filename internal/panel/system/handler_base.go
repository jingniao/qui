package system

import (
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) base(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Repository.ReadBase(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
}
func (h *Handler) saveBase(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit identity unavailable")
		return
	}
	var input BaseInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	settings, changed, err := h.Repository.SaveBase(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	if changed && h.BaseChanged != nil {
		h.BaseChanged()
	}
	httpapi.JSON(w, 200, settings)
}
