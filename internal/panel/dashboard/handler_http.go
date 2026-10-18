package dashboard

import (
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type Handler struct {
	Service *Service
	Auth    *auth.Handler
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/dashboard", h.Auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view, err := h.Service.Read(r.Context(), auth.Actor(r.Context()).Role == "Owner")
		if err != nil {
			httpapi.Error(w, 500, "internal_error", "Dashboard query failed")
			return
		}
		httpapi.JSON(w, 200, view)
	}), false))
}
