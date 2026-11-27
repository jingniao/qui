package deployment

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/pkg/contracts/desiredstate"
)

type nodePublishPreviewer interface {
	PreviewNodePublication(context.Context, string, proxy.AccessReader, func(context.Context, string) (desiredstate.State, []string, error)) ([]proxy.NodeSnapshot, error)
}

type nodePublishChoice struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}

func (h *Handler) previewNodePublish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	items, unavailable := []nodePublishChoice{}, []nodePublishChoice{}
	const size = 100
	for page := 1; ; page++ {
		nodes, total, err := h.Nodes.Choices(ctx, "", page, size)
		if err != nil {
			h.fail(w, err)
			return
		}
		for _, n := range nodes {
			if !n.Enabled {
				continue
			}
			item := nodePublishChoice{ID: n.ID, Name: n.Name}
			changed, err := h.nodePublicationChanged(ctx, n.ID)
			if err != nil {
				item.Error = err.Error()
				unavailable = append(unavailable, item)
			} else if changed {
				items = append(items, item)
			}
		}
		if page*size >= total {
			break
		}
	}
	httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "unavailable": unavailable})
}

func (h *Handler) nodePublicationChanged(ctx context.Context, nodeID string) (bool, error) {
	previewer, ok := h.Proxy.(nodePublishPreviewer)
	if !ok || h.Users == nil {
		return false, errors.New("节点发布预览不可用")
	}
	snapshots, err := previewer.PreviewNodePublication(ctx, nodeID, h.Users, h.Repository.PublishedWithIntent)
	if errors.Is(err, proxy.ErrPublicationAllocationNeeded) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, snapshot := range snapshots {
		published, intent, err := h.Repository.PublishedWithIntent(ctx, snapshot.State.NodeID)
		if errors.Is(err, sql.ErrNoRows) {
			// An unused node with only the defaults has no configuration
			// changes to publish, even if it has never applied a revision.
			published = desiredstate.Baseline(snapshot.State.NodeID, nil)
			intent = []string{}
		} else if err != nil {
			return false, err
		}
		if !sameDesiredState(published, snapshot.State) || !sameInboundIntent(intent, snapshot.InboundIntent) {
			return true, nil
		}
	}
	return false, nil
}
