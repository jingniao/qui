package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PublishCanvasScheme is the only way a drawing reaches the nodes. It translates
// the scheme through this module (canvasApplyPlan), snapshots every node the
// publication depends on and hands them to the Deployment module, which creates
// and starts one batch (or a single revision for a one-node drawing) in
// dependency order: targets first, the entry node last.
func (s *Service) PublishCanvasScheme(ctx context.Context, schemeID, author string, users AccessReader) (CanvasPublishResult, error) {
	if users == nil {
		return CanvasPublishResult{}, errors.New("access reader required")
	}
	if s.Deploy == nil {
		return CanvasPublishResult{}, errors.New("deployment unavailable")
	}
	scheme, err := s.Repository.CanvasScheme(ctx, schemeID)
	if err != nil {
		return CanvasPublishResult{}, err
	}
	if scheme.Execution == canvasExecutionClient {
		if err := ValidateClientLayout(scheme.Layout); err != nil {
			return CanvasPublishResult{}, err
		}
		for _, i := range scheme.Layout.Instances {
			ref, _ := parseCanvasRef(i.Ref)
			if ref.kind == "import" {
				var count int
				if err := s.Repository.Database.QueryRowContext(ctx, "SELECT count(*) FROM imported_proxies WHERE id=?", ref.id).Scan(&count); err != nil {
					return CanvasPublishResult{}, err
				}
				if count == 0 {
					return CanvasPublishResult{}, errors.New("客户端方案引用了不存在的导入节点")
				}
			}
		}
		if err := s.validateRefs(ctx, scheme.Layout); err != nil {
			return CanvasPublishResult{}, err
		}
		if err := s.Repository.PublishClientCanvas(ctx, scheme); err != nil {
			return CanvasPublishResult{}, err
		}
		return CanvasPublishResult{Nodes: []CanvasPublishNode{}, Warnings: []string{"客户端链仅在 mihomo 订阅中生效；每一跳均须授权给订阅用户"}}, nil
	}
	plan, err := s.canvasBuildPlan(ctx, scheme)
	if err != nil {
		return CanvasPublishResult{}, err
	}
	warnings, err := s.canvasApplyPlan(ctx, plan)
	if err != nil {
		return CanvasPublishResult{}, err
	}
	snapshots, err := s.canvasSnapshots(ctx, plan.nodes, users)
	if err != nil {
		return CanvasPublishResult{}, err
	}
	result, err := s.Deploy.PublishCanvas(ctx, "方案发布 "+scheme.Name, author, snapshots)
	if err != nil {
		return CanvasPublishResult{}, err
	}
	if err := s.Repository.MarkCanvasPublished(ctx, scheme.ID, result.BatchID); err != nil {
		return CanvasPublishResult{}, err
	}
	for index := range result.Nodes {
		if n, err := s.Nodes.Get(ctx, result.Nodes[index].NodeID); err == nil {
			result.Nodes[index].Name = n.Name
		}
	}
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	result.Warnings = append(result.Warnings, warnings...)
	return result, nil
}

func inboundInputOf(inbound Inbound, defaultOutbound string) InboundInput {
	return InboundInput{Name: inbound.Name, NodeID: inbound.NodeID, Protocol: inbound.Protocol, Listen: inbound.Listen, Port: inbound.Port, Enabled: inbound.Enabled, SubscriptionVisible: inbound.SubscriptionVisible, InternalRelay: inbound.InternalRelay, DefaultOutbound: defaultOutbound, AllowedUserIDs: inbound.AllowedUserIDs, Settings: inbound.Settings}
}

