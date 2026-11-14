package proxy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"proxycontrol/pkg/contracts/desiredstate"
)

type CanvasRemoveInput struct {
	UpdatedAt          string            `json:"updatedAt"`
	RestoreTargets     map[string]string `json:"restoreTargets,omitempty"`
	AcknowledgeChanges bool              `json:"acknowledgeChanges,omitempty"`
}

type CanvasRemoveEntry struct {
	BindingID          string                 `json:"bindingId"`
	InstanceID         string                 `json:"instanceId"`
	InboundID          string                 `json:"inboundId"`
	NodeID             string                 `json:"nodeId"`
	Name               string                 `json:"name"`
	Protocol           string                 `json:"protocol"`
	Core               string                 `json:"core"`
	Listen             string                 `json:"listen"`
	Port               int                    `json:"port"`
	Mode               string                 `json:"mode"`
	Action             string                 `json:"action"`
	CurrentRoute       string                 `json:"currentRoute"`
	RestoreTarget      string                 `json:"restoreTarget,omitempty"`
	NeedsRestoreTarget bool                   `json:"needsRestoreTarget"`
	RestoreReason      string                 `json:"restoreReason,omitempty"`
	Groups             []CanvasPlanEntryGroup `json:"groups"`
	Changes            []string               `json:"changes"`
	Referrers          []CanvasRemoveReferrer `json:"referrers"`
	binding            CanvasEntryBindingRecord
	inbound            Inbound
}
type CanvasRemoveReferrer struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}
type CanvasRemoveObject struct {
	CanvasTunnelObject
	TunnelID  string                 `json:"tunnelId"`
	Keep      bool                   `json:"keep"`
	Referrers []CanvasRemoveReferrer `json:"referrers"`
}
type CanvasRemovePlan struct {
	OK                  bool                 `json:"ok"`
	SchemeID            string               `json:"schemeId"`
	UpdatedAt           string               `json:"updatedAt"`
	Entries             []CanvasRemoveEntry  `json:"entries"`
	Objects             []CanvasRemoveObject `json:"objects"`
	Nodes               []string             `json:"nodes"`
	ChangeDetectionRule string               `json:"changeDetectionRule"`
	scheme              CanvasScheme
	unsharedTunnelIDs   []string
}

func (s *Service) PlanRemoveCanvasScheme(ctx context.Context, schemeID string, input CanvasRemoveInput) (CanvasRemovePlan, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	return s.planRemoveCanvasScheme(ctx, schemeID, input)
}

