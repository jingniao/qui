package deployment

import (
	"errors"
	"net/http"

	"proxycontrol/internal/platform/httpapi"
)

// coreReleases answers GET /api/v1/core-releases?component=&source=[&refresh=1]
// for the Owner's add/upgrade core dialogs. A lookup failure is one fixed code;
// the upstream status and body are never echoed.
func (h *CoreUpdateHandler) coreReleases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := r.URL.Query()
	service := h.Releases
	if service == nil {
		service = defaultCoreReleases
	}
	list, err := service.List(r.Context(), query.Get("component"), query.Get("source"), query.Get("refresh") == "1")
	if errors.Is(err, ErrCoreReleaseSource) {
		httpapi.Error(w, 400, "invalid_request", "Unknown core component or source")
		return
	}
	if err != nil {
		httpapi.Error(w, 502, "core_release_lookup_failed", "Official release information is unavailable")
		return
	}
	httpapi.JSON(w, 200, list)
}

var defaultCoreReleases = &CoreReleaseService{}
