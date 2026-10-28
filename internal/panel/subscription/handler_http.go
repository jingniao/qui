package subscription

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/httpapi"
)

type Handler struct {
	Service *Service
	Auth    *auth.Handler
	Audit   auth.Audit
	// Rules keeps the client rule template; ServiceRules serves panel service
	// rules to clients as rule providers.
	Rules        RulesStore
	ServiceRules ServiceRules
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /sub/{token}", h.content)
	mux.Handle("GET /api/v1/subscriptions", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/users/{id}/subscription-token", h.Auth.Require(http.HandlerFunc(h.rotate), true))
	mux.Handle("DELETE /api/v1/users/{id}/subscription-token", h.Auth.Require(http.HandlerFunc(h.revoke), true))
	mux.Handle("GET /api/v1/users/{id}/subscription-preview", h.Auth.Require(http.HandlerFunc(h.preview), true))
	mux.Handle("GET /api/v1/users/{id}/subscription-link", h.Auth.Require(http.HandlerFunc(h.link), true))
	mux.Handle("GET /api/v1/subscription-formats", h.Auth.Require(http.HandlerFunc(h.formats), false))
	h.registerRules(mux)
}

// content renders a subscription. The format comes from `?format=`, then from the
// client User-Agent when the Owner enabled detection, then from the Owner default;
// the exporter decides the content type and the download name.
func (h *Handler) content(w http.ResponseWriter, r *http.Request) {
	rendered, err := h.Service.Render(r.Context(), r.PathValue("token"), r.URL.Query().Get("format"), r.UserAgent(), providerURLFor(r, r.PathValue("token")))
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", rendered.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(rendered.FileName))
	w.Header().Set("subscription-userinfo", rendered.Usage.Header())
	w.Header().Set("profile-update-interval", strconv.Itoa(rendered.UpdateHours))
	w.Write(rendered.Body)
}

// fail answers `/sub` errors. The token holder is unauthenticated, so nothing but
// the shape of the failure is echoed back.
func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrFormat):
		httpapi.Error(w, 400, "invalid_format", "Unknown subscription format")
	case errors.Is(err, ErrSettings):
		h.Auth.Logger.Error("subscription settings unavailable", "error", err)
		httpapi.Error(w, 503, "subscription_unavailable", "Subscription settings unavailable")
	case errors.Is(err, ErrToken), errors.Is(err, user.ErrInactive), errors.Is(err, user.ErrNotFound), errors.Is(err, user.ErrCredential):
		httpapi.Error(w, 403, "subscription_unavailable", "Subscription unavailable")
	default:
		// Never echo internal error text (published-state identity, key or
		// material details) to the token holder.
		h.Auth.Logger.Error("subscription content failed", "error", err)
		httpapi.Error(w, 400, "invalid_subscription", "Subscription unavailable")
	}
}

