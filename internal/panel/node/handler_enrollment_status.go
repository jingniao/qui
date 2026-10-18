package node

import (
	"errors"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

func (h *Handler) enrollmentStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	result, err := h.Repository.EnrollmentStatus(r.Context(), r.PathValue("id"), h.Authority)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httpapi.Error(w, 404, "not_found", "节点不存在")
		case errors.Is(err, ErrToken), errors.Is(err, ErrSequence):
			httpapi.Error(w, 409, "enrollment_material_invalid", "原登记回执、身份或消息材料无法核对，保留原记录")
		default:
			httpapi.Error(w, 500, "enrollment_status_unavailable", "登记阶段暂不可用，请核对数据库迁移和原回执存储")
		}
		return
	}
	httpapi.JSON(w, 200, result)
}
