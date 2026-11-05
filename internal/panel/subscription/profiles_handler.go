package subscription

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/platform/httpapi"
)

type ProfileStore interface {
	ValidateTemplate(context.Context, rules.Template) error
	Profiles(context.Context, int, int) (rules.ProfileList, error)
	Profile(context.Context, string) (rules.ProfileResult, error)
	WriteProfile(context.Context, string, rules.ProfileInput, bool, string) (rules.ProfileResult, bool, error)
	DeleteProfile(context.Context, string, int64) (rules.ProfileResult, error)
}

func (h *Handler) registerProfiles(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{
		"POST /api/v1/client-policy-profiles/preview":       h.profilePreview,
		"GET /api/v1/client-policy-profiles":                h.profilesList,
		"POST /api/v1/client-policy-profiles":               h.profileWrite,
		"GET /api/v1/client-policy-profiles/{id}":           h.profileGet,
		"PUT /api/v1/client-policy-profiles/{id}":           h.profileWrite,
		"DELETE /api/v1/client-policy-profiles/{id}":        h.profileDelete,
		"POST /api/v1/client-policy-profiles/{id}/activate": h.profileActivate,
	} {
		mux.Handle(pattern, h.Auth.Require(handler, true))
	}
}

func (h *Handler) profileError(w http.ResponseWriter, state rules.ProfileResult, err error) {
	var field *rules.FieldError
	switch {
	case errors.As(err, &field):
		httpapi.ErrorDetails(w, 400, "invalid_rules", field.Message, map[string]any{"field": field.Field})
	case errors.Is(err, rules.ErrProfileConflict):
		httpapi.ErrorDetails(w, 409, "stale_client_policy", "配置已在其他地方修改，草稿已保留", map[string]any{"revision": state.Profile.Revision})
	case errors.Is(err, rules.ErrConflict):
		httpapi.ErrorDetails(w, 409, "stale_rules", "订阅模板已在其他地方修改，草稿已保留", map[string]any{"activeRevision": state.ActiveRevision})
	case errors.Is(err, rules.ErrProfileActive):
		httpapi.Error(w, 409, "client_policy_active", "请先将其他配置设为订阅模板")
	case errors.Is(err, rules.ErrProfileLimit):
		httpapi.Error(w, 400, "client_policy_limit", "最多保存 50 个配置")
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Error(w, 404, "not_found", "配置不存在")
	default:
		h.internal(w, err)
	}
}

func (h *Handler) profilesList(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "分页参数无效")
		return
	}
	list, err := h.Profiles.Profiles(r.Context(), page, size)
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, 200, list)
}

func (h *Handler) profileGet(w http.ResponseWriter, r *http.Request) {
	state, err := h.Profiles.Profile(r.Context(), r.PathValue("id"))
	if err != nil {
		h.profileError(w, state, err)
		return
	}
	httpapi.JSON(w, 200, state)
}

func (h *Handler) profileWrite(w http.ResponseWriter, r *http.Request) {
	var input rules.ProfileInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	// Activation has its own CAS endpoint; an edit cannot also switch profiles.
	if r.Method == http.MethodPut && (input.Activate || input.ImportKey != nil) {
		httpapi.Error(w, 400, "invalid_request", "编辑配置不能设置 activate 或 importKey")
		return
	}
	h.writeProfile(w, r, input, false)
}

func (h *Handler) profileActivate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpectedRevision       int64  `json:"expectedRevision"`
		ExpectedActiveRevision *int64 `json:"expectedActiveRevision"`
	}
	if err := httpapi.Decode(w, r, &body); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	h.writeProfile(w, r, rules.ProfileInput{ExpectedRevision: body.ExpectedRevision, ExpectedActiveRevision: body.ExpectedActiveRevision, Activate: true}, true)
}

func (h *Handler) writeProfile(w http.ResponseWriter, r *http.Request, input rules.ProfileInput, activateOnly bool) {
	actor := auth.Actor(r.Context()).Username
	state, changed, err := h.Profiles.WriteProfile(r.Context(), r.PathValue("id"), input, activateOnly, actor)
	if err != nil {
		h.profileError(w, state, err)
		return
	}
	if changed {
		action := "保存客户端策略配置"
		if input.Activate {
			action = "设为客户端订阅模板"
		}
		if err := h.Audit.Record(r.Context(), actor, action, state.Profile.ID, "成功", r.RemoteAddr); err != nil {
			h.internal(w, err)
			return
		}
	}
	status := http.StatusOK
	if r.PathValue("id") == "" && changed {
		status = http.StatusCreated
	}
	httpapi.JSON(w, status, state)
}

func (h *Handler) profileDelete(w http.ResponseWriter, r *http.Request) {
	expected, err := strconv.ParseInt(r.URL.Query().Get("expectedRevision"), 10, 64)
	if err != nil || expected < 1 {
		httpapi.Error(w, 400, "invalid_request", "请提供配置版本")
		return
	}
	state, err := h.Profiles.DeleteProfile(r.Context(), r.PathValue("id"), expected)
	if err != nil {
		h.profileError(w, state, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "删除客户端策略配置", r.PathValue("id"), "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
