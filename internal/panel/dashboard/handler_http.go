package dashboard

import (
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/httpapi"
)

type Handler struct {
	Service       *Service
	ExchangeRates *ExchangeRateService
	Auth          *auth.Handler
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/dashboard/base", h.Auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view, err := h.Service.Base(r.Context(), auth.Actor(r.Context()).Role == "Owner")
		if err != nil {
			httpapi.Error(w, http.StatusInternalServerError, "dashboard_base_failed", err.Error())
			return
		}
		httpapi.JSON(w, http.StatusOK, view)
	}), false))
	mux.Handle("GET /api/v1/dashboard/statistics", h.Auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.statistics(w, r, "")
	}), false))
	mux.Handle("GET /api/v1/statistics/nodes", h.Auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.statistics(w, r, r.URL.Query().Get("nodeId"))
	}), false))
	mux.Handle("GET /api/v1/dashboard", h.Auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		view, err := h.Service.Read(r.Context(), auth.Actor(r.Context()).Role == "Owner")
		if err != nil {
			httpapi.Error(w, 500, "internal_error", "Dashboard query failed")
			return
		}
		httpapi.JSON(w, 200, view)
	}), false))
	mux.Handle("GET /api/v1/dashboard/exchange-rates", h.Auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rates, err := h.ExchangeRates.Read(r.Context())
		if err != nil {
			httpapi.Error(w, http.StatusBadGateway, "exchange_rates_unavailable", err.Error())
			return
		}
		httpapi.JSON(w, http.StatusOK, rates)
	}), false))
}

func (h *Handler) statistics(w http.ResponseWriter, r *http.Request, id string) {
	view, err := h.Service.Statistics.Read(r.Context(), id)
	if errors.Is(err, node.ErrNotFound) {
		httpapi.Error(w, http.StatusNotFound, "node_not_found", "Node not found")
		return
	}
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "statistics_unavailable", err.Error())
		return
	}
	httpapi.JSON(w, http.StatusOK, view)
}
