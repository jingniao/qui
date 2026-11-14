package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type CanvasStatusDraft struct {
	Dirty       bool   `json:"dirty"`
	UpdatedAt   string `json:"updatedAt"`
	PublishedAt string `json:"publishedAt"`
}

type CanvasStatusBatchItem struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Status   string `json:"status"`
	Revision int64  `json:"revision,omitempty"`
}

type CanvasStatusBatch struct {
	ID     string                  `json:"id,omitempty"`
	Status string                  `json:"status,omitempty"`
	Items  []CanvasStatusBatchItem `json:"items"`
}

// CanvasStatusReader keeps deployment batch ownership in the deployment module.
type CanvasStatusReader interface {
	CanvasBatch(context.Context, string) (CanvasStatusBatch, error)
}

type CanvasStatusNode struct {
	NodeID            string `json:"nodeId"`
	NodeName          string `json:"nodeName"`
	ConfirmedRevision int64  `json:"confirmedRevision,omitempty"`
	ExpectedRevision  int64  `json:"expectedRevision,omitempty"`
	Confirmed         bool   `json:"confirmed"`
}

type CanvasStatusEntry struct {
	InstanceID string `json:"instanceId"`
	InboundID  string `json:"inboundId,omitempty"`
	Name       string `json:"name,omitempty"`
	Mode       string `json:"mode,omitempty"`
	State      string `json:"state,omitempty"`
	Route      string `json:"route,omitempty"`
}

type CanvasStatusSubscription struct {
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
}

type CanvasStatusProbe struct {
	Status  string                    `json:"status"`
	At      string                    `json:"at,omitempty"`
	Results []CanvasStatusProbeResult `json:"results,omitempty"`
}

// These are TCP observations of an entry socket, never end-to-end route tests.
type CanvasStatusProbeResult struct {
	InboundID string   `json:"inboundId"`
	NodeID    string   `json:"nodeId"`
	Status    string   `json:"status"`
	At        string   `json:"at"`
	Attempted uint32   `json:"attempted"`
	Lost      uint32   `json:"lost"`
	MedianMs  *float64 `json:"medianMs,omitempty"`
	Code      string   `json:"code"`
}

type CanvasStatus struct {
	Draft        CanvasStatusDraft        `json:"draft"`
	Batch        CanvasStatusBatch        `json:"batch"`
	Nodes        []CanvasStatusNode       `json:"nodes"`
	Entries      []CanvasStatusEntry      `json:"entries"`
	Subscription CanvasStatusSubscription `json:"subscription"`
	Probe        CanvasStatusProbe        `json:"probe"`
}

