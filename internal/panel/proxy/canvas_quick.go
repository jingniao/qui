package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

type QuickCanvasEntryGroups struct {
	Mode     string   `json:"mode"`
	GroupIDs []string `json:"groupIds,omitempty"`
}

type QuickCanvasInput struct {
	RequestID     string                 `json:"requestId"`
	EntryNodeID   string                 `json:"entryNodeId"`
	MiddleNodeIDs []string               `json:"middleNodeIds"`
	ExitInboundID string                 `json:"exitInboundId"`
	EntryProtocol string                 `json:"entryProtocol"`
	EntryCore     string                 `json:"entryCore,omitempty"`
	EntryGroups   QuickCanvasEntryGroups `json:"entryGroups"`
}

type QuickCanvasResult struct {
	SchemeID    string              `json:"schemeId"`
	Publication CanvasPublishResult `json:"publication"`
	Warnings    []string            `json:"warnings"`
}

func (s *Service) quickCanvasScheme(ctx context.Context, input QuickCanvasInput) (CanvasScheme, error) {
	if !validCanvasToken(input.RequestID) {
		return CanvasScheme{}, errors.New("requestId 必填，须为不超过 128 字节的字母、数字、点、下划线或短横线")
	}
	if !validCanvasToken(input.EntryNodeID) || !validCanvasToken(input.ExitInboundID) {
		return CanvasScheme{}, errors.New("请选择有效的入口服务器和出口入站")
	}
	if strings.TrimSpace(input.EntryProtocol) == "" {
		return CanvasScheme{}, errors.New("新建入口必须选择协议")
	}
	exit, err := s.Repository.Inbound(ctx, input.ExitInboundID, false)
	if err != nil {
		return CanvasScheme{}, fmt.Errorf("读取出口入站失败：%w", err)
	}
	entryNode, err := s.Nodes.Get(ctx, input.EntryNodeID)
	if err != nil {
		return CanvasScheme{}, fmt.Errorf("读取入口服务器失败：%w", err)
	}
	exitNode, err := s.Nodes.Get(ctx, exit.NodeID)
	if err != nil {
		return CanvasScheme{}, fmt.Errorf("读取出口服务器失败：%w", err)
	}
	groups := input.EntryGroups
	if groups.Mode == "" {
		groups.Mode = "exit_groups"
	}
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "server:" + input.EntryNodeID, Role: "entry", X: 40, Y: 120, EntryBinding: &CanvasEntryBinding{Mode: "create", Protocol: input.EntryProtocol, Core: input.EntryCore, PortMode: "auto", GroupMode: groups.Mode, GroupIDs: groups.GroupIDs}}}}
	for index, nodeID := range input.MiddleNodeIDs {
		if !validCanvasToken(nodeID) {
			return CanvasScheme{}, errors.New("中间服务器 ID 无效")
		}
		layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: fmt.Sprintf("middle-%d", index), Ref: "server:" + nodeID, Role: "relay", X: float64(320 + index*280), Y: 120, RelayPortMode: "auto"})
	}
	layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "exit", Ref: "inbound:" + exit.ID, Role: "exit", X: float64(320 + len(input.MiddleNodeIDs)*280), Y: 120})
	for index := 1; index < len(layout.Instances); index++ {
		// Canvas defaults to TLS. TCP-only also accepts exits whose protocol
		// cannot relay UDP; the ordinary planner enforces its chain hop limit.
		layout.Connections = append(layout.Connections, CanvasConnection{ID: fmt.Sprintf("connection-%d", index), Source: layout.Instances[index-1].InstanceID, Target: layout.Instances[index].InstanceID, Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true})
	}
	return CanvasScheme{Name: entryNode.Name + " → " + exitNode.Name, Execution: canvasExecutionServer, Layout: layout}, nil
}

func (s *Service) PlanQuickCanvas(ctx context.Context, input QuickCanvasInput) (CanvasPlanResult, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	scheme, err := s.quickCanvasScheme(ctx, input)
	if err != nil {
		return CanvasPlanResult{}, err
	}
	return s.planCanvas(ctx, scheme)
}

