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
	// Observations fills in the live connection count and over-limit flag on
	// returned users. Optional and nil-safe.
	Observations *ConnectionObserver
}

func (h *Handler) observe(ctx context.Context, account *User) {
	if h.Observations == nil {
		return
	}
	count, err := h.Observations.ForUser(ctx, *account)
	if err != nil {
		return
	}
	account.OnlineConnections = count
	account.OverLimit = count != nil && account.MaxConnections > 0 && *count > account.MaxConnections
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

func (h *Handler) Register(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{"GET /api/v1/users": h.list, "POST /api/v1/users": h.save, "GET /api/v1/users/{id}": h.get, "PUT /api/v1/users/{id}": h.save, "DELETE /api/v1/users/{id}": h.delete, "GET /api/v1/users/{id}/devices": h.devices, "POST /api/v1/users/{id}/devices": h.createDevice, "GET /api/v1/devices": h.devices, "GET /api/v1/devices/{id}": h.getDevice, "PUT /api/v1/devices/{id}": h.updateDevice, "DELETE /api/v1/devices/{id}": h.deleteDevice, "POST /api/v1/devices/{id}/credentials/{protocol}/reset": h.rotate, "DELETE /api/v1/devices/{id}/credentials/{protocol}": h.rotate, "POST /api/v1/users/{id}/node-groups/{groupId}": h.grant, "DELETE /api/v1/users/{id}/node-groups/{groupId}": h.grant, "POST /api/v1/users/{id}/reset-traffic": h.resetTraffic} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
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
	if err := h.Repository.Delete(r.Context(), id); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "删除代理用户", id); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) devices(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, errors.New("invalid pagination"))
		return
	}
	userID := r.PathValue("id")
	if userID != "" {
		if _, err := h.Repository.Get(r.Context(), userID); err != nil {
			h.fail(w, err)
			return
		}
	}
	items, total, err := h.Repository.Devices(r.Context(), userID, page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) createDevice(w http.ResponseWriter, r *http.Request) {
	var input DeviceInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	device, err := h.Repository.CreateDevice(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "创建设备与凭据", device.ID); err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 201, device)
}
func (h *Handler) getDevice(w http.ResponseWriter, r *http.Request) {
	device, err := h.Repository.Device(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, device)
}
func (h *Handler) updateDevice(w http.ResponseWriter, r *http.Request) {
	var input DeviceInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Repository.UpdateDevice(r.Context(), r.PathValue("id"), input); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "修改设备", r.PathValue("id")); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) deleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := h.Repository.DeleteDevice(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "删除设备", r.PathValue("id")); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) rotate(w http.ResponseWriter, r *http.Request) {
	revoke := r.Method == http.MethodDelete
	credential, err := h.Repository.RotateCredential(r.Context(), r.PathValue("id"), r.PathValue("protocol"), revoke)
	if err != nil {
		h.fail(w, err)
		return
	}
	action := "重置设备凭据"
	if revoke {
		action = "撤销协议凭据"
	}
	if err := h.audit(r, action, r.PathValue("id")+":"+r.PathValue("protocol")); err != nil {
		h.internal(w, err)
		return
	}
	if revoke {
		w.WriteHeader(204)
	} else {
		httpapi.JSON(w, 201, credential)
	}
}
func (h *Handler) grant(w http.ResponseWriter, r *http.Request) {
	if err := h.Repository.Grant(r.Context(), r.PathValue("id"), r.PathValue("groupId"), r.Method == http.MethodDelete); err != nil {
		h.fail(w, err)
		return
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
