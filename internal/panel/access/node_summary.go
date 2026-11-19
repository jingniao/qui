package access

import (
	"errors"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
)

// nodeSummaries uses the same source and precision checks as AccountSummary.
func (h *Handler) nodeSummaries(w http.ResponseWriter, r *http.Request) {
	filter, err := filterRequest(r)
	if err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if filter.UserID != "" || filter.DeviceID != "" || filter.Domain != "" || filter.Allowed != nil {
		httpapi.Error(w, 400, "invalid_request", "Node summaries only accept node and time filters")
		return
	}
	totals, err := ReadNodeTraffic(r.Context(), h.Repository.Database, filter.From, filter.To, filter.NodeID, false)
	if err != nil {
		if errors.Is(err, ErrTrafficResolution) {
			httpapi.Error(w, 422, "traffic_history_resolution", err.Error())
			return
		}
		h.fail(w)
		return
	}
	type item struct {
		NodeID string `json:"nodeId"`
		AccountSummary
	}
	items := []item{}
	for _, v := range totals {
		items = append(items, item{v.NodeID, AccountSummary{UploadBytes: v.Upload, DownloadBytes: v.Download, Observations: v.Samples}})
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