// canvasSnapshots collects every node the publication depends on, targets
// first. RelaySnapshots already walks a node's whole relay component in that
// order; a node without relay relationships is published on its own.
func (s *Service) canvasSnapshots(ctx context.Context, nodeIDs []string, users AccessReader) ([]NodeSnapshot, error) {
	seen := map[string]bool{}
	result := []NodeSnapshot{}
	for _, nodeID := range nodeIDs {
		if seen[nodeID] {
			continue
		}
		snapshots, err := s.RelaySnapshots(ctx, nodeID, users)
		if errors.Is(err, ErrNoRelayRelationships) {
			state, enabled, snapshotErr := s.SnapshotIntent(ctx, nodeID, users)
			if snapshotErr != nil {
				return nil, snapshotErr
			}
			snapshots, err = []NodeSnapshot{{State: state, InboundIntent: enabled}}, nil
		}
		if err != nil {
			return nil, err
		}
		for _, snapshot := range snapshots {
			if seen[snapshot.State.NodeID] {
				continue
			}
			seen[snapshot.State.NodeID] = true
			result = append(result, snapshot)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("方案没有可发布的服务器")
	}
	return result, nil
}

// canvasStale is what the previous publication of a scheme generated and the
// drawing no longer describes.
type canvasStale struct {
	chains    []Chain
	outbounds []Outbound
	inbounds  []Inbound
	chainID   map[string]bool
	outbound  map[string]bool
	inbound   map[string]bool
}

// canvasApplyPlan makes the stored objects match the drawing without ever
// leaving half of it behind:
//
//  1. everything that can be checked up front is checked first (listeners,
//     outside references to the objects that will be removed);
//  2. the new objects are created or updated and the entry inbounds are moved
//     onto them; when any step fails, everything this run changed is undone
//     and the previous objects are untouched, because nothing was removed yet;
//  3. only then are the scheme's stale objects removed. They are no longer
//     referenced by the drawing, so a removal that still fails leaves a warning
//     instead of failing a publication whose new state is already complete.
//
// The warnings it returns are shown on the canvas next to the node results.
func (s *Service) canvasApplyPlan(ctx context.Context, plan *canvasPlan) ([]string, error) {
	stale, err := s.canvasStaleObjects(ctx, plan)
	if err != nil {
		return nil, err
	}
	if err := s.canvasCheckListeners(ctx, plan, stale); err != nil {
		return nil, err
	}
	// A node that only loses objects is published too, otherwise the removal
	// would stay a draft and the node would keep running the old relay.
	for _, chain := range stale.chains {
		for _, nodeID := range chain.NodeIDs {
			plan.node(nodeID)
		}
	}
	for _, outbound := range stale.outbounds {
		plan.node(outbound.NodeID)
	}
	for _, inbound := range stale.inbounds {
		plan.node(inbound.NodeID)
	}
	if err := s.canvasCheckReferences(ctx, plan, stale); err != nil {
		return nil, err
	}
	undo := &canvasUndo{}
	warnings, err := s.canvasEnsure(ctx, plan, stale, undo)
	if err != nil {
		if undoErr := undo.run(ctx); undoErr != nil {
			return nil, fmt.Errorf("%w（撤销本次修改时出错：%v，请检查方案生成的对象）", err, undoErr)
		}
		return nil, err
	}
	return append(warnings, s.canvasRemoveStale(ctx, stale)...), nil
}

// canvasStaleObjects lists the scheme's generated objects the drawing no longer
// names.
func (s *Service) canvasStaleObjects(ctx context.Context, plan *canvasPlan) (*canvasStale, error) {
	stale := &canvasStale{chainID: map[string]bool{}, outbound: map[string]bool{}, inbound: map[string]bool{}}
	chains, err := s.Repository.canvasChainsByPrefix(ctx, plan.prefix)
	if err != nil {
		return nil, err
	}
	for _, chain := range chains {
		if !plan.chainName[chain.Name] {
			stale.chains = append(stale.chains, chain)
			stale.chainID[chain.ID] = true
		}
	}
	outbounds, err := s.Repository.canvasOutboundsByPrefix(ctx, plan.prefix)
	if err != nil {
		return nil, err
	}
	for _, outbound := range outbounds {
		if !plan.outbounds[outbound.Name] {
			stale.outbounds = append(stale.outbounds, outbound)
			stale.outbound[outbound.ID] = true
		}
	}
	inbounds, err := s.Repository.canvasInboundsByPrefix(ctx, plan.prefix)
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if !plan.inbounds[inbound.Name] {
			stale.inbounds = append(stale.inbounds, inbound)
			stale.inbound[inbound.ID] = true
		}
	}
	return stale, nil
}