// QuickCanvas composes the existing save and publish services under canvasMu.
// The binding's unique request_id survives restarts and canvas layout edits.
// Replays read the original entry and deployment batch, never publish again.
func (s *Service) QuickCanvas(ctx context.Context, input QuickCanvasInput, author string, users AccessReader) (QuickCanvasResult, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	if !validCanvasToken(input.RequestID) {
		return QuickCanvasResult{}, errors.New("requestId 必填且格式无效")
	}
	record, err := scanCanvasEntryBinding(s.Repository.Database.QueryRowContext(ctx, "SELECT "+canvasEntryBindingColumns+" FROM canvas_entry_bindings WHERE request_id=?", input.RequestID))
	if err == nil {
		return s.quickCanvasExisting(ctx, record)
	}
	if !errors.Is(err, ErrNotFound) {
		return QuickCanvasResult{}, err
	}
	scheme, err := s.quickCanvasScheme(ctx, input)
	if err != nil {
		return QuickCanvasResult{}, err
	}
	preview, err := s.planCanvas(ctx, scheme)
	if err != nil {
		return QuickCanvasResult{}, err
	}
	if !preview.OK {
		return QuickCanvasResult{}, fmt.Errorf("中转规划失败：%s", preview.Message)
	}
	saved, err := s.saveCanvasScheme(ctx, "", CanvasSchemeInput{Name: scheme.Name, Execution: scheme.Execution, Layout: scheme.Layout})
	if err != nil {
		return QuickCanvasResult{}, err
	}
	undo := &canvasUndo{}
	publication, err := s.publishCanvasScheme(ctx, saved.ID, author, users, undo, input.RequestID)
	result := QuickCanvasResult{SchemeID: saved.ID, Publication: publication, Warnings: append([]string{}, publication.Warnings...)}
	if err == nil {
		return result, nil
	}
	if publication.BatchID != "" {
		// StartBatch commits its queued work and status together. A draft batch
		// failed before that commit and can still be undone as a failed create.
		var status string
		cleanupCtx := context.WithoutCancel(ctx)
		if statusErr := s.Repository.Database.QueryRowContext(cleanupCtx, "SELECT status FROM deployment_batches WHERE id=?", publication.BatchID).Scan(&status); statusErr != nil {
			return result, fmt.Errorf("%w（读取发布批次状态失败：%v）", err, statusErr)
		}
		if status != "draft" {
			_, stampErr := s.Repository.Database.ExecContext(cleanupCtx, "UPDATE canvas_schemes SET published_batch_id=? WHERE id=?", publication.BatchID, saved.ID)
			if stampErr != nil {
				return result, fmt.Errorf("%w（记录发布批次失败：%v）", err, stampErr)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("发布批次 %s 已启动，方案保留，请查看批次回滚状态：%v", publication.BatchID, err))
			return result, nil
		}
		if _, deleteErr := s.Repository.Database.ExecContext(cleanupCtx, "DELETE FROM deployment_batches WHERE id=? AND status='draft'", publication.BatchID); deleteErr != nil {
			return result, fmt.Errorf("%w（清理未启动批次失败：%v）", err, deleteErr)
		}
	}
	if undoErr := undo.run(ctx); undoErr != nil {
		return result, fmt.Errorf("%w（撤销本次修改时出错：%v，请检查方案生成的对象）", err, undoErr)
	}
	if deleteErr := s.Repository.DeleteCanvasScheme(context.WithoutCancel(ctx), saved.ID); deleteErr != nil {
		return result, fmt.Errorf("%w（清理本次方案失败：%v）", err, deleteErr)
	}
	// These inline tunnels were created by this request while canvasMu was
	// held. The undo has removed their objects and no other scheme can own them.
	if _, deleteErr := s.Repository.Database.ExecContext(context.WithoutCancel(ctx), "DELETE FROM canvas_tunnels WHERE inline_scheme_id=?", saved.ID); deleteErr != nil {
		return result, fmt.Errorf("%w（清理本次连线失败：%v）", err, deleteErr)
	}
	return QuickCanvasResult{}, err
}

func (s *Service) quickCanvasExisting(ctx context.Context, binding CanvasEntryBindingRecord) (QuickCanvasResult, error) {
	if binding.SchemeID == nil {
		return QuickCanvasResult{}, errors.New("此 requestId 的方案已删除，原入口仍保留，请先处理遗留入口")
	}
	scheme, err := s.Repository.CanvasScheme(ctx, *binding.SchemeID)
	if err != nil {
		return QuickCanvasResult{}, err
	}
	inbound, err := s.Repository.Inbound(ctx, binding.InboundID, false)
	if err != nil {
		return QuickCanvasResult{}, err
	}
	n, err := s.Nodes.Get(ctx, inbound.NodeID)
	if err != nil {
		return QuickCanvasResult{}, err
	}
	groups, err := s.canvasEntryGroups(ctx, inbound.ID, false)
	if err != nil {
		return QuickCanvasResult{}, err
	}
	publication := CanvasPublishResult{BatchID: scheme.PublishedBatchID, Entries: []CanvasPlanEntry{{InstanceID: binding.InstanceID, Mode: binding.Mode, NodeID: inbound.NodeID, NodeName: n.Name, InboundID: inbound.ID, Protocol: inbound.Protocol, Port: inbound.Port, Name: inbound.Name, Route: inbound.DefaultOutbound, Groups: groups}}, Nodes: []CanvasPublishNode{}, Warnings: []string{}}
	if scheme.PublishedBatchID != "" {
		rows, err := s.Repository.Database.QueryContext(ctx, "SELECT i.node_id,n.name,i.revision,i.status FROM deployment_batch_items i JOIN nodes n ON n.id=i.node_id WHERE i.batch_id=? ORDER BY i.position", scheme.PublishedBatchID)
		if err != nil {
			return QuickCanvasResult{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var item CanvasPublishNode
			if err := rows.Scan(&item.NodeID, &item.Name, &item.Revision, &item.Status); err != nil {
				return QuickCanvasResult{}, err
			}
			publication.Nodes = append(publication.Nodes, item)
		}
		if err := rows.Err(); err != nil {
			return QuickCanvasResult{}, err
		}
	} else if scheme.PublishedAt == "" {
		return QuickCanvasResult{}, errors.New("此 requestId 已创建入口但发布尚未完成，请在原方案中检查发布状态")
	}
	return QuickCanvasResult{SchemeID: scheme.ID, Publication: publication, Warnings: []string{}}, nil
}

func (h *Handler) planQuickCanvas(w http.ResponseWriter, r *http.Request) {
	var input QuickCanvasInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	result, err := h.Service.PlanQuickCanvas(r.Context(), input)
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}

func (h *Handler) quickCanvas(w http.ResponseWriter, r *http.Request) {
	var input QuickCanvasInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	result, err := h.Service.QuickCanvas(r.Context(), input, auth.Actor(r.Context()).Username, h.Users)
	if err != nil {
		h.failCanvas(w, err)
		return
	}
	if err := h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "快速创建中转方案", result.SchemeID, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, http.StatusAccepted, result)
}
