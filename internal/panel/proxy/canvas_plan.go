package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CanvasPlanObject describes a write publication would attempt, without secrets
// or generated database IDs. Existing names are updates, as in canvasEnsure.
type CanvasPlanObject struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Protocol string `json:"protocol,omitempty"`
	Port     int    `json:"port,omitempty"`
	Target   string `json:"target,omitempty"`
}
type CanvasPlanServer struct {
	NodeID  string             `json:"nodeId"`
	Name    string             `json:"name"`
	Creates []CanvasPlanObject `json:"creates"`
	Updates []CanvasPlanObject `json:"updates"`
	Removes []CanvasPlanObject `json:"removes"`
}
type CanvasPlanConnection struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
type CanvasPlanConflict struct {
	Reason       string `json:"reason,omitempty"`
	ObjectID     string `json:"objectId,omitempty"`
	NodeID       string `json:"nodeId"`
	Port         int    `json:"port"`
	With         string `json:"with"`
	ConnectionID string `json:"connectionId"`
}
type CanvasPlanResult struct {
	Tunnels     []CanvasTunnelImpact   `json:"tunnels"`
	OK          bool                   `json:"ok"`
	Servers     []CanvasPlanServer     `json:"servers"`
	Connections []CanvasPlanConnection `json:"connections"`
	Conflicts   []CanvasPlanConflict   `json:"conflicts"`
	Warnings    []string               `json:"warnings"`
	Message     string                 `json:"message,omitempty"`
}

type canvasListener struct {
	node, name, protocol, connection string
	port                             int
	edge                             *canvasEdge
}

// Both publication and preview use this exact listener check, including stale
// relay adoption. Listener ports are reserved node-wide by number: the Agent's
// DesiredState validation rejects a second enabled inbound on the same port
// whatever its transport or core, so a TCP and a UDP listener (VLESS on TCP 443
// next to Hysteria2 on UDP 443) cannot share one either. The node editor applies
// the same numeric rule. Disabled inbounds reserve nothing, because the Agent
// only validates enabled listeners.
func (s *Service) canvasListenerConflicts(ctx context.Context, plan *canvasPlan, stale *canvasStale) ([]CanvasPlanConflict, error) {
	listeners := []canvasListener{}
	for _, e := range plan.edges {
		listeners = append(listeners, canvasListener{e.targetNode, e.inboundName, e.protocol, e.connectionID, e.port, e})
	}
	for i, f := range plan.forwards {
		listeners = append(listeners, canvasListener{f.NodeID, f.Name, f.Protocol, plan.forwardConnections[i], f.Port, nil})
	}
	owned, err := s.canvasOwnedInbounds(ctx, plan)
	if err != nil {
		return nil, err
	}
	claimed := map[string]canvasListener{}
	conflicts := []CanvasPlanConflict{}
	for _, l := range listeners {
		key := fmt.Sprintf("%s:%d", l.node, l.port)
		if other, found := claimed[key]; found {
			conflicts = append(conflicts, CanvasPlanConflict{NodeID: l.node, Port: l.port, With: other.name, ConnectionID: l.connection}, CanvasPlanConflict{NodeID: other.node, Port: other.port, With: l.name, ConnectionID: other.connection})
		} else {
			claimed[key] = l
		}
		occupants, err := s.Repository.canvasInboundsAtPort(ctx, l.node, l.port)
		if err != nil {
			return nil, err
		}
		for _, o := range occupants {
			// Exact names alone do not establish ownership: the same prefix/shape
			// recognition used for pruning must also recognise this inbound.
			isOwned := false
			for _, item := range owned {
				if item.ID == o.ID {
					isOwned = true
					break
				}
			}
			if isOwned && o.Name == l.name {
				owner, e := s.Repository.objectOwner(ctx, "inbound", o.ID)
				if e != nil {
					return nil, e
				}
				if owner != plan.owners[l.name] {
					conflicts = append(conflicts, CanvasPlanConflict{NodeID: l.node, Port: l.port, With: o.Name, ConnectionID: l.connection, Reason: "tunnel_object", ObjectID: o.ID})
					continue
				}
				continue
			}
			owner, e := s.Repository.objectOwner(ctx, "inbound", o.ID)
			if e != nil {
				return nil, e
			}
			if stale.inbound[o.ID] && o.Protocol == l.protocol && l.edge != nil && owner == l.edge.tunnelID {
				l.edge.adopt = o
				delete(stale.inbound, o.ID)
				continue
			}
			// The Agent rejects a duplicate port only among enabled inbounds, so a
			// disabled occupant reserves nothing for the node-wide number.
			if !o.Enabled {
				continue
			}
			reason := "listener"
			if owner == "" && legacyCanvasName("inbound", o.Name) {
				reason = "legacy_object"
			}
			conflicts = append(conflicts, CanvasPlanConflict{NodeID: l.node, Port: l.port, With: o.Name, ConnectionID: l.connection, Reason: reason, ObjectID: o.ID})
		}
	}
	named, err := s.canvasNamedObjectConflicts(ctx, plan)
	if err != nil {
		return nil, err
	}
	conflicts = append(conflicts, named...)
	return conflicts, nil
}

