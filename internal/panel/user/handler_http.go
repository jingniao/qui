package user

import (
	"context"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/httpapi"
	"strings"
)

type Handler struct {
	Repository *Repository
	Auth       *auth.Handler
	Audit      auth.Audit
	Nodes      *node.Repository
	// QuotaChanged re-evaluates a user's active state after an Owner edit to
	// enabled, expires_at, traffic_limit or a usage reset. Optional.
	QuotaChanged func(context.Context, string)
	// ExpiryRenewed re-evaluates a user after a Save that changed expires_at.
	// Renewing a user who stays active is not an activity transition, so
	// QuotaChanged alone would leave the nodes on the old deadline. Optional.
	ExpiryRenewed func(context.Context, string)
	// AccessRemoved revokes access a user or one of its devices lost to a
	// delete. The deleted rows, credentials, grants and subscription token are
	// gone, so no later Owner edit can reach the nodes that still run those
	// credentials; the panel has to schedule that revocation itself. It is
	// called only after the delete succeeded, with the reason deleted or
	// device deleted. Optional.
	AccessRemoved func(ctx context.Context, userID, username, reason string)
	// AccessChanged queues the automatic revision for a user whose credentials
	// or grants may have changed: a save (protocols, node groups) or a
	// credentials reset. The revision is only created for nodes whose user
	// access surface differs. Optional.
	AccessChanged func(ctx context.Context, userID, username, reason string)
	// Observations fills in the live connection count and over-limit flag on
	// returned users. Optional and nil-safe.
	Observations *ConnectionObserver
	Devices      *DeviceObserver
}

func (h *Handler) observe(ctx context.Context, account *User) {
	if h.Devices != nil {
		if view, err := h.Devices.View(ctx, *account); err == nil {
			account.OnlineDevices = view.OnlineDevices
			account.DeviceLimitEnforced = view.Enforced
			account.DeviceLimitUnsupportedNodes = view.UnsupportedNodes
		}
	}
	if h.Observations == nil {
		return
	}
	count, err := h.Observations.ForUser(ctx, *account)
	if err != nil {
		return
	}
	account.OnlineConnections = count
}

func (h *Handler) quotaChanged(ctx context.Context, userID string) {
	if h.QuotaChanged != nil {
		h.QuotaChanged(ctx, userID)
	}
}

func (h *Handler) expiryRenewed(ctx context.Context, userID string) {
	if h.ExpiryRenewed != nil {
		h.ExpiryRenewed(ctx, userID)
	}
}

func (h *Handler) accessRemoved(ctx context.Context, userID, username, reason string) {
	if h.AccessRemoved != nil {
		h.AccessRemoved(ctx, userID, username, reason)
	}
}

