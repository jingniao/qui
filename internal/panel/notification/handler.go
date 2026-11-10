package notification

import (
	"net/http"
	"strings"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type Handler struct {
	Repository *Repository
	Worker     *Worker
	Auth       *auth.Handler
	Audit      auth.Audit
}

func (h *Handler) recordAudit(r *http.Request, action, target string) {
	if h.Audit == nil {
		return
	}
	actor := auth.Actor(r.Context())
	username := ""
	if actor != nil {
		username = actor.Username
	}
	_ = h.Audit.Record(r.Context(), username, action, target, "成功", r.RemoteAddr)
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/notifications", h.Auth.Require(http.HandlerFunc(h.listNotifications), false))
	mux.Handle("POST /api/v1/notifications/read-all", h.Auth.Require(http.HandlerFunc(h.readAllNotifications), true))
	mux.Handle("GET /api/v1/notification-rules", h.Auth.Require(http.HandlerFunc(h.listRules), true))
	mux.Handle("PUT /api/v1/notification-rules", h.Auth.Require(http.HandlerFunc(h.updateRules), true))
	mux.Handle("GET /api/v1/notification-channels", h.Auth.Require(http.HandlerFunc(h.listChannels), true))
	mux.Handle("POST /api/v1/notification-channels", h.Auth.Require(http.HandlerFunc(h.createChannel), true))
	mux.Handle("PUT /api/v1/notification-channels/{id}", h.Auth.Require(http.HandlerFunc(h.updateChannel), true))
	mux.Handle("DELETE /api/v1/notification-channels/{id}", h.Auth.Require(http.HandlerFunc(h.deleteChannel), true))
	mux.Handle("POST /api/v1/notification-channels/{id}/test", h.Auth.Require(http.HandlerFunc(h.testChannel), true))
}

func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_pagination", err.Error())
		return
	}

	actor := auth.Actor(r.Context())
	isViewer := actor != nil && actor.Role == "Viewer"
	unreadOnly := r.URL.Query().Get("unread") == "1"

	items, total, err := h.Repository.List(r.Context(), isViewer, unreadOnly, page, size)
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	if items == nil {
		items = []Notification{}
	}

	unreadCount, err := h.Repository.UnreadCount(r.Context(), isViewer)
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}

	httpapi.JSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"unread": unreadCount,
	})
}

func (h *Handler) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if err := h.Repository.MarkAllRead(r.Context()); err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.Repository.Rules(r.Context())
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	if rules == nil {
		rules = []Rule{}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"items": rules,
	})
}

type updateRulesRequest struct {
	Items []Rule `json:"items"`
}

func (h *Handler) updateRules(w http.ResponseWriter, r *http.Request) {
	var req updateRulesRequest
	if err := httpapi.Decode(w, r, &req); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	for _, rule := range req.Items {
		if err := rule.Validate(); err != nil {
			httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	if err := h.Repository.SaveRules(r.Context(), req.Items); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	h.recordAudit(r, "修改通知规则", "notification_rules")
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"items": req.Items,
	})
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.Repository.Channels(r.Context())
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	if channels == nil {
		channels = []Channel{}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"items": channels,
	})
}

type createChannelRequest struct {
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
	Enabled bool           `json:"enabled"`
	Config  *ChannelConfig `json:"config"`
}

func validateChannelConfig(kind string, cfg *ChannelConfig) bool {
	return cfg != nil && cfg.Validate(kind) == nil
}

func (h *Handler) createChannel(w http.ResponseWriter, r *http.Request) {
	var req createChannelRequest
	if err := httpapi.Decode(w, r, &req); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || (req.Kind != "telegram" && req.Kind != "webhook") {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "invalid channel name or kind")
		return
	}
	if !validateChannelConfig(req.Kind, req.Config) {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "invalid channel configuration")
		return
	}

	ch, err := h.Repository.SaveChannel(r.Context(), Channel{
		Kind:    req.Kind,
		Name:    req.Name,
		Enabled: req.Enabled,
	}, req.Config)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	h.recordAudit(r, "修改通知渠道", ch.Name)
	httpapi.JSON(w, http.StatusCreated, ch)
}

type updateChannelRequest struct {
	Name    string               `json:"name"`
	Enabled bool                 `json:"enabled"`
	Config  *updateChannelConfig `json:"config"`
}

type updateChannelConfig struct {
	ChannelConfig
	ClearSecret bool `json:"clearSecret"`
}

func (h *Handler) updateChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateChannelRequest
	if err := httpapi.Decode(w, r, &req); err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}

	channels, err := h.Repository.Channels(r.Context())
	if err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	var targetKind string
	var targetFormat string
	found := false
	for _, ch := range channels {
		if ch.ID == id {
			targetKind = ch.Kind
			targetFormat = ch.Format
			found = true
			break
		}
	}
	if !found {
		httpapi.Error(w, http.StatusNotFound, "not_found", "channel not found")
		return
	}

	var config *ChannelConfig
	if req.Config != nil {
		incoming := req.Config
		if (targetKind == "webhook" && incoming.Format == "") ||
			(targetKind == "telegram" && incoming.ClearSecret) {
			httpapi.Error(w, http.StatusBadRequest, "invalid_request", "invalid channel configuration")
			return
		}
		merged, err := h.Repository.ChannelConfig(r.Context(), id)
		if err != nil {
			httpapi.Error(w, http.StatusInternalServerError, "database_error", "cannot read channel configuration")
			return
		}
		if incoming.URL != "" {
			merged.URL = incoming.URL
		}
		if incoming.BotToken != "" {
			merged.BotToken = incoming.BotToken
		}
		if incoming.ChatID != "" {
			merged.ChatID = incoming.ChatID
		}
		merged.Format = incoming.Format
		if incoming.Secret != "" {
			merged.Secret = incoming.Secret
		}
		if incoming.ClearSecret {
			merged.Secret = ""
		}
		if targetKind == "webhook" && merged.Format != "dingtalk" && merged.Format != "feishu" {
			// Reject a newly supplied secret, but discard a previously saved signing key.
			if incoming.Secret != "" {
				httpapi.Error(w, http.StatusBadRequest, "invalid_request", "invalid channel configuration")
				return
			}
			merged.Secret = ""
		}
		if !validateChannelConfig(targetKind, &merged) {
			httpapi.Error(w, http.StatusBadRequest, "invalid_request", "invalid channel configuration")
			return
		}
		config = &merged
	}

	ch, err := h.Repository.SaveChannel(r.Context(), Channel{
		ID:      id,
		Kind:    targetKind,
		Format:  targetFormat,
		Name:    req.Name,
		Enabled: req.Enabled,
	}, config)
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	h.recordAudit(r, "修改通知渠道", ch.Name)
	httpapi.JSON(w, http.StatusOK, ch)
}

func (h *Handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Repository.DeleteChannel(r.Context(), id); err != nil {
		httpapi.Error(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	h.recordAudit(r, "修改通知渠道", id)
	httpapi.JSON(w, http.StatusOK, map[string]any{})
}

func (h *Handler) testChannel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	code := ""
	if h.Worker != nil {
		code = h.Worker.Test(r.Context(), id)
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{
		"code": code,
	})
}