// canvasCheckListeners refuses a listener another inbound already holds, and two
// edges of the drawing that would share one. A stale inbound of this scheme on
// the same listener and protocol is taken over (renamed and kept) instead of
// being removed and recreated, so a rewired drawing keeps its port; with another
// protocol it cannot be taken over before it is removed, which this order of
// operations never does, so the publication is refused with a way out.
func (s *Service) canvasCheckListeners(ctx context.Context, plan *canvasPlan, stale *canvasStale) error {
	claimed := map[string]*canvasEdge{}
	for _, edge := range plan.edges {
		key := fmt.Sprintf("%s:%d", edge.targetNode, edge.port)
		if other, taken := claimed[key]; taken {
			return fmt.Errorf("连接 %q 与 %q 在同一台目标服务器上使用了同一端口 %d，请为其中一条换一个端口", other.connectionID, edge.connectionID, edge.port)
		}
		claimed[key] = edge
		occupants, err := s.Repository.canvasInboundsAtPort(ctx, edge.targetNode, edge.port)
		if err != nil {
			return err
		}
		for _, occupant := range occupants {
			switch {
			case occupant.Name == edge.inboundName:
			case stale.inbound[occupant.ID] && occupant.Protocol == edge.protocol:
				edge.adopt = occupant
				delete(stale.inbound, occupant.ID)
			case stale.inbound[occupant.ID]:
				return fmt.Errorf("目标服务器上端口 %d 仍由本方案旧的 %s 中转入口使用，协议不同无法直接替换：请先换一个端口发布，或先删掉这条连接发布一次", edge.port, occupant.Protocol)
			default:
				return fmt.Errorf("目标服务器上端口 %d 已被其他入站占用", edge.port)
			}
		}
	}
	return nil
}

// canvasCheckReferences refuses to start when an object outside the drawing
// still uses one of the objects the publication will remove: a routing policy,
// an outbound group, another chain or an inbound the drawing does not route.
// Removing them would fail at the end, or would take a route away from an
// object the Owner configured by hand.
func (s *Service) canvasCheckReferences(ctx context.Context, plan *canvasPlan, stale *canvasStale) error {
	entries := map[string]bool{}
	for _, entry := range plan.entries {
		entries[entry.existing.ID] = true
	}
	ignored := func(referrer canvasReferrer) bool {
		switch referrer.Kind {
		case "inbound":
			return entries[referrer.ID]
		case "chain":
			return stale.chainID[referrer.ID]
		case "outbound":
			return stale.outbound[referrer.ID]
		}
		return false
	}
	check := func(id, name string) error {
		if err := s.checkPolicyReference(ctx, id); err != nil {
			return fmt.Errorf("本方案旧的对象 %s 仍被分流规则引用，请先修改该规则", name)
		}
		referrers, err := s.Repository.canvasReferrers(ctx, id)
		if err != nil {
			return err
		}
		for _, referrer := range referrers {
			if !ignored(referrer) {
				return fmt.Errorf("本方案旧的对象 %s 仍被 %s %q 引用，请先解除引用", name, referrer.Kind, referrer.Name)
			}
		}
		return nil
	}
	for _, chain := range stale.chains {
		if err := check(chain.ID, chain.Name); err != nil {
			return err
		}
	}
	for _, outbound := range stale.outbounds {
		if err := check(outbound.ID, outbound.Name); err != nil {
			return err
		}
	}
	for _, inbound := range stale.inbounds {
		if err := check(inbound.ID, inbound.Name); err != nil {
			return err
		}
	}
	return nil
}

// canvasRemoveStale removes the scheme's stale objects, chains first because
// they reference outbounds, outbounds before the inbounds they target.
func (s *Service) canvasRemoveStale(ctx context.Context, stale *canvasStale) []string {
	warnings := []string{}
	for _, chain := range stale.chains {
		if err := s.DeleteChain(ctx, chain.ID); err != nil {
			warnings = append(warnings, fmt.Sprintf("旧链路 %s 未能删除：%v", chain.Name, err))
		}
	}
	for _, outbound := range stale.outbounds {
		if err := s.DeleteOutbound(ctx, outbound.ID); err != nil {
			warnings = append(warnings, fmt.Sprintf("旧出站 %s 未能删除：%v", outbound.Name, err))
		}
	}
	for _, inbound := range stale.inbounds {
		if !stale.inbound[inbound.ID] {
			continue // taken over by an edge of the drawing
		}
		if err := s.Repository.DeleteInbound(ctx, inbound.ID); err != nil {
			warnings = append(warnings, fmt.Sprintf("旧入站 %s 未能删除：%v", inbound.Name, err))
		}
	}
	return warnings
}

// canvasUndo records how to reverse each change of one publication. The steps
// run newest first and on a context that ignores cancellation, so a dropped
// request still restores the previous objects.
type canvasUndo struct {
	steps []func(context.Context) error
}

func (u *canvasUndo) add(step func(context.Context) error) { u.steps = append(u.steps, step) }

func (u *canvasUndo) run(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	failures := []string{}
	for index := len(u.steps) - 1; index >= 0; index-- {
		if err := u.steps[index](ctx); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "；"))
	}
	return nil
}