func (h *Handler) accessChanged(ctx context.Context, userID, username, reason string) {
	if h.AccessChanged != nil {
		h.AccessChanged(ctx, userID, username, reason)
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/users/{id}/devices-online", h.Auth.Require(http.HandlerFunc(h.devicesOnline), true))
	mux.Handle("POST /api/v1/users/{id}/devices-online/reset", h.Auth.Require(http.HandlerFunc(h.devicesOnline), true))
	for pattern, handler := range map[string]http.HandlerFunc{"GET /api/v1/users": h.list, "POST /api/v1/users": h.save, "GET /api/v1/users/{id}": h.get, "PUT /api/v1/users/{id}": h.save, "DELETE /api/v1/users/{id}": h.delete, "POST /api/v1/users/{id}/credentials/reset": h.resetCredentials, "POST /api/v1/users/{id}/node-groups/{groupId}": h.grant, "DELETE /api/v1/users/{id}/node-groups/{groupId}": h.grant, "POST /api/v1/users/{id}/reset-traffic": h.resetTraffic} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
	// v1.1 manages access per user; the device API is gone. Its routes answer
	// 410 so a stale caller gets a clear error instead of a 404 or a write.
	for _, pattern := range []string{"GET /api/v1/users/{id}/devices", "POST /api/v1/users/{id}/devices", "GET /api/v1/devices", "GET /api/v1/devices/{id}", "PUT /api/v1/devices/{id}", "DELETE /api/v1/devices/{id}", "POST /api/v1/devices/{id}/credentials/{protocol}/reset", "DELETE /api/v1/devices/{id}/credentials/{protocol}"} {
		mux.Handle(pattern, h.Auth.Require(http.HandlerFunc(devicesRemoved), true))
	}
}

func devicesRemoved(w http.ResponseWriter, _ *http.Request) {
	httpapi.Error(w, 410, "devices_removed", "设备管理已在 v1.1 移除：权限、凭据与订阅按用户管理")
}

// resetCredentials re-issues every proxy credential of the user. The old
// settings stop working once the automatic revision with the new ones has been
// applied on the nodes.
func (h *Handler) resetCredentials(w http.ResponseWriter, r *http.Request) {
	account, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Repository.ResetCredentials(r.Context(), account.ID); err != nil {
		h.fail(w, err)
		return
	}
	if h.Devices != nil {
		h.Devices.Changed()
	}
	h.accessChanged(r.Context(), account.ID, account.Username, "credentials reset")
	if err := h.audit(r, "重置用户凭据", account.Username); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) audit(r *http.Request, action, target string) error {
	return h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, action, target, "成功", r.RemoteAddr)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, errors.New("invalid pagination"))
		return
	}
	items, total, err := h.Repository.List(r.Context(), r.URL.Query().Get("keyword"), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	for index := range items {
		h.observe(r.Context(), &items[index])
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	user, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.observe(r.Context(), &user)
	httpapi.JSON(w, 200, user)
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	var input Input
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	id := r.PathValue("id")
	previousExpiry, loaded := "", false
	if id != "" {
		previous, err := h.Repository.Get(r.Context(), id)
		if err != nil {
			h.fail(w, err)
			return
		}
		previousExpiry, loaded = previous.ExpiresAt, true
	}
	user, err := h.Repository.Save(r.Context(), id, input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if loaded && previousExpiry != user.ExpiresAt {
		h.expiryRenewed(r.Context(), user.ID)
	} else {
		h.quotaChanged(r.Context(), user.ID)
	}
	if h.Devices != nil {
		h.Devices.Changed()
	}
	h.accessChanged(r.Context(), user.ID, user.Username, "updated")
	if err := h.audit(r, "保存代理用户", user.Username); err != nil {
		h.internal(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	h.observe(r.Context(), &user)
	httpapi.JSON(w, status, user)
}
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// The username only exists until the delete succeeds, and the revocation
	// below reports it to the Owner.
	account, err := h.Repository.Get(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Repository.Delete(r.Context(), id); err != nil {
		h.fail(w, err)
		return
	}
	if h.Devices != nil {
		h.Devices.Admission.Reset(id)
		h.Devices.Changed()
	}
	h.accessRemoved(r.Context(), id, account.Username, "deleted")
	if err := h.audit(r, "删除代理用户", id); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) grant(w http.ResponseWriter, r *http.Request) {
	if err := h.Repository.Grant(r.Context(), r.PathValue("id"), r.PathValue("groupId"), r.Method == http.MethodDelete); err != nil {
		h.fail(w, err)
		return
	}
	if account, err := h.Repository.Get(r.Context(), r.PathValue("id")); err == nil {
		h.accessChanged(r.Context(), account.ID, account.Username, "updated")
	}
	if err := h.audit(r, "修改用户节点组授权", r.PathValue("id")); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) resetTraffic(w http.ResponseWriter, r *http.Request) {
	if err := h.Repository.ResetTraffic(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	h.quotaChanged(r.Context(), r.PathValue("id"))
	if err := h.audit(r, "重置用户流量", r.PathValue("id")); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, node.ErrNotFound):
		httpapi.Error(w, 404, "not_found", err.Error())
	case errors.Is(err, ErrConflict), errors.Is(err, ErrDeviceLimit):
		httpapi.Error(w, 409, "conflict", err.Error())
	case errors.Is(err, ErrInactive), errors.Is(err, ErrCredential):
		httpapi.Error(w, 403, "user_inactive", err.Error())
	default:
		message := err.Error()
		for _, prefix := range []string{"invalid ", "username ", "device name ", "maximum devices ", "protocol ", "node group ", "too many ", "display name ", "expiry ", "choose ", "request ", "Content-Type "} {
			if strings.HasPrefix(message, prefix) {
				httpapi.Error(w, 400, "invalid_request", message)
				return
			}
		}
		h.internal(w, err)
	}
}
func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.Auth.Logger.Error("user operation failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "User operation failed")
}
