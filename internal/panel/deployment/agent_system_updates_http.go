package deployment

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

func (h *AgentUpdateHandler) systemQueue(w http.ResponseWriter, r *http.Request) {
	var input struct {
		NodeID        string `json:"nodeId"`
		ConfirmNodeID string `json:"confirmNodeId"`
		Revision      int64  `json:"revision"`
		TargetVersion string `json:"targetVersion"`
	}
	if httpapi.Decode(w, r, &input) != nil || input.NodeID == "" || input.ConfirmNodeID != input.NodeID {
		httpapi.Error(w, 400, "invalid_request", "请确认目标节点、正式版本和业务修订")
		return
	}
	task, err := h.Repository.QueueAgentSystemUpdate(r.Context(), h.Catalogue, input.NodeID, input.TargetVersion, auth.Actor(r.Context()).Username, r.RemoteAddr, input.Revision)
	if err != nil {
		if errors.Is(err, ErrAgentVersionUnsupported) {
			httpapi.Error(w, 400, "agent_version_unsupported", err.Error())
			return
		}
		httpapi.Error(w, 409, "agent_system_update_conflict", err.Error())
		return
	}
	httpapi.JSON(w, 202, task)
}
func (h *AgentUpdateHandler) systemRetry(w http.ResponseWriter, r *http.Request) {
	var input AgentRecoveryConfirmation
	if httpapi.Decode(w, r, &input) != nil {
		httpapi.Error(w, 400, "invalid_request", "请确认原任务和尝试编号")
		return
	}
	task, err := h.Repository.RetryAgentSystemUpdate(r.Context(), h.Catalogue, r.PathValue("id"), auth.Actor(r.Context()).Username, r.RemoteAddr, input)
	if err != nil {
		if errors.Is(err, ErrAgentVersionUnsupported) {
			httpapi.Error(w, 400, "agent_version_unsupported", err.Error())
			return
		}
		httpapi.Error(w, 409, "agent_system_update_conflict", err.Error())
		return
	}
	httpapi.JSON(w, 202, task)
}

var errLogPageFull = errors.New("log page full")

type logPageWriter struct {
	bytes.Buffer
	limit int
}

func (w *logPageWriter) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		n, _ := w.Buffer.Write(p[:w.limit-w.Len()])
		return n, errLogPageFull
	}
	return w.Buffer.Write(p)
}
func (h *AgentUpdateHandler) systemLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	offset := int64(0)
	var err error
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || offset < 0 {
			httpapi.Error(w, 400, "invalid_request", "日志偏移无效")
			return
		}
	}
	attempt := r.URL.Query().Get("attemptId")
	if r.URL.Query().Get("download") == "1" {
		var buffer bytes.Buffer
		_, complete, logError, readErr := h.Repository.WriteAgentSystemLogs(r.Context(), r.PathValue("id"), attempt, 0, &buffer)
		err = readErr
		if err != nil {
			httpapi.Error(w, 409, "agent_update_logs", err.Error())
			return
		}
		if !complete || logError != "" {
			if logError == "" {
				logError = "更新日志仍在写入或补传，完整输出尚未确认"
			}
			httpapi.Error(w, 409, "agent_update_logs_incomplete", logError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="agent-update.log"`)
		_, _ = io.Copy(w, &buffer)
		return
	}
	buffer := &logPageWriter{limit: 65536}
	next, complete, logError, err := h.Repository.WriteAgentSystemLogs(r.Context(), r.PathValue("id"), attempt, offset, buffer)
	if errors.Is(err, errLogPageFull) {
		err = nil
		complete = false
		// Keep an incomplete UTF-8 code point for the next byte-offset page.
		data := buffer.Bytes()
		start := len(data) - 1
		for start > 0 && !utf8.RuneStart(data[start]) && len(data)-start < 4 {
			start--
		}
		if start >= 0 && !utf8.FullRune(data[start:]) {
			next -= int64(len(data) - start)
			buffer.Truncate(start)
		}
	}
	if err != nil {
		httpapi.Error(w, 409, "agent_update_logs", err.Error())
		return
	}
	httpapi.JSON(w, 200, map[string]any{"output": buffer.String(), "nextOffset": next, "complete": complete, "error": logError})
}