func (s *Service) planRemoveCanvasScheme(ctx context.Context, schemeID string, input CanvasRemoveInput) (CanvasRemovePlan, error) {
	if strings.TrimSpace(input.UpdatedAt) == "" {
		return CanvasRemovePlan{}, errors.New("缺少 updatedAt 字段")
	}
	scheme, err := s.Repository.CanvasScheme(ctx, schemeID)
	if err != nil {
		return CanvasRemovePlan{}, err
	}
	if input.UpdatedAt != scheme.UpdatedAt {
		return CanvasRemovePlan{}, ErrStaleScheme
	}
	if scheme.Execution == canvasExecutionClient {
		return CanvasRemovePlan{}, errors.New("客户端方案没有服务器运行资源，请仅删除画布")
	}
	result := CanvasRemovePlan{OK: true, SchemeID: schemeID, UpdatedAt: scheme.UpdatedAt, Entries: []CanvasRemoveEntry{}, Objects: []CanvasRemoveObject{}, Nodes: []string{}, scheme: scheme, ChangeDetectionRule: "新建入口按创建时配置摘要与分组集合检测改动；有完整基线且未改动时无需确认，人工改动或缺失基线须显式确认；默认出站单独核对，确认不能绕过外部引用"}
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT "+canvasEntryBindingColumns+" FROM canvas_entry_bindings WHERE scheme_id=? ORDER BY id", schemeID)
	if err != nil {
		return result, err
	}
	bindings := []CanvasEntryBindingRecord{}
	for rows.Next() {
		b, e := scanCanvasEntryBinding(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		bindings = append(bindings, b)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	bound := map[string]bool{}
	for _, b := range bindings {
		bound[b.InboundID] = true
	}
	// Legacy entries have no binding ID. Their stable request key is explicitly
	// surfaced in the preview; no original route is inferred from live state.
	for _, instance := range scheme.Layout.Instances {
		ref, _ := parseCanvasRef(instance.Ref)
		if instance.Role == "entry" && ref.kind == "inbound" && !bound[ref.id] {
			bindings = append(bindings, CanvasEntryBindingRecord{ID: "legacy:" + instance.InstanceID, InstanceID: instance.InstanceID, InboundID: ref.id, Mode: "reuse"})
			bound[ref.id] = true
		}
	}
	recognized := map[string]bool{}
	nodeSeen := map[string]bool{}
	addNode := func(id string) {
		if id != "" && !nodeSeen[id] {
			result.Nodes = append(result.Nodes, id)
			nodeSeen[id] = true
		}
	}
	for _, b := range bindings {
		recognized[b.ID] = true
		inbound, e := s.Repository.Inbound(ctx, b.InboundID, true)
		if e != nil {
			return result, e
		}
		groups, e := s.canvasEntryGroups(ctx, inbound.ID, false)
		if e != nil {
			return result, e
		}
		entry := CanvasRemoveEntry{BindingID: b.ID, InstanceID: b.InstanceID, InboundID: b.InboundID, NodeID: inbound.NodeID, Name: inbound.Name, Mode: b.Mode, CurrentRoute: inbound.DefaultOutbound, Groups: groups, Changes: []string{}, Referrers: []CanvasRemoveReferrer{}, binding: b, inbound: inbound}
		entry.Protocol, entry.Core, entry.Listen, entry.Port = inbound.Protocol, inbound.Core, inbound.Listen, inbound.Port
		if b.Mode == "create" {
			entry.Action = "delete"
			refs, err := s.Repository.canvasReferrers(ctx, inbound.ID)
			if err != nil {
				return result, err
			}
			for _, ref := range refs {
				entry.Referrers = append(entry.Referrers, CanvasRemoveReferrer{Kind: ref.Kind, ID: ref.ID, Name: ref.Name})
			}
			extra, err := s.canvasRemoveExtraReferrers(ctx, schemeID, "inbound", inbound.ID)
			if err != nil {
				return result, err
			}
			entry.Referrers = append(entry.Referrers, extra...)
			if len(entry.Referrers) > 0 {
				result.OK = false
			}
			entry.Changes, err = canvasEntryBaselineChanges(b, inbound, groups)
			if err != nil {
				return result, err
			}
			if inbound.DefaultOutbound != b.AppliedDefaultOutbound {
				entry.Changes = append(entry.Changes, "默认出站已修改："+b.AppliedDefaultOutbound+" → "+inbound.DefaultOutbound)
			}
			if len(entry.Changes) > 0 && !input.AcknowledgeChanges {
				result.OK = false
			}
		} else {
			entry.Action = "restore"
			target, explicit := input.RestoreTargets[b.ID]
			if !explicit {
				target = b.OriginalDefaultOutbound
			}
			entry.RestoreTarget = target
			if !explicit && b.AppliedDefaultOutbound != "" && inbound.DefaultOutbound != b.AppliedDefaultOutbound {
				entry.RestoreReason = "复用入口默认出站已被修改，须显式指定恢复目标"
			}
			if entry.RestoreReason != "" {
				entry.NeedsRestoreTarget = true
				result.OK = false
			}
		}
		result.Entries = append(result.Entries, entry)
		addNode(inbound.NodeID)
	}
	// Validate against the complete final route set, not each entry's old route.
	// Two individually valid changes can form a cycle when applied together.
	restored := map[string]string{}
	removed := map[string]bool{}
	for _, entry := range result.Entries {
		if entry.Mode == "reuse" {
			restored[entry.InboundID] = entry.RestoreTarget
		} else {
			removed[entry.InboundID] = true
		}
	}
	for index := range result.Entries {
		entry := &result.Entries[index]
		if entry.Mode != "reuse" || entry.NeedsRestoreTarget {
			continue
		}
		if err := s.canvasValidateRemoveTarget(ctx, entry.inbound, entry.RestoreTarget, restored, removed); err != nil {
			entry.RestoreReason, entry.NeedsRestoreTarget, result.OK = err.Error(), true, false
		}
	}
	for key := range input.RestoreTargets {
		if !recognized[key] {
			return result, fmt.Errorf("未知入口绑定恢复目标：%s", key)
		}
	}
	tunnels := map[string]bool{}
	for _, c := range scheme.Layout.Connections {
		if c.TunnelID != "" {
			tunnels[c.TunnelID] = true
		}
	}
	rows, err = s.Repository.Database.QueryContext(ctx, "SELECT tunnel_id FROM canvas_tunnel_publications WHERE scheme_id=?", schemeID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		tunnels[id] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	tunnelIDs := make([]string, 0, len(tunnels))
	for id := range tunnels {
		tunnelIDs = append(tunnelIDs, id)
	}
	sort.Strings(tunnelIDs)
	for _, tunnelID := range tunnelIDs {
		refs, e := s.Repository.tunnelSchemes(ctx, tunnelID)
		if e != nil {
			return result, e
		}
		// Published associations protect channels even after another draft drops them.
		rows, e := s.Repository.Database.QueryContext(ctx, "SELECT s.id,s.name FROM canvas_tunnel_publications p JOIN canvas_schemes s ON s.id=p.scheme_id WHERE p.tunnel_id=?", tunnelID)
		if e != nil {
			return result, e
		}
		for rows.Next() {
			var ref CanvasSchemeRef
			if e = rows.Scan(&ref.ID, &ref.Name); e != nil {
				rows.Close()
				return result, e
			}
			refs = append(refs, ref)
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return result, e
		}
		if e = rows.Close(); e != nil {
			return result, e
		}
		shared := false
		for _, ref := range refs {
			if ref.ID != schemeID {
				shared = true
			}
		}
		if !shared {
			result.unsharedTunnelIDs = append(result.unsharedTunnelIDs, tunnelID)
		}
		objects, e := s.Repository.tunnelObjects(ctx, tunnelID)
		if e != nil {
			return result, e
		}
		for _, object := range objects {
			item := CanvasRemoveObject{CanvasTunnelObject: object, TunnelID: tunnelID, Referrers: []CanvasRemoveReferrer{}}
			seen := map[string]bool{}
			for _, ref := range refs {
				if ref.ID != schemeID && !seen[ref.ID] {
					item.Keep = true
					item.Referrers = append(item.Referrers, CanvasRemoveReferrer{Kind: "scheme", ID: ref.ID, Name: ref.Name})
					seen[ref.ID] = true
				}
			}
			result.Objects = append(result.Objects, item)
		}
	}
	candidates := map[string]int{}
	for i, o := range result.Objects {
		candidates[o.Kind+":"+o.ID] = i
	}
	// Retention propagates from external users through the generated dependency graph.
	changed := true
	for changed {
		changed = false
		for index := range result.Objects {
			o := &result.Objects[index]
			if o.Keep {
				continue
			}
			refs, e := s.Repository.canvasReferrers(ctx, o.ID)
			if e != nil {
				return result, e
			}
			extra, e := s.canvasRemoveExtraReferrers(ctx, schemeID, o.Kind, o.ID)
			if e != nil {
				return result, e
			}
			o.Referrers = append(o.Referrers, extra...)
			for _, ref := range refs {
				if ref.Kind == "inbound" && bound[ref.ID] {
					keepsRoute := false
					for _, entry := range result.Entries {
						if entry.InboundID == ref.ID && entry.Mode == "reuse" && (entry.RestoreTarget == o.ID || entry.RestoreTarget == "chain:"+o.ID) {
							keepsRoute = true
						}
					}
					if !keepsRoute {
						continue
					}
				} else if ci, ok := candidates[ref.Kind+":"+ref.ID]; ok && !result.Objects[ci].Keep {
					continue
				}
				o.Referrers = append(o.Referrers, CanvasRemoveReferrer{Kind: ref.Kind, ID: ref.ID, Name: ref.Name})
			}
			if reader, ok := s.Policies.(PolicyReferences); ok {
				used, e := reader.UsesProxyTarget(ctx, o.ID)
				if e != nil {
					return result, e
				}
				if used {
					o.Referrers = append(o.Referrers, CanvasRemoveReferrer{Kind: "policy", ID: o.ID, Name: "引用此对象的分流策略"})
				}
			}
			// An explicitly chosen restoration target remains even though its current
			// inbound points elsewhere until execution starts.
			for _, entry := range result.Entries {
				if entry.Mode == "reuse" && (entry.RestoreTarget == o.ID || entry.RestoreTarget == "chain:"+o.ID) {
					o.Referrers = append(o.Referrers, CanvasRemoveReferrer{Kind: "restore", ID: entry.InboundID, Name: entry.Name})
				}
			}
			if len(o.Referrers) > 0 {
				o.Keep = true
				changed = true
			}
		}
	}
	// Order consumers before providers. Entry nodes are already at the front;
	// saved tunnel endpoints determine relay/exit order independent of object IDs.
	before := map[string][]string{}
	allNodes := append([]string{}, result.Nodes...)
	for _, o := range result.Objects {
		if !o.Keep {
			addNode(o.NodeID)
		}
	}
	allNodes = append(allNodes, result.Nodes[len(allNodes):]...)
	for _, tunnelID := range tunnelIDs {
		var source, target string
		if err = s.Repository.Database.QueryRowContext(ctx, "SELECT source_ref,target_ref FROM canvas_tunnels WHERE id=?", tunnelID).Scan(&source, &target); err != nil {
			return result, err
		}
		sourceID, e := s.canvasRemoveRefNode(ctx, source)
		if e != nil {
			return result, e
		}
		targetID, e := s.canvasRemoveRefNode(ctx, target)
		if e != nil {
			return result, e
		}
		if sourceID != targetID && nodeSeen[sourceID] && nodeSeen[targetID] {
			before[targetID] = append(before[targetID], sourceID)
		}
	}
	ordered, err := canvasPublishOrder(allNodes, before)
	if err != nil {
		return result, err
	}
	result.Nodes = ordered
	return result, nil
}

// Draft object references and linked relay rules are outside canvasReferrers'
// proxy-object graph, but remain consumers when a tunnel is removed.
func (s *Service) canvasRemoveExtraReferrers(ctx context.Context, schemeID, kind, objectID string) ([]CanvasRemoveReferrer, error) {
	rows, err := s.Repository.Database.QueryContext(ctx, `SELECT DISTINCT 'scheme',s.id,s.name FROM canvas_schemes s,json_each(s.layout_json,'$.instances') i WHERE s.id<>? AND json_extract(i.value,'$.ref')=?
UNION ALL SELECT 'tunnel_rule',id,name FROM tunnel_rules WHERE ?='inbound' AND target_inbound_id=?`, schemeID, kind+":"+objectID, kind, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := []CanvasRemoveReferrer{}
	for rows.Next() {
		var ref CanvasRemoveReferrer
		if err = rows.Scan(&ref.Kind, &ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *Service) canvasRemoveRefNode(ctx context.Context, raw string) (string, error) {
	ref, err := parseCanvasRef(raw)
	if err != nil {
		return "", err
	}
	switch ref.kind {
	case "server":
		return ref.id, nil
	case "inbound":
		i, e := s.Repository.Inbound(ctx, ref.id, false)
		return i.NodeID, e
	case "outbound":
		o, e := s.Repository.Outbound(ctx, ref.id, false)
		return o.NodeID, e
	}
	return "", nil
}

// Validate the entire restoration route, including relay targets and backup
// branches. DFS is read-only and detects a return to the user entry itself.
func (s *Service) canvasValidateRemoveTarget(ctx context.Context, entry Inbound, target string, restored map[string]string, removed map[string]bool) error {
	if target == "" {
		return errors.New("原默认出站未登记，须显式指定恢复目标")
	}
	active := map[string]bool{"inbound:" + entry.ID: true}
	var route func(string, string) error
	var visit func(string, string, string) error
	backup := func(policy, id, nodeID string) error {
		switch desiredstate.BackupKind(policy) {
		case "Outbound":
			return visit("outbound", id, nodeID)
		case "OutboundGroup":
			return visit("group", id, nodeID)
		case "ProxyChain":
			return visit("chain", id, nodeID)
		}
		return nil
	}
	route = func(value, nodeID string) error {
		if value == "direct" || value == "block" {
			return nil
		}
		if strings.HasPrefix(value, "chain:") {
			return visit("chain", strings.TrimPrefix(value, "chain:"), nodeID)
		}
		if strings.HasPrefix(value, "group:") {
			return visit("group", strings.TrimPrefix(value, "group:"), nodeID)
		}
		return visit("outbound", value, nodeID)
	}
	visit = func(kind, id, nodeID string) error {
		key := kind + ":" + id
		if active[key] {
			return errors.New("恢复目标会形成路由环路")
		}
		active[key] = true
		defer delete(active, key)
		switch kind {
		case "inbound":
			if removed[id] {
				return errors.New("恢复路径引用了本次将删除的入口")
			}
			i, e := s.Repository.Inbound(ctx, id, false)
			if e != nil {
				return e
			}
			if !i.Enabled {
				return errors.New("恢复路径入站已禁用")
			}
			if target, ok := restored[id]; ok {
				return route(target, i.NodeID)
			}
			return route(i.DefaultOutbound, i.NodeID)
		case "outbound":
			o, e := s.Repository.Outbound(ctx, id, false)
			if e != nil {
				return e
			}
			if !o.Enabled || o.NodeID != nodeID {
				return errors.New("恢复出站已禁用或不属于入口服务器")
			}
			if o.TargetInboundID != "" {
				if e = visit("inbound", o.TargetInboundID, o.TargetNodeID); e != nil {
					return e
				}
			}
			return backup(o.FailurePolicy, o.BackupOutboundID, nodeID)
		case "chain":
			c, e := s.Repository.Chain(ctx, id)
			if e != nil {
				return e
			}
			if !c.Enabled || len(c.NodeIDs) == 0 || c.NodeIDs[0] != nodeID {
				return errors.New("恢复代理链已禁用或不适用入口服务器")
			}
			for _, hop := range c.Hops {
				if hop.OutboundID != "" {
					if e = visit("outbound", hop.OutboundID, hop.SourceNodeID); e != nil {
						return e
					}
				}
			}
			if c.FinalOutboundID != "" {
				if e = route(c.FinalOutboundID, c.NodeIDs[len(c.NodeIDs)-1]); e != nil {
					return e
				}
			}
			return backup(c.FailurePolicy, c.BackupTargetID, nodeID)
		case "group":
			g, e := s.Repository.Group(ctx, id)
			if e != nil {
				return e
			}
			if !g.Enabled || g.NodeID != nodeID {
				return errors.New("恢复出口组已禁用或不适用入口服务器")
			}
			for _, member := range g.Members {
				if e = visit("outbound", member.OutboundID, nodeID); e != nil {
					return e
				}
			}
			return backup(g.FailurePolicy, g.BackupOutboundID, nodeID)
		}
		return nil
	}
	return route(target, entry.NodeID)
}
