package subscription

import (
	"net/http"

	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/platform/httpapi"
)

type profilePreviewInput struct {
	ProfileID       string          `json:"profileId"`
	ProfileRevision *int64          `json:"profileRevision"`
	Template        *rules.Template `json:"template"`
	UserID          string          `json:"userId"`
	Format          string          `json:"format"`
}

type previewContext struct {
	User         *candidateUser `json:"user,omitempty"`
	AutoSelected bool           `json:"autoSelected"`
}

type previewMember struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Ref  string `json:"ref,omitempty"`
}

type previewGroup struct {
	Name    string          `json:"name"`
	Members []previewMember `json:"members"`
}

type profilePreviewResult struct {
	Body            string           `json:"body"`
	ContentType     string           `json:"contentType"`
	Format          string           `json:"format"`
	PreviewSource   string           `json:"previewSource"`
	ProfileID       string           `json:"profileId,omitempty"`
	ProfileRevision int64            `json:"profileRevision,omitempty"`
	TemplateDigest  string           `json:"templateDigest"`
	ActiveRevision  int64            `json:"activeRevision"`
	Context         previewContext   `json:"context"`
	Groups          []previewGroup   `json:"groups"`
	Warnings        []previewWarning `json:"warnings"`
	Diagnostic      string           `json:"diagnostic,omitempty"`
}

func (h *Handler) profilePreview(w http.ResponseWriter, r *http.Request) {
	var input profilePreviewInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	saved := input.ProfileID != "" && input.ProfileRevision != nil
	if input.Template != nil && (input.ProfileID != "" || input.ProfileRevision != nil) || input.Template == nil && !saved {
		httpapi.Error(w, 400, "invalid_request", "请提供草稿 template 或 profileId 与 profileRevision")
		return
	}
	result := profilePreviewResult{PreviewSource: "draft", Format: input.Format, Context: previewContext{AutoSelected: input.UserID == ""}, Groups: []previewGroup{}, Warnings: []previewWarning{}}
	if result.Format == "" {
		result.Format = "clash"
	}
	var template rules.Template
	if saved {
		state, err := h.Profiles.Profile(r.Context(), input.ProfileID)
		if err != nil {
			h.profileError(w, state, err)
			return
		}
		if state.Profile.Revision != *input.ProfileRevision {
			h.profileError(w, state, rules.ErrProfileConflict)
			return
		}
		template = state.Profile.Template
		result.PreviewSource, result.ProfileID, result.ProfileRevision = "saved", state.Profile.ID, state.Profile.Revision
		result.ActiveRevision = state.ActiveRevision
	} else {
		template = *input.Template
		if err := h.Profiles.ValidateTemplate(r.Context(), template); err != nil {
			h.profileError(w, rules.ProfileResult{}, err)
			return
		}
		active, err := h.Rules.Read(r.Context())
		if err != nil {
			h.internal(w, err)
			return
		}
		result.ActiveRevision = active.Revision
	}
	digest, err := rules.Digest(template)
	if err != nil {
		h.internal(w, err)
		return
	}
	result.TemplateDigest = digest
	userID, err := h.Service.previewUser(r.Context(), input.UserID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if userID == "" {
		result.Diagnostic = noPreviewUser
	} else {
		rendered, err := h.Service.render(r.Context(), userID, "", result.Format, "", nil, template)
		if err != nil {
			h.fail(w, err)
			return
		}
		result.Context.User = &rendered.User
		result.Body, result.ContentType = string(rendered.Body), rendered.ContentType
		if rendered.Resolved != nil {
			for _, group := range rendered.Resolved.Groups {
				item := previewGroup{Name: group.Name, Members: []previewMember{}}
				for _, member := range group.Members {
					value := previewMember{Kind: member.Kind, Name: member.Name}
					if member.Kind == rules.KindProxy {
						value.Ref = rendered.Resolved.EndpointRefs[member.Name]
					}
					item.Members = append(item.Members, value)
				}
				result.Groups = append(result.Groups, item)
			}
		}
		for _, warning := range rendered.Warnings {
			result.Warnings = append(result.Warnings, previewWarning{Stage: warning.Stage, Ref: warning.Ref, Message: warning.Message})
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.JSON(w, http.StatusOK, result)
}
