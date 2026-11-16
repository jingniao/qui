package system

import (
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) realityPresets(w http.ResponseWriter, r *http.Request) {
	value, err := h.Repository.ReadRealityPresets(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}

func (h *Handler) saveRealityPresets(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit identity unavailable")
		return
	}
	var input RealityPresetsInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_settings", err.Error())
		return
	}
	value, err := h.Repository.SaveRealityPresets(r.Context(), input, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, value)
}
