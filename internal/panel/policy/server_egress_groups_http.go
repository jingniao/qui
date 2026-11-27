package policy

import (
	"net/http"

	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) serverEgressGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.Service.Repository.ServerEgressGroups(r.Context())
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, groups)
}

func (h *Handler) previewServerEgressGroup(w http.ResponseWriter, r *http.Request) {
	var input ServerEgressGroupInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	members, err := h.Service.Repository.ServerEgressMembers(r.Context(), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, members)
}

func (h *Handler) createServerEgressGroup(w http.ResponseWriter, r *http.Request) {
	var input ServerEgressGroupInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	group, err := h.Service.Repository.SaveServerEgressGroup(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "保存出站服务器组", group.Name); err != nil {
		h.internal(w, err)
		return
	}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	httpapi.JSON(w, status, group)
}

func (h *Handler) deleteServerEgressGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.Service.Repository.deleteServerEgressGroup(r.Context(), id); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.audit(r, "删除出站服务器组", id); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
