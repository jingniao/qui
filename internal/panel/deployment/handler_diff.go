package deployment

import (
	"database/sql"
	"errors"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	"strconv"
)

func (h *Handler) diff(w http.ResponseWriter, r *http.Request) {
	revision, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || revision < 1 {
		httpapi.Error(w, 400, "invalid_request", "Invalid revision ID")
		return
	}
	var base *int64
	if values, exists := r.URL.Query()["baseRevision"]; exists {
		if len(values) != 1 {
			httpapi.Error(w, 400, "invalid_request", "Exactly one comparison revision required")
			return
		}
		id, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || id < 0 {
			httpapi.Error(w, 400, "invalid_request", "Invalid comparison revision")
			return
		}
		base = &id
	}
	result, err := h.Repository.Diff(r.Context(), revision, base)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Error(w, 404, "not_found", "Comparison revision not found")
		return
	}
	if errors.Is(err, ErrDiffNode) {
		httpapi.Error(w, 400, "invalid_request", "Comparison revisions must belong to the same node")
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, result)
}
