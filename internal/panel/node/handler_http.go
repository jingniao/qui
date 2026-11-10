package node

import (
	"context"
	"errors"
	"net"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"time"
)

// InstallInfo is what the register dialog needs for the one-command install: the
// release version to download, the gRPC port the Agent connects to and the
// SHA-256 of the certificate trust anchor the panel serves at /install/ca.pem.
// Every field is empty when the panel cannot supply it, which is not an error.
type InstallInfo struct {
	Version  string `json:"version"`
	GRPCPort int    `json:"grpcPort"`
	CASHA256 string `json:"caSha256"`
}

// auditLog is the operation log: Record for its own writes, RecordTx when the
// entry must commit with a change owned by this package.
type auditLog interface {
	auth.Audit
	DeletionAudit
}

type Handler struct {
	Authority  *pki.Authority
	Repository *Repository
	Audit      auditLog
	Auth       *auth.Handler
	Notify     func(string)
	Disconnect func(string)
	// Tasks closes a deleted node's own coordination rows; the deployment module
	// owns those tables. A nil field leaves them as they are.
	Tasks Tasks
	// Install supplies the one-command install metadata. A nil field leaves the
	// fields empty; enrollment never depends on it.
	Install func() InstallInfo
	// GroupAccessChanged queues the automatic revision for the users granted a
	// node group whose membership, enablement or purpose changed: the group is
	// what decides which inbounds their credentials reach, so a saved or
	// deleted group changes their access without any user row being touched.
	// The node package does not import the user or platform packages; the
	// platform wires this to the same scheduler a grant change uses. Optional.
	GroupAccessChanged func(ctx context.Context, groupID string, userIDs []string, reason string)
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/nodes/machine-traffic", h.Auth.Require(http.HandlerFunc(h.machineTrafficBatch), false))
	mux.Handle("GET /api/v1/nodes/{id}/machine-traffic", h.Auth.Require(http.HandlerFunc(h.machineTraffic), false))
	mux.Handle("GET /api/v1/nodes/{id}/machine-traffic/settings", h.Auth.Require(http.HandlerFunc(h.machineTrafficSettings), false))
	mux.Handle("PUT /api/v1/nodes/{id}/machine-traffic/settings", h.Auth.Require(http.HandlerFunc(h.putMachineTrafficSettings), true))
	mux.Handle("GET /api/v1/nodes/{id}/metrics", h.Auth.Require(http.HandlerFunc(h.metrics), false))
	mux.Handle("GET /api/v1/nodes/{id}/tuning", h.Auth.Require(http.HandlerFunc(h.tuning), false))
	mux.Handle("PUT /api/v1/nodes/{id}/tuning", h.Auth.Require(http.HandlerFunc(h.tuning), true))
	mux.Handle("GET /api/v1/nodes/{id}/latency-targets", h.Auth.Require(http.HandlerFunc(h.latencyTargets), false))
	mux.Handle("PUT /api/v1/nodes/{id}/latency-targets", h.Auth.Require(http.HandlerFunc(h.latencyTargets), true))
	mux.Handle("GET /api/v1/nodes/{id}/latency", h.Auth.Require(http.HandlerFunc(h.latency), false))
	mux.Handle("PUT /api/v1/probe-targets/{target}", h.Auth.Require(http.HandlerFunc(h.probeTargets), true))
	mux.Handle("GET /api/v1/probe-targets", h.Auth.Require(http.HandlerFunc(h.probeTargets), false))
	mux.Handle("POST /api/v1/probe-targets", h.Auth.Require(http.HandlerFunc(h.probeTargets), true))
	mux.Handle("DELETE /api/v1/probe-targets/{target}", h.Auth.Require(http.HandlerFunc(h.deleteProbeTarget), true))
	mux.Handle("GET /api/v1/nodes/{id}/tcp-probes", h.Auth.Require(http.HandlerFunc(h.tcpProbes), false))
	mux.Handle("POST /api/v1/nodes/{id}/tcp-probes", h.Auth.Require(http.HandlerFunc(h.tcpProbes), true))
	mux.Handle("GET /api/v1/nodes/{id}/meta", h.Auth.Require(http.HandlerFunc(h.getMeta), false))
	mux.Handle("PUT /api/v1/nodes/{id}/meta", h.Auth.Require(http.HandlerFunc(h.putMeta), true))
	mux.Handle("GET /api/v1/nodes/{id}/enrollment-status", h.Auth.Require(http.HandlerFunc(h.enrollmentStatus), true))
	mux.Handle("GET /api/v1/nodes/{id}/certificate-status", h.Auth.Require(http.HandlerFunc(h.certificateStatus), true))
	mux.Handle("POST /api/v1/nodes/{id}/health-checks", h.Auth.Require(http.HandlerFunc(h.requestHealthCheck), true))
	mux.Handle("GET /api/v1/nodes/{id}/health-checks", h.Auth.Require(http.HandlerFunc(h.listHealthChecks), true))
	mux.Handle("GET /api/v1/nodes/{id}/deletion-preview", h.Auth.Require(http.HandlerFunc(h.deletionPreview), true))
	mux.Handle("GET /api/v1/nodes", h.Auth.Require(http.HandlerFunc(h.list), false))
	mux.Handle("GET /api/v1/nodes/{id}", h.Auth.Require(http.HandlerFunc(h.get), false))
	mux.Handle("POST /api/v1/nodes/enrollment-token", h.Auth.Require(http.HandlerFunc(h.enroll), true))
	mux.Handle("POST /api/v1/nodes/{id}/enrollment-token", h.Auth.Require(http.HandlerFunc(h.reissueEnrollment), true))
	mux.Handle("PUT /api/v1/nodes/{id}", h.Auth.Require(http.HandlerFunc(h.update), true))
	mux.Handle("POST /api/v1/nodes/{id}/revoke-certificate", h.Auth.Require(http.HandlerFunc(h.revoke), true))
	mux.Handle("POST /api/v1/nodes/{id}/recovery-token", h.Auth.Require(http.HandlerFunc(h.issueRecovery), true))
	mux.Handle("GET /api/v1/nodes/{id}/recovery-status", h.Auth.Require(http.HandlerFunc(h.recoveryStatus), true))
	mux.Handle("DELETE /api/v1/nodes/{id}", h.Auth.Require(http.HandlerFunc(h.delete), true))
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.List(r.Context(), r.URL.Query().Get("keyword"), page, size, r.URL.Query().Get("deleted") == "1")
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) deletionPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := h.Repository.Preview(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, preview)
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, n)
}
func (h *Handler) enroll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name             string `json:"name"`
		ExpiresInSeconds int    `json:"expiresInSeconds"`
		ConnectHost      string `json:"connectHost"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Name) > 120 {
		httpapi.Error(w, 400, "invalid_request", "Node name must contain 1 to 120 bytes")
		return
	}
	connectHost := strings.TrimSpace(body.ConnectHost)
	if !validConnectHost(connectHost) {
		httpapi.Error(w, 400, "invalid_request", "Invalid connect host: use a domain name or an IP literal without a scheme or port")
		return
	}
	if body.ExpiresInSeconds == 0 {
		body.ExpiresInSeconds = 3600
	}
	if body.ExpiresInSeconds < 60 || body.ExpiresInSeconds > 86400 {
		httpapi.Error(w, 400, "invalid_request", "Enrollment expiry must be between 60 and 86400 seconds")
		return
	}
	n, token, expires, err := h.Repository.CreateEnrollment(r.Context(), strings.TrimSpace(body.Name), connectHost, time.Duration(body.ExpiresInSeconds)*time.Second)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "创建节点注册码", n.Name, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	install := InstallInfo{}
	if h.Install != nil {
		install = h.Install()
	}
	httpapi.JSON(w, 201, map[string]any{"node": n, "enrollmentToken": token, "expiresAt": expires, "install": install})
	if h.Notify != nil {
		h.Notify(n.ID)
	}
}

func (h *Handler) reissueEnrollment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpiresInSeconds int `json:"expiresInSeconds"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.ExpiresInSeconds == 0 {
		body.ExpiresInSeconds = 3600
	}
	if body.ExpiresInSeconds < 60 || body.ExpiresInSeconds > 86400 {
		httpapi.Error(w, 400, "invalid_request", "Enrollment expiry must be between 60 and 86400 seconds")
		return
	}
	n, token, expires, err := h.Repository.ReissueEnrollment(r.Context(), r.PathValue("id"), time.Duration(body.ExpiresInSeconds)*time.Second)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "重新签发节点注册码", n.Name, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	install := InstallInfo{}
	if h.Install != nil {
		install = h.Install()
	}
	httpapi.JSON(w, 201, map[string]any{"node": n, "enrollmentToken": token, "expiresAt": expires, "install": install})
	if h.Notify != nil {
		h.Notify(n.ID)
	}
}
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name              string  `json:"name"`
		Enabled           *bool   `json:"enabled"`
		AllowSubscription *bool   `json:"allowSubscription"`
		Maintenance       *bool   `json:"maintenance"`
		PublicIP          *string `json:"publicIp"`
		PublicIPSelected  *string `json:"publicIpSelected"`
		ConnectHost       *string `json:"connectHost"`
		OutboundSourceIP  *string `json:"outboundSourceIp"`
		OutboundInterface *string `json:"outboundInterface"`
		HostSourceIPv4    *string `json:"hostSourceIpv4"`
		HostSourceIPv6    *string `json:"hostSourceIpv6"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" || len(body.Name) > 120 || body.Enabled == nil {
		httpapi.Error(w, 400, "invalid_request", "Node name and enabled are required")
		return
	}
	if body.PublicIP != nil && *body.PublicIP != "" && net.ParseIP(*body.PublicIP) == nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid public IP")
		return
	}
	if body.PublicIPSelected != nil && *body.PublicIPSelected != "" && net.ParseIP(*body.PublicIPSelected) == nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid selected public IP")
		return
	}
	if body.ConnectHost != nil && !validConnectHost(strings.TrimSpace(*body.ConnectHost)) {
		httpapi.Error(w, 400, "invalid_request", "Invalid connect host: use a domain name or an IP literal without a scheme or port")
		return
	}
	settings := SettingsInput{Name: body.Name, Enabled: *body.Enabled, AllowSubscription: body.AllowSubscription, Maintenance: body.Maintenance, PublicIP: body.PublicIP, PublicIPSelected: body.PublicIPSelected, ConnectHost: body.ConnectHost, OutboundSourceIP: body.OutboundSourceIP, OutboundInterface: body.OutboundInterface, HostSourceIPv4: body.HostSourceIPv4, HostSourceIPv6: body.HostSourceIPv6}
	if _, err := networkSettings(desiredstate.Network{}, settings); err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid outbound network settings")
		return
	}
	if err := h.Repository.UpdateSettings(r.Context(), r.PathValue("id"), settings); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "修改节点", body.Name, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
	if h.Notify != nil {
		h.Notify(r.PathValue("id"))
	}
	if !*body.Enabled && h.Disconnect != nil {
		h.Disconnect(r.PathValue("id"))
	}
}
func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Repository.Revoke(r.Context(), n.ID); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "吊销节点证书", n.Name, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
	if h.Disconnect != nil {
		h.Disconnect(n.ID)
	}
}

func (h *Handler) issueRecovery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpiresInSeconds int  `json:"expiresInSeconds"`
		RevokeNow        bool `json:"revokeNow"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if body.ExpiresInSeconds == 0 {
		body.ExpiresInSeconds = 900
	}
	if body.ExpiresInSeconds < 300 || body.ExpiresInSeconds > 3600 {
		httpapi.Error(w, 400, "invalid_request", "Recovery expiry must be between 300 and 3600 seconds")
		return
	}
	actor := auth.Actor(r.Context()).Username
	n, token, expires, err := h.Repository.IssueRecovery(r.Context(), r.PathValue("id"), actor, time.Duration(body.ExpiresInSeconds)*time.Second, body.RevokeNow)
	if err != nil {
		h.fail(w, err)
		return
	}
	action := "签发节点恢复码"
	if body.RevokeNow {
		action = "签发节点恢复码并吊销证书"
	}
	if err := h.Audit.Record(r.Context(), actor, action, n.Name, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	install := InstallInfo{}
	if h.Install != nil {
		install = h.Install()
	}
	httpapi.JSON(w, 201, map[string]any{"node": n, "enrollmentToken": token, "expiresAt": expires, "install": install, "purpose": "recover"})
	if body.RevokeNow && h.Disconnect != nil {
		h.Disconnect(n.ID)
	}
	if h.Notify != nil {
		h.Notify(n.ID)
	}
}

