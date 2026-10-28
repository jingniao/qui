package portal

import (
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) devicesOnline(w http.ResponseWriter, r *http.Request) {
	u, err := h.Service.Users.Get(r.Context(), current(r).CurrentUser.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	v, err := h.Service.Devices.View(r.Context(), u)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, v)
}
