package system

import (
	"context"
	"io"
	"net/http"
	"time"

	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/release"
)

type ReleaseHandler struct {
	Repository *Repository
	Auth       interface {
		Require(http.Handler, bool) http.Handler
	}
	ActorName func(context.Context) string
	Keys      map[string]release.TrustedKey
}

func (h *ReleaseHandler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/releases", h.Auth.Require(http.HandlerFunc(h.list), true))
	mux.Handle("POST /api/v1/releases", h.Auth.Require(http.HandlerFunc(h.save), true))
}
func (h *ReleaseHandler) list(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page, size, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid pagination")
		return
	}
	items, total, err := h.Repository.ListReleases(r.Context(), page, size)
	if err != nil {
		httpapi.Error(w, 500, "internal_error", "Release catalogue unavailable")
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "pageSize": size, "configured": len(h.Keys) > 0})
}
func (h *ReleaseHandler) save(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	data, err := io.ReadAll(io.LimitReader(r.Body, release.MaxManifestBytes+1))
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", "Invalid signed manifest")
		return
	}
	signed, err := release.Decode(data)
	if err == nil {
		m := signed.Manifest
		err = signed.Verify(release.Policy{Component: m.Component, OS: m.OS, Architecture: m.Architecture, Now: time.Now().UTC(), Keys: h.Keys})
	}
	if err != nil {
		httpapi.Error(w, 400, "release_untrusted", "Signed release is invalid, expired or untrusted")
		return
	}
	entry, err := h.Repository.SaveRelease(r.Context(), signed, h.Keys[signed.KeyID].PublicKey, h.ActorName(r.Context()), r.RemoteAddr)
	if err != nil {
		httpapi.Error(w, 409, "release_conflict", "Release sequence or catalogue record conflicts")
		return
	}
	httpapi.JSON(w, 201, entry)
}
