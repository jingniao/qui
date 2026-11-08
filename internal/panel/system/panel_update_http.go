package system

import (
	"errors"
	"net/http"

	"proxycontrol/internal/platform/httpapi"
)

// panelUpdateStatus answers what the 系统信息 card shows: the running version, the
// newest official release, a request the helper has not picked up yet and the last
// status the helper wrote. Only the Owner sees this card, so the read is Owner-only.
// refresh=1 bypasses the release cache for the 检查更新 button.
func (h *Handler) panelUpdateStatus(w http.ResponseWriter, r *http.Request) {
	state := h.PanelUpdate.State(r.Context(), r.URL.Query().Get("refresh") == "1")
	httpapi.JSON(w, 200, state)
}

// startPanelUpdate asks the root helper to replace the panel. The panel only writes
// the request file; the helper backs up the database, runs the update, health-checks
// it and rolls back on failure.
func (h *Handler) startPanelUpdate(w http.ResponseWriter, r *http.Request) {
	if h.ActorName == nil {
		httpapi.Error(w, 500, "internal_error", "Settings audit identity unavailable")
		return
	}
	var input struct {
		Version string `json:"version"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	// A malformed version is a request-shape error, so it is refused before the state
	// checks: no request file is written for a version the updater cannot download.
	if !officialVersionPattern.MatchString(input.Version) {
		httpapi.Error(w, 400, "invalid_request", "要更新的版本必须形如 1.2.3 的正式版本号。")
		return
	}
	actor := h.ActorName(r.Context())
	request, err := h.PanelUpdate.Submit(r.Context(), input.Version, actor)
	if err != nil {
		h.failPanelUpdate(w, err)
		return
	}
	// The request file is in place, so the update will run; a missing audit row is
	// reported in the log instead of failing a submission that already happened. The
	// row says 成功 because admin_operations.result only accepts 成功 or 失败: the
	// audit records that the request was accepted, and the outcome is in status.json.
	if err := h.Repository.Record(r.Context(), actor, "更新面板", "panel", "成功", r.RemoteAddr); err != nil && h.Logger != nil {
		h.Logger.Error("panel update audit row failed")
	}
	httpapi.JSON(w, 202, map[string]string{"requestId": request.RequestID, "version": request.Version})
}

// failPanelUpdate maps the submission errors to the fixed codes the card shows. The
// upstream release text never reaches the browser.
func (h *Handler) failPanelUpdate(w http.ResponseWriter, err error) {
	var unsupported PanelUpdateUnsupportedError
	if errors.As(err, &unsupported) {
		message := "当前面板版本不是正式版（开发版本或候选版本），不支持网页更新。"
		if unsupported.Reason == "helper_missing" {
			message = "这台面板还没有安装网页更新组件：请先在服务器上执行一次 qui-update --component panel，之后即可在这里更新。"
		}
		httpapi.ErrorDetails(w, 409, "panel_update_unsupported", message, map[string]any{"reason": unsupported.Reason})
		return
	}
	if errors.Is(err, ErrPanelUpdateBusy) {
		httpapi.Error(w, 409, "panel_update_busy", "已有待处理或正在进行的面板更新，请等待完成后重试。")
		return
	}
	if errors.Is(err, ErrPanelUpdateCheckFailed) {
		httpapi.Error(w, 502, "panel_update_check_failed", "无法获取最新正式版本，请稍后重试。")
		return
	}
	var stale PanelUpdateStaleError
	if errors.As(err, &stale) {
		httpapi.ErrorDetails(w, 409, "panel_update_stale", "要更新的版本已不是当前最新正式版，请刷新后重试。",
			map[string]any{"latestVersion": stale.Latest, "currentVersion": h.PanelUpdate.CurrentVersion})
		return
	}
	if h.Logger != nil {
		h.Logger.Error("panel update request failed")
	}
	httpapi.Error(w, 500, "internal_error", "面板更新请求未能写入，请稍后重试。")
}