// PlanCanvasScheme only reads storage. Supplying layout substitutes a draft in
// memory, never saves it. It shares build, ownership/diff, listener and stale
// reference checks with publish; it does not run Save*, Snapshot or Deploy.
func (s *Service) PlanCanvasScheme(ctx context.Context, schemeID string, layout *CanvasLayout) (CanvasPlanResult, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	scheme, err := s.Repository.CanvasScheme(ctx, schemeID)
	if err != nil {
		return CanvasPlanResult{}, err
	}
	if layout != nil {
		scheme.Layout = *layout
	}
	result := CanvasPlanResult{Tunnels: []CanvasTunnelImpact{}, OK: true, Servers: []CanvasPlanServer{}, Connections: []CanvasPlanConnection{}, Conflicts: []CanvasPlanConflict{}, Warnings: []string{}}
	for _, c := range scheme.Layout.Connections {
		result.Connections = append(result.Connections, CanvasPlanConnection{c.ID, "ok", "可发布"})
	}
	failAll := func(err error) {
		result.OK = false
		result.Message = err.Error()
		for i := range result.Connections {
			result.Connections[i].Status = "error"
			result.Connections[i].Message = err.Error()
		}
	}
	if scheme.Execution != canvasExecutionServer {
		failAll(errors.New("客户端方案暂不支持发布计划；客户端链仅在 mihomo 订阅中生效"))
		return result, nil
	}
	impactLayout := scheme.Layout
	if err := s.Repository.attachInlineTunnelIDs(ctx, scheme.ID, &impactLayout); err != nil {
		return result, err
	}
	seenTunnels := map[string]bool{}
	for _, c := range impactLayout.Connections {
		if c.TunnelID == "" || seenTunnels[c.TunnelID] {
			continue
		}
		seenTunnels[c.TunnelID] = true
		refs, e := s.Repository.tunnelSchemes(ctx, c.TunnelID)
		if e != nil {
			return result, e
		}
		result.Tunnels = append(result.Tunnels, CanvasTunnelImpact{c.TunnelID, refs})
	}
	plan, err := s.canvasBuildPlan(ctx, scheme)
	if err != nil {
		result.OK = false
		result.Message = err.Error()
		// Diagnose every row independently so one invalid tunnel does not hide
		// errors in later rows. Graph-wide errors are assigned to relay rows when
		// every individual connection is valid (cycle, branch, excessive hops).
		found := false
		for i, c := range scheme.Layout.Connections {
			one := scheme
			one.Layout.Connections = []CanvasConnection{c}
			// Entry bindings describe the whole graph, not an individual arrow.
			one.Layout.Instances = nil
			for _, instance := range scheme.Layout.Instances {
				if instance.InstanceID == c.Source || instance.InstanceID == c.Target {
					one.Layout.Instances = append(one.Layout.Instances, instance)
				}
			}
			if _, rowErr := s.canvasBuildPlan(ctx, one); rowErr != nil {
				result.Connections[i].Status = "error"
				result.Connections[i].Message = rowErr.Error()
				found = true
			}
		}
		if !found {
			if validationErr := scheme.Layout.validate(); validationErr != nil {
				failAll(validationErr)
				return result, nil
			}
			for i, c := range scheme.Layout.Connections {
				if c.Type != "port_forward" {
					result.Connections[i].Status = "error"
					result.Connections[i].Message = err.Error()
				}
			}
		}
		return result, nil
	}
	for tunnelID := range plan.tunnelIDs {
		if !seenTunnels[tunnelID] {
			refs, e := s.Repository.tunnelSchemes(ctx, tunnelID)
			if e != nil {
				return result, e
			}
			result.Tunnels = append(result.Tunnels, CanvasTunnelImpact{tunnelID, refs})
			seenTunnels[tunnelID] = true
		}
	}
	stale, err := s.canvasStaleObjects(ctx, plan)
	if err != nil {
		return result, err
	}
	// A last-reference removal affects the publishing drawing even though the
	// tunnel no longer appears in its saved layout.
	removed := []struct{ kind, id string }{}
	for _, o := range stale.inbounds {
		removed = append(removed, struct{ kind, id string }{"inbound", o.ID})
	}
	for _, o := range stale.outbounds {
		removed = append(removed, struct{ kind, id string }{"outbound", o.ID})
	}
	for _, o := range stale.chains {
		removed = append(removed, struct{ kind, id string }{"chain", o.ID})
	}
	for _, o := range removed {
		tunnelID, e := s.Repository.objectOwner(ctx, o.kind, o.id)
		if e != nil {
			return result, e
		}
		if tunnelID == "" || seenTunnels[tunnelID] {
			continue
		}
		refs, e := s.Repository.tunnelSchemes(ctx, tunnelID)
		if e != nil {
			return result, e
		}
		found := false
		for _, ref := range refs {
			if ref.ID == scheme.ID {
				found = true
			}
		}
		if !found {
			refs = append(refs, CanvasSchemeRef{ID: scheme.ID, Name: scheme.Name})
		}
		result.Tunnels = append(result.Tunnels, CanvasTunnelImpact{tunnelID, refs})
		seenTunnels[tunnelID] = true
	}
	result.Conflicts, err = s.canvasListenerConflicts(ctx, plan, stale)
	if err != nil {
		return result, err
	}
	for _, conflict := range result.Conflicts {
		result.OK = false
		for i := range result.Connections {
			if result.Connections[i].ID == conflict.ConnectionID {
				result.Connections[i].Status = "error"
				result.Connections[i].Message = fmt.Sprintf("端口 %d 已被 %s 占用", conflict.Port, conflict.With)
			}
		}
	}
	if err := s.canvasCheckReferences(ctx, plan, stale); err != nil {
		failAll(err)
	}
	servers := map[string]int{}
	add := func(nodeID string, object CanvasPlanObject, action string) {
		index, ok := servers[nodeID]
		if !ok {
			index = len(result.Servers)
			servers[nodeID] = index
			name := nodeID
			if n, e := s.Nodes.Get(ctx, nodeID); e == nil {
				name = n.Name
			}
			result.Servers = append(result.Servers, CanvasPlanServer{NodeID: nodeID, Name: name, Creates: []CanvasPlanObject{}, Updates: []CanvasPlanObject{}, Removes: []CanvasPlanObject{}})
		}
		server := &result.Servers[index]
		switch action {
		case "create":
			server.Creates = append(server.Creates, object)
		case "update":
			server.Updates = append(server.Updates, object)
		case "remove":
			server.Removes = append(server.Removes, object)
		}
	}
	inbound := func(nodeID, name, protocol string, port int, target string, adopt bool) error {
		_, e := s.Repository.canvasInboundNamed(ctx, nodeID, name)
		action := "update"
		if errors.Is(e, ErrNotFound) {
			action = "create"
			if adopt {
				action = "update"
			}
		} else if e != nil {
			return e
		}
		kind := "inbound"
		if protocol == "forward" {
			kind = "forward"
		}
		add(nodeID, CanvasPlanObject{kind, name, protocol, port, target}, action)
		return nil
	}
	for _, f := range plan.forwards {
		if err := inbound(f.NodeID, f.Name, f.Protocol, f.Port, fmt.Sprintf("%s:%d", f.Settings.Forward.Host, f.Settings.Forward.Port), false); err != nil {
			return result, err
		}
		result.Warnings = append(result.Warnings, "端口转发不验证用户身份；用户额度、设备限制不适用，也不生成用户域名访问记录")
	}
	for _, e := range plan.edges {
		if err := inbound(e.targetNode, e.inboundName, e.protocol, e.port, "", e.adopt.ID != ""); err != nil {
			return result, err
		}
		existing, err := s.Repository.canvasOutboundNamed(ctx, e.sourceNode, e.outboundName)
		action := "update"
		if errors.Is(err, ErrNotFound) {
			action = "create"
		} else if err != nil {
			return result, err
		}
		if existing.ID != "" {
			target, targetErr := s.Repository.canvasInboundNamed(ctx, e.targetNode, e.inboundName)
			if errors.Is(targetErr, ErrNotFound) && e.adopt.ID != "" {
				target = e.adopt
				targetErr = nil
			}
			if targetErr != nil && !errors.Is(targetErr, ErrNotFound) {
				return result, targetErr
			}
			if existing.Protocol != e.protocol || existing.TargetInboundID != target.ID {
				failAll(fmt.Errorf("生成的出站 %s 已被手动修改，请删除它后重新发布", existing.Name))
			}
		}
		target := e.targetNode
		if n, err := s.Nodes.Get(ctx, target); err == nil {
			target = n.Name
		}
		add(e.sourceNode, CanvasPlanObject{"outbound", e.outboundName, e.protocol, e.port, target}, action)
	}
	for _, c := range plan.chains {
		_, err := s.Repository.canvasChainNamed(ctx, c.name)
		action := "update"
		if errors.Is(err, ErrNotFound) {
			action = "create"
		} else if err != nil {
			return result, err
		}
		add(c.nodes[0], CanvasPlanObject{Kind: "chain", Name: c.name}, action)
	}
	for _, e := range plan.entries {
		target := e.existing.DefaultOutbound
		switch {
		case e.chain != nil:
			target = e.chain.name
			if existing, err := s.Repository.canvasChainNamed(ctx, e.chain.name); err == nil {
				target = "chain:" + existing.ID
			} else if !errors.Is(err, ErrNotFound) {
				return result, err
			}
		case e.edge != nil:
			target = e.edge.outboundName
			if existing, err := s.Repository.canvasOutboundNamed(ctx, e.edge.sourceNode, e.edge.outboundName); err == nil {
				target = existing.ID
			} else if !errors.Is(err, ErrNotFound) {
				return result, err
			}
		case stale.outbound[target] || stale.chainID[strings.TrimPrefix(target, "chain:")]:
			target = "direct"
			result.Warnings = append(result.Warnings, fmt.Sprintf("入站 %s 在方案中已没有出站连接，默认出站改为 direct", e.existing.Name))
		}
		if target != e.existing.DefaultOutbound {
			add(e.existing.NodeID, CanvasPlanObject{"inbound", e.existing.Name, e.existing.Protocol, e.existing.Port, target}, "update")
		}
	}
	for _, c := range stale.chains {
		for _, nodeID := range c.NodeIDs {
			add(nodeID, CanvasPlanObject{Kind: "chain", Name: c.Name}, "remove")
		}
	}
	for _, o := range stale.outbounds {
		add(o.NodeID, CanvasPlanObject{Kind: "outbound", Name: o.Name, Protocol: o.Protocol}, "remove")
	}
	for _, i := range stale.inbounds {
		if stale.inbound[i.ID] {
			kind := "inbound"
			if i.Protocol == "forward" {
				kind = "forward"
			}
			add(i.NodeID, CanvasPlanObject{Kind: kind, Name: i.Name, Protocol: i.Protocol, Port: i.Port}, "remove")
		}
	}
	// A referenced entry can still be published when its route is unchanged.
	// Include nodes with no object writes, matching canvasSnapshots.
	for _, nodeID := range plan.nodes {
		if _, ok := servers[nodeID]; !ok {
			name := nodeID
			if n, err := s.Nodes.Get(ctx, nodeID); err == nil {
				name = n.Name
			}
			servers[nodeID] = len(result.Servers)
			result.Servers = append(result.Servers, CanvasPlanServer{NodeID: nodeID, Name: name, Creates: []CanvasPlanObject{}, Updates: []CanvasPlanObject{}, Removes: []CanvasPlanObject{}})
		}
	}
	if len(result.Servers) == 0 {
		failAll(errors.New("方案没有可发布的服务器"))
	}
	return result, nil
}
