package access

import (
	"context"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	policy "proxycontrol/pkg/contracts/policy"
	"time"
)

type Handler struct {
	BaseSettings interface {
		ReadSetup(context.Context) (string, string, error)
	}
	Repository *Repository
	Auth       *auth.Handler
	Service    *Service
	Audit      interface {
		Record(context.Context, string, string, string, string, string) error
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/traffic/nodes/monthly", h.Auth.Require(http.HandlerFunc(h.monthlyNodes), false))
	mux.Handle("GET /api/v1/access-retention-job", h.Auth.Require(http.HandlerFunc(h.retentionJob), true))
	mux.Handle("GET /api/v1/access-storage-observation", h.Auth.Require(http.HandlerFunc(h.storageObservation), true))
	mux.Handle("GET /api/v1/access-records", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("GET /api/v1/access-records/blocked", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("GET /api/v1/access-records/domains", h.Auth.Require(http.HandlerFunc(h.domains), true))
	mux.Handle("GET /api/v1/traffic/reconciliation", h.Auth.Require(http.HandlerFunc(h.pendingTraffic), true))
	mux.Handle("GET /api/v1/traffic/summary", h.Auth.Require(http.HandlerFunc(h.summary), false))
	mux.Handle("GET /api/v1/traffic/hourly", h.Auth.Require(http.HandlerFunc(h.traffic), false))
	mux.Handle("GET /api/v1/traffic/daily", h.Auth.Require(http.HandlerFunc(h.traffic), false))
	mux.Handle("GET /api/v1/traffic/accounts/summary", h.Auth.Require(http.HandlerFunc(h.accountTraffic), false))
	mux.Handle("GET /api/v1/traffic/accounts/hourly", h.Auth.Require(http.HandlerFunc(h.accountTraffic), false))
	mux.Handle("GET /api/v1/traffic/accounts/daily", h.Auth.Require(http.HandlerFunc(h.accountTraffic), false))
}

func (h *Handler) pendingTraffic(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.ListPendingTraffic(r.Context(), page, size)
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *Handler) accountTraffic(w http.ResponseWriter, r *http.Request) {
	filter, err := filterRequest(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if filter.Domain != "" || filter.Allowed != nil {
		httpapi.Error(w, 400, "invalid_request", "Account traffic has no domain or outcome dimension")
		return
	}
	if auth.Actor(r.Context()).Role != "Owner" && (filter.UserID != "" || filter.DeviceID != "") {
		httpapi.Error(w, 403, "forbidden", "Detailed account traffic requires Owner")
		return
	}
	if r.URL.Path == "/api/v1/traffic/accounts/summary" {
		result, err := h.Repository.AccountSummary(r.Context(), filter)
		if err != nil {
			h.fail(w)
			return
		}
		httpapi.JSON(w, 200, result)
		return
	}
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	granularity := "hourly"
	if r.URL.Path == "/api/v1/traffic/accounts/daily" {
		granularity = "daily"
	}
	items, total, err := h.Repository.AccountTraffic(r.Context(), filter, granularity, page, size)
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func filterRequest(r *http.Request) (Filter, error) {
	now := time.Now().UTC()
	filter := Filter{From: now.Truncate(24 * time.Hour), To: now.Add(time.Minute), NodeID: r.URL.Query().Get("nodeId"), UserID: r.URL.Query().Get("userId"), DeviceID: r.URL.Query().Get("deviceId"), Domain: r.URL.Query().Get("domain")}
	for _, value := range []string{filter.NodeID, filter.UserID, filter.DeviceID} {
		if len(value) > 128 {
			return filter, ErrInvalid
		}
	}
	for key, target := range map[string]*time.Time{"from": &filter.From, "to": &filter.To} {
		if raw := r.URL.Query().Get(key); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return filter, errors.New("invalid time filter")
			}
			*target = parsed.UTC()
		}
	}
	if !filter.From.Before(filter.To) {
		return filter, errors.New("invalid time range")
	}
	if filter.Domain != "" {
		value, err := policy.Domain(filter.Domain, false)
		if err != nil {
			return filter, errors.New("domain filter must be a hostname")
		}
		filter.Domain = value
	}
	switch r.URL.Query().Get("result") {
	case "":
	case "allowed":
		value := true
		filter.Allowed = &value
	case "blocked":
		value := false
		filter.Allowed = &value
	default:
		return filter, errors.New("invalid access result filter")
	}
	if r.URL.Path == "/api/v1/access-records/blocked" {
		value := false
		filter.Allowed = &value
	}
	return filter, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	filter, err := filterRequest(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	items, total, err := h.Repository.List(r.Context(), filter, page, size)
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *Handler) domains(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	filter, err := filterRequest(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	items, total, err := h.Repository.Domains(r.Context(), filter, page, size)
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	filter, err := filterRequest(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if actor := auth.Actor(r.Context()); actor.Role != "Owner" && (filter.UserID != "" || filter.DeviceID != "" || filter.Domain != "") {
		httpapi.Error(w, 403, "forbidden", "Detailed traffic filters require Owner")
		return
	}
	result, err := h.Repository.Summary(r.Context(), filter)
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, result)
}

func (h *Handler) fail(w http.ResponseWriter) {
	httpapi.Error(w, 500, "internal_error", "Access query failed")
}

func (h *Handler) traffic(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	filter, err := filterRequest(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if auth.Actor(r.Context()).Role != "Owner" && (filter.UserID != "" || filter.DeviceID != "" || filter.Domain != "") {
		httpapi.Error(w, 403, "forbidden", "Detailed traffic filters require Owner")
		return
	}
	if filter.Domain != "" || filter.Allowed != nil {
		httpapi.Error(w, 400, "invalid_request", "Hourly and daily traffic do not have domain or outcome dimensions")
		return
	}
	granularity := "hourly"
	if r.URL.Path == "/api/v1/traffic/daily" {
		granularity = "daily"
	}
	items, total, err := h.Repository.Traffic(r.Context(), filter, granularity, page, size)
	if err != nil {
		h.fail(w)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