func (h *Handler) recoveryStatus(w http.ResponseWriter, r *http.Request) {
	x, err := h.Repository.RecoveryStatus(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, x)
}
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := h.Repository.Get(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	// Deleting twice is a no-op.
	if n.DeletedAt != "" {
		w.WriteHeader(204)
		return
	}
	// A pending node that never enrolled keeps its own path: the unused record is
	// removed and its registration code stops working.
	if n.Status == "pending" {
		if err := h.Repository.CancelEnrollment(r.Context(), id); err == nil {
			if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "取消节点登记", n.Name+" ("+n.ID+")", "成功", r.RemoteAddr); err != nil {
				h.fail(w, err)
				return
			}
			w.WriteHeader(204)
			if h.Notify != nil {
				h.Notify(id)
			}
			return
		} else if !errors.Is(err, ErrNodeEnrolled) {
			h.fail(w, err)
			return
		}
		// An Agent enrolled while this request was in flight: delete logically.
	}
	if err := h.Repository.Delete(r.Context(), id, h.Tasks, h.Audit, auth.Actor(r.Context()).Username, r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
	if h.Notify != nil {
		h.Notify(id)
	}
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		httpapi.Error(w, 404, "not_found", err.Error())
		return
	}
	if errors.Is(err, ErrNodeNotPending) {
		httpapi.Error(w, 409, "node_not_pending", err.Error())
		return
	}
	if errors.Is(err, ErrNodeNotEnrolled) {
		httpapi.Error(w, 409, "node_not_enrolled", err.Error())
		return
	}
	if errors.Is(err, ErrSettings) {
		httpapi.Error(w, 400, "invalid_request", "Invalid node settings")
		return
	}
	if errors.Is(err, ErrNodeDeleted) {
		httpapi.Error(w, 409, "node_deleted", "节点已删除，不能修改。")
		return
	}
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		code := blocked.Code
		if code == "" {
			code = "node_delete_blocked"
		}
		message := "节点仍被配置引用或有进行中的发布，请先处理下列项目。"
		if code == "node_recovery_blocked" {
			message = "节点存在未完成任务，请先处理下列项目。"
		}
		httpapi.ErrorDetails(w, 409, code, message, map[string]any{"blockers": blocked.Items})
		return
	}
	if errors.Is(err, ErrNodeEnrolled) {
		httpapi.Error(w, 409, "node_delete_not_allowed", "节点已登记，不能按取消登记处理。")
		return
	}
	if errors.Is(err, ErrNodeInUse) {
		httpapi.Error(w, 409, "node_in_use", "节点已被入站、出站、代理链、分流规则或发布记录引用，请先移除这些配置。")
		return
	}
	h.Auth.Logger.Error("node operation failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "Node operation failed")
}
