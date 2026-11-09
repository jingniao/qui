package node

import (
	"context"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) RegisterGroups(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/node-groups", h.Auth.Require(http.HandlerFunc(h.listGroups), true))
	mux.Handle("POST /api/v1/node-groups", h.Auth.Require(http.HandlerFunc(h.saveGroup), true))
	mux.Handle("PUT /api/v1/node-groups/{id}", h.Auth.Require(http.HandlerFunc(h.saveGroup), true))
	mux.Handle("DELETE /api/v1/node-groups/{id}", h.Auth.Require(http.HandlerFunc(h.deleteGroup), true))
}
func (h *Handler) listGroups(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.Groups(r.Context(), page, size)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}
func (h *Handler) saveGroup(w http.ResponseWriter, r *http.Request) {
	var input GroupInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	group, err := h.Repository.SaveGroup(r.Context(), r.PathValue("id"), input)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.fail(w, err)
		} else {
			httpapi.Error(w, 400, "invalid_request", err.Error())
		}
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "保存节点组", group.Name, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	// The saved members decide which inbounds the group's users may reach, so
	// their credentials have to follow. Only a committed save with its audit
	// entry reaches this point; a rejected save changes nothing and queues
	// nothing.
	h.groupAccessChanged(r.Context(), group.ID, group.GrantedUserIDs, "node group updated")
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	httpapi.JSON(w, status, group)
}
func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// The grants cascade away with the group, and they are what the automatic
	// revision needs to know whose credentials to take off the nodes.
	group, err := h.Repository.Group(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Repository.DeleteGroup(r.Context(), id); err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除节点组", id, "成功", r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	h.groupAccessChanged(r.Context(), id, group.GrantedUserIDs, "node group deleted")
	w.WriteHeader(204)
}

// groupAccessChanged hands the group's granted users to the platform's
// scheduler. It queues only: the affected nodes are resolved there, exactly as
// for a grant change, and a change the nodes already reflect deploys nothing.
func (h *Handler) groupAccessChanged(ctx context.Context, groupID string, userIDs []string, reason string) {
	if h.GroupAccessChanged == nil || len(userIDs) == 0 {
		return
	}
	h.GroupAccessChanged(ctx, groupID, userIDs, reason)
}