// CanvasSchemeStatus projects stored facts only. Subscription readiness describes
// administrator-level conditions, not any individual user's authorization.
func (s *Service) CanvasSchemeStatus(ctx context.Context, schemeID string) (CanvasStatus, error) {
	// No canvasMu: a status poll must not wait behind a publish holding it.
	result := CanvasStatus{Batch: CanvasStatusBatch{Items: []CanvasStatusBatchItem{}}, Nodes: []CanvasStatusNode{}, Entries: []CanvasStatusEntry{}, Subscription: CanvasStatusSubscription{Reasons: []string{}}, Probe: CanvasStatusProbe{Status: "untested"}}
	scheme, err := s.Repository.CanvasScheme(ctx, schemeID)
	if err != nil {
		return result, err
	}
	result.Draft = CanvasStatusDraft{Dirty: scheme.PublishedAt == "", UpdatedAt: scheme.UpdatedAt, PublishedAt: scheme.PublishedAt}
	if scheme.PublishedAt != "" {
		updated, err := time.Parse(time.RFC3339Nano, scheme.UpdatedAt)
		if err != nil {
			return result, fmt.Errorf("canvas updated_at: %w", err)
		}
		published, err := time.Parse(time.RFC3339Nano, scheme.PublishedAt)
		if err != nil {
			return result, fmt.Errorf("canvas published_at: %w", err)
		}
		result.Draft.Dirty = updated.After(published)
	}
	if scheme.PublishedBatchID != "" {
		reader, ok := s.Deploy.(CanvasStatusReader)
		if !ok {
			return result, errors.New("canvas deployment status reader unavailable")
		}
		result.Batch, err = reader.CanvasBatch(ctx, scheme.PublishedBatchID)
		if err != nil {
			return result, err
		}
	}
	nodeIDs := []string{}
	expected := map[string]int64{}
	for index := range result.Batch.Items {
		item := &result.Batch.Items[index]
		n, err := s.Nodes.Get(ctx, item.NodeID)
		if err != nil {
			return result, err
		}
		item.NodeName = n.Name
		nodeIDs = append(nodeIDs, item.NodeID)
		expected[item.NodeID] = item.Revision
	}
	for _, instance := range scheme.Layout.Instances {
		nodeID, err := s.canvasManagedNode(ctx, instance)
		if err != nil {
			return result, err
		}
		if nodeID == "" {
			continue
		}
		if _, found := expected[nodeID]; !found {
			nodeIDs = append(nodeIDs, nodeID)
			expected[nodeID] = 0
		}
	}
	for _, nodeID := range nodeIDs {
		n, err := s.Nodes.Get(ctx, nodeID)
		if err != nil {
			return result, err
		}
		item := CanvasStatusNode{NodeID: nodeID, NodeName: n.Name, ExpectedRevision: expected[nodeID]}
		err = s.Repository.Database.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", nodeID).Scan(&item.ConfirmedRevision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		item.Confirmed = item.ExpectedRevision != 0 && item.ExpectedRevision == item.ConfirmedRevision
		result.Nodes = append(result.Nodes, item)
	}
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT "+canvasEntryBindingColumns+" FROM canvas_entry_bindings WHERE scheme_id=? ORDER BY instance_id,id", schemeID)
	if err != nil {
		return result, err
	}
	bindings := []CanvasEntryBindingRecord{}
	for rows.Next() {
		binding, err := scanCanvasEntryBinding(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		bindings = append(bindings, binding)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(bindings) == 0 {
		result.Subscription.Reasons = append(result.Subscription.Reasons, "用户入口未绑定或尚未创建")
	}
	for _, binding := range bindings {
		entry := CanvasStatusEntry{InstanceID: binding.InstanceID, InboundID: binding.InboundID, Mode: binding.Mode, State: binding.State}
		inbound, err := s.Repository.Inbound(ctx, binding.InboundID, false)
		if errors.Is(err, ErrNotFound) {
			result.Subscription.Reasons = append(result.Subscription.Reasons, "入口 "+binding.InstanceID+" 的入站不存在")
			result.Entries = append(result.Entries, entry)
			continue
		}
		if err != nil {
			return result, err
		}
		entry.Name, entry.Route = inbound.Name, inbound.DefaultOutbound
		result.Entries = append(result.Entries, entry)
		reasons, err := s.canvasEntrySubscriptionReasons(ctx, inbound)
		if err != nil {
			return result, err
		}
		result.Subscription.Reasons = append(result.Subscription.Reasons, reasons...)
		probe, err := s.canvasEntryProbe(ctx, inbound)
		if err != nil {
			return result, err
		}
		if probe != nil && (result.Probe.At == "" || probe.At > result.Probe.At) {
			result.Probe = CanvasStatusProbe{Status: probe.Status, At: probe.At, Results: []CanvasStatusProbeResult{*probe}}
		}
	}
	if result.Batch.Status != "success" {
		result.Subscription.Reasons = append(result.Subscription.Reasons, "部署批次尚未成功")
	}
	result.Subscription.Ready = len(result.Subscription.Reasons) == 0
	return result, nil
}

func (s *Service) canvasEntryProbe(ctx context.Context, inbound Inbound) (*CanvasStatusProbeResult, error) {
	n, err := s.Nodes.Get(ctx, inbound.NodeID)
	if err != nil {
		return nil, err
	}
	host := n.ConnectHost
	if host == "" {
		host = n.PublicIP
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return nil, nil
	} // DNS targets have no persisted resolution-to-entry identity.
	var raw string
	result := CanvasStatusProbeResult{InboundID: inbound.ID}
	err = s.Repository.Database.QueryRowContext(ctx, `SELECT p.node_id,p.created_at,p.result_json FROM node_tcp_probes p
 WHERE p.status='complete' AND p.result_json IS NOT NULL AND json_extract(p.command_json,'$.port')=?
 AND json_array_length(p.command_json,'$.addresses')=1
 AND EXISTS(SELECT 1 FROM json_each(p.command_json,'$.addresses') a WHERE a.value=?)
 ORDER BY p.created_at DESC,p.id DESC LIMIT 1`, inbound.Port, address.String()).Scan(&result.NodeID, &result.At, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recorded pb.TCPProbeResult
	if err := json.Unmarshal([]byte(raw), &recorded); err != nil {
		return nil, fmt.Errorf("entry TCP probe result: %w", err)
	}
	result.Attempted, result.Lost, result.MedianMs, result.Code = recorded.Attempted, recorded.Lost, recorded.MedianMs, recorded.Code
	result.Status = "unreachable"
	if result.Attempted == 0 || result.Code == "cancelled" || result.Code == "invalid" {
		result.Status = "incomplete"
	} else if result.Attempted > result.Lost {
		result.Status = "reachable"
	}
	return &result, nil
}

func (s *Service) canvasEntrySubscriptionReasons(ctx context.Context, inbound Inbound) ([]string, error) {
	reasons := []string{}
	add := func(reason string) { reasons = append(reasons, "入口「"+inbound.Name+"」"+reason) }
	if !inbound.Enabled {
		add("未启用")
	}
	if !inbound.SubscriptionVisible {
		add("订阅不可见")
	}
	var total, eligible, enabled int
	err := s.Repository.Database.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(g.purpose IN ('subscription','mixed')),0),COALESCE(SUM(g.purpose IN ('subscription','mixed') AND g.enabled=1),0)
 FROM node_group_inbounds i JOIN node_groups g ON g.id=i.group_id WHERE i.inbound_id=?`, inbound.ID).Scan(&total, &eligible, &enabled)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		add("未加入节点分组")
	} else if eligible == 0 {
		add("所在分组用途须为订阅或混合")
	} else if enabled == 0 {
		add("所在订阅分组未启用")
	}
	n, err := s.Nodes.Get(ctx, inbound.NodeID)
	if err != nil {
		return nil, err
	}
	if n.ConnectHost == "" && n.PublicIP == "" {
		add("服务器缺少客户端连接地址")
	}
	return reasons, nil
}

func (h *Handler) canvasSchemeStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.Service.CanvasSchemeStatus(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.failCanvas(w, err)
		} else {
			h.internal(w, err)
		}
		return
	}
	httpapi.JSON(w, http.StatusOK, status)
}