// formats lists the registered formats, so the admin UI stops hard-coding them.
func (h *Handler) formats(w http.ResponseWriter, r *http.Request) {
	defaultFormat := ""
	if policy, err := h.Service.policy(r.Context()); err == nil {
		defaultFormat = policy.Default
	}
	httpapi.JSON(w, 200, map[string]any{"items": h.Service.formats().Info(), "default": defaultFormat})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Service.Repository.List(r.Context(), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

func (h *Handler) rotate(w http.ResponseWriter, r *http.Request) {
	// The token belongs to the user since v1.1. A caller that still names a
	// device is told so instead of having the field silently ignored.
	var body struct {
		DeviceID string `json:"deviceId"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.DeviceID != "" {
		httpapi.Error(w, 410, "devices_removed", "订阅已按用户管理，不再指定设备")
		return
	}
	token, plaintext, err := h.Service.Rotate(r.Context(), r.PathValue("id"), "")
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "重置订阅 Token", token.UserID, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 201, map[string]any{"subscription": token, "token": plaintext, "path": "/sub/" + plaintext})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, err := h.Service.Users.Get(r.Context(), userID); err != nil {
		httpapi.Error(w, 404, "not_found", "User not found")
		return
	}
	if err := h.Service.Repository.Revoke(r.Context(), userID); err != nil {
		h.internal(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "撤销订阅 Token", userID, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}

// previewItem is the admin preview of one published endpoint. The link is absent
// when the endpoint has no URI form, which keeps a pinned-CA Hysteria2 or TUIC
// endpoint listed without inventing a link no client could import.
type previewItem struct {
	NodeID    string  `json:"nodeId"`
	NodeName  string  `json:"nodeName"`
	InboundID string  `json:"inboundId"`
	Protocol  string  `json:"protocol"`
	Address   string  `json:"address"`
	Port      int     `json:"port"`
	Link      *string `json:"link,omitempty"`
}

// previewWarning is one diagnostic of the preview. Warnings are shown to the
// Owner only; `/sub` never carries them.
type previewWarning struct {
	Stage   string `json:"stage"`
	Ref     string `json:"ref"`
	Message string `json:"message"`
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	if requested := r.URL.Query().Get("format"); requested != "" {
		h.previewDocument(w, r, requested)
		return
	}
	proxies, raised, err := h.Service.Proxies(r.Context(), r.PathValue("id"), "")
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	warnings := make([]previewWarning, 0, len(raised))
	for _, warning := range raised {
		warnings = append(warnings, previewWarning{Stage: warning.Stage, Ref: warning.Ref, Message: warning.Message})
	}
	items := make([]previewItem, 0, len(proxies))
	for _, endpoint := range proxies {
		item := previewItem{NodeID: endpoint.Origin.Node.ID, NodeName: endpoint.Origin.Node.Name, InboundID: endpoint.Origin.Ref, Protocol: endpoint.Protocol, Address: endpoint.Server, Port: endpoint.Port}
		if link, err := uri.Link(endpoint); err == nil {
			item.Link = &link
		} else {
			// The uri and Base64 formats leave this endpoint out; say so here,
			// where the Owner can see it.
			warnings = append(warnings, previewWarning{Stage: "format", Ref: "uri", Message: endpoint.Name + " (" + endpoint.Origin.Ref + ") is not in the uri/base64 formats: " + err.Error()})
		}
		items = append(items, item)
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": len(items), "warnings": warnings})
}

// link returns the user's subscription link again (R5). The plaintext exists only
// because 0114 stores a vault-encrypted copy next to the hash; a token issued
// before v1.1 has no copy and reports link_not_recoverable instead of inventing a
// link. Every registered format gets its own URL, so the admin UI never builds one.
func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, err := h.Service.Users.Get(r.Context(), userID); err != nil {
		httpapi.Error(w, 404, "not_found", "User not found")
		return
	}
	plaintext, err := h.Service.Repository.Link(r.Context(), userID)
	switch {
	case errors.Is(err, ErrToken):
		httpapi.Error(w, 404, "not_found", "The user has no active subscription token")
		return
	case errors.Is(err, ErrNotRecoverable):
		httpapi.Error(w, 404, "link_not_recoverable", "订阅链接是 v1.1 之前签发的，重置后才能查看")
		return
	case err != nil:
		h.internal(w, err)
		return
	}
	path := "/sub/" + plaintext
	infos := h.Service.formats().Info()
	formats := make([]map[string]string, 0, len(infos))
	for _, info := range infos {
		formats = append(formats, map[string]string{"id": info.ID, "url": path + "?format=" + url.QueryEscape(info.ID)})
	}
	httpapi.JSON(w, 200, map[string]any{"path": path, "formats": formats})
}

func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.Auth.Logger.Error("subscription operation failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "Subscription operation failed")
}

// previewDocument renders the whole subscription document of one account in
// one format, with every warning, for the Owner's preview. It issues no token
// and touches no token's last-fetched time.
func (h *Handler) previewDocument(w http.ResponseWriter, r *http.Request, requested string) {
	rendered, err := h.Service.Preview(r.Context(), r.PathValue("id"), "", requested)
	switch {
	case errors.Is(err, ErrFormat):
		httpapi.Error(w, 400, "invalid_format", "Unknown subscription format")
		return
	case errors.Is(err, ErrSettings):
		h.internal(w, err)
		return
	case err != nil:
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	warnings := make([]previewWarning, 0, len(rendered.Warnings))
	for _, warning := range rendered.Warnings {
		warnings = append(warnings, previewWarning{Stage: warning.Stage, Ref: warning.Ref, Message: warning.Message})
	}
	httpapi.JSON(w, 200, map[string]any{"format": requested, "contentType": rendered.ContentType, "body": string(rendered.Body), "warnings": warnings})
}
