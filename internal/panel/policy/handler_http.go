package policy

import (
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	shared "proxycontrol/pkg/contracts/policy"
)

type Handler struct {
	Service *Service
	Auth    *auth.Handler
	Audit   auth.Audit
}

func (h *Handler) Register(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{"GET /api/v1/policies": h.list, "GET /api/v1/policies/{id}": h.get, "POST /api/v1/policies": h.save, "PUT /api/v1/policies/{id}": h.save, "DELETE /api/v1/policies/{id}": h.delete, "POST /api/v1/policies/simulate": h.simulate, "GET /api/v1/service-sets": h.services, "GET /api/v1/service-sets/{id}": h.getService, "POST /api/v1/service-sets": h.saveService, "PUT /api/v1/service-sets/{id}": h.saveService, "DELETE /api/v1/service-sets/{id}": h.deleteService} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
}
func (h *Handler) audit(r *http.Request, action, target string) error {
	return h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, action, target, "成功", r.RemoteAddr)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Service.Repository.List(r.Context(), r.URL.Query().Get("keyword"), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	record, err := h.Service.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, record)
}
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	var input Input
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	record, err := h.Service.Save(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "保存分流与网站控制策略", record.Name); err != nil {
		h.internal(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, record)
}
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Repository.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "删除分流策略", r.PathValue("id")); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) simulate(w http.ResponseWriter, r *http.Request) {
	var request shared.Request
	if err := httpapi.Decode(w, r, &request); err != nil {
		h.fail(w, err)
		return
	}
	decision, err := h.Service.Simulate(r.Context(), request)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, decision)
}
func (h *Handler) services(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	items, total, err := h.Service.Repository.Services(r.Context(), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) getService(w http.ResponseWriter, r *http.Request) {
	service, err := h.Service.Repository.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, service)
}
func (h *Handler) saveService(w http.ResponseWriter, r *http.Request) {
	var input ServiceInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	service, err := h.Service.Repository.SaveService(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "更新服务规则草稿", service.Name); err != nil {
		h.internal(w, err)
		return
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, service)
}
func (h *Handler) deleteService(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Repository.DeleteService(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "删除服务规则", r.PathValue("id")); err != nil {
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
	httpapi.Error(w, 400, "invalid_policy", err.Error())
}
func (h *Handler) internal(w http.ResponseWriter, err error) {
	h.Auth.Logger.Error("policy operation failed", "error", err)
	httpapi.Error(w, 500, "internal_error", "Policy operation failed")
}
