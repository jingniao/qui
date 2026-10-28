package proxy

import (
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
)

type Handler struct {
	Service *Service
	Auth    *auth.Handler
	Audit   auth.Audit
	// Users reads the user credentials a node snapshot needs. Publishing a
	// canvas scheme snapshots the nodes it touches, exactly like a publish from
	// the node page.
	Users AccessReader
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/inbounds/{id}/reachability", h.Auth.Require(http.HandlerFunc(h.reachability), true))
	for pattern, handler := range map[string]http.HandlerFunc{"GET /api/v1/inbounds": h.listInbounds, "POST /api/v1/inbounds": h.saveInbound, "PUT /api/v1/inbounds/{id}": h.saveInbound, "DELETE /api/v1/inbounds/{id}": h.deleteInbound, "GET /api/v1/outbounds": h.listOutbounds, "POST /api/v1/outbounds": h.saveOutbound, "PUT /api/v1/outbounds/{id}": h.saveOutbound, "DELETE /api/v1/outbounds/{id}": h.deleteOutbound} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
	// The protocol capability basis is read-only and non-sensitive, so both Owner
	// and Viewer may read it. It is the same table the server validates against.
	mux.Handle("GET /api/v1/protocol-support", h.Auth.Require(http.HandlerFunc(h.protocolSupport), false))
	h.registerCanvas(mux)
}

func (h *Handler) reachability(w http.ResponseWriter, r *http.Request) {
	i, err := h.Service.Repository.Inbound(r.Context(), r.PathValue("id"), false)
	if err != nil {
		h.fail(w, err)
		return
	}
	n, err := h.Service.Nodes.Get(r.Context(), i.NodeID)
	if err != nil {
		h.fail(w, err)
		return
	}
	err = node.PanelReachability(r.Context(), n.PublicIP, i.Port, i.Settings.ServerName, i.Settings.Security == "tls", i.Settings.TrustedCAPEM, i.Settings.CertificatePEM)
	code := "reachable"
	if err != nil {
		code = "unreachable"
	}
	httpapi.JSON(w, 200, map[string]any{"reachable": err == nil, "code": code, "origin": "panel"})
}
func (h *Handler) protocolSupport(w http.ResponseWriter, r *http.Request) {
	httpapi.JSON(w, 200, map[string]any{"protocols": desiredstate.ProtocolSupportTable()})
}
func (h *Handler) listInbounds(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Service.Repository.Inbounds(r.Context(), r.URL.Query().Get("nodeId"), page, size, false)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) listOutbounds(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Service.Repository.Outbounds(r.Context(), r.URL.Query().Get("nodeId"), page, size, false)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) saveInbound(w http.ResponseWriter, r *http.Request) {
	var input InboundInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	inbound, err := h.Service.SaveInbound(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "保存入站配置", inbound.Name, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, inbound)
}
func (h *Handler) saveOutbound(w http.ResponseWriter, r *http.Request) {
	var input OutboundInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	outbound, err := h.Service.SaveOutbound(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "保存出站配置", outbound.Name, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, outbound)
}
func (h *Handler) deleteInbound(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Repository.DeleteInbound(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除入站配置", r.PathValue("id"), "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) deleteOutbound(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.DeleteOutbound(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除出站配置", r.PathValue("id"), "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		httpapi.Error(w, 404, "not_found", err.Error())
		return
	}
	httpapi.Error(w, 400, "invalid_request", err.Error())
}
func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.Auth.Logger.Error("proxy operation failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "Proxy operation failed")
}
