package user

import (
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) devicesOnline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	u, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if r.Method == http.MethodPost && h.Devices != nil {
		h.Devices.Reset(r.Context(), u.ID)
		h.Devices.Changed()
		if err := h.audit(r, "重置设备准入顺序", u.Username); err != nil {
			h.internal(w, err)
			return
		}
	}
	v, err := h.Devices.View(r.Context(), u)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, v)
}
