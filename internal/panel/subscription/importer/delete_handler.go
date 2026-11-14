package importer

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) deletionPreview(w http.ResponseWriter, r *http.Request) {
	result, err := h.Store.DeletionPreview(r.Context(), r.PathValue("id"))
	if err != nil {
		h.deleteFailure(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}
func (h *Handler) deleteProxy(w http.ResponseWriter, r *http.Request) {
	var in DeleteInput
	if httpapi.Decode(w, r, &in) != nil {
		httpapi.Error(w, 400, "invalid_json", "请求内容无效，请检查字段与 JSON 格式")
		return
	}
	if err := h.Store.DeleteProxy(r.Context(), r.PathValue("id"), in); err != nil {
		h.deleteFailure(w, r, err)
		return
	}
	if !h.record(w, r, "删除导入节点", r.PathValue("id")) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) deleteFailure(w http.ResponseWriter, r *http.Request, err error) {
	var conflict *DeletionConflict
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Error(w, 404, "import_node_missing", "导入节点不存在或已被删除")
	case errors.Is(err, ErrDeleteUnsupported):
		httpapi.Error(w, 409, "import_delete_unsupported", ErrDeleteUnsupported.Error())
	case errors.Is(err, ErrRenameStale):
		httpapi.Error(w, 409, "stale", "节点已被修改，请刷新后重试")
	case errors.As(err, &conflict):
		httpapi.ErrorDetails(w, 409, conflict.Code, conflict.Message, conflict.Details)
	default:
		slog.ErrorContext(r.Context(), "delete imported proxy", "id", r.PathValue("id"), "error", err)
		httpapi.Error(w, 500, "import_delete_failed", "删除导入节点失败")
	}
}
