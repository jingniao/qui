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
	NodeID       string `json:"nodeId"`
	Port         int    `json:"port"`
	With         string `json:"with"`
	ConnectionID string `json:"connectionId"`
}
type CanvasPlanResult struct {
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
	networks                         []string
}

// listenNetworks is the transport an inbound listens on. A TCP and a UDP
// listener may share a port (VLESS on TCP 443 next to Hysteria2 on UDP 443),
// so only overlapping transports conflict. The node editor uses the same rule.
func listenNetworks(protocol, forwardNetwork string) []string {
	switch protocol {
	case "hysteria2", "tuic", "wireguard":
		return []string{"udp"}
	case "forward":
		if forwardNetwork == "" {
			return []string{"tcp"}
		}
		return strings.Split(forwardNetwork, ",")
	case "shadowsocks", "socks5":
		return []string{"tcp", "udp"}
	}
	return []string{"tcp"}
}

func inboundNetworks(i Inbound) []string {
	network := ""
	if i.Settings.Forward != nil {
		network = i.Settings.Forward.Network
	}
	return listenNetworks(i.Protocol, network)
}

func networksOverlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// Both publication and preview use this exact listener check, including stale
// relay adoption. A listener is reserved even when its existing inbound is off.
func (s *Service) canvasListenerConflicts(ctx context.Context, plan *canvasPlan, stale *canvasStale) ([]CanvasPlanConflict, error) {
	listeners := []canvasListener{}
	for _, e := range plan.edges {
		listeners = append(listeners, canvasListener{e.targetNode, e.inboundName, e.protocol, e.connectionID, e.port, e, listenNetworks(e.protocol, "")})
	}
	for i, f := range plan.forwards {
		listeners = append(listeners, canvasListener{f.NodeID, f.Name, f.Protocol, plan.forwardConnections[i], f.Port, nil, listenNetworks(f.Protocol, f.Settings.Forward.Network)})
	}
	owned, err := s.Repository.canvasInboundsByPrefix(ctx, plan.prefix)
	if err != nil {
		return nil, err
	}
	claimed := map[string]canvasListener{}
	conflicts := []CanvasPlanConflict{}
	for _, l := range listeners {
		for _, network := range l.networks {
			key := fmt.Sprintf("%s:%d/%s", l.node, l.port, network)
			if other, found := claimed[key]; found {
				conflicts = append(conflicts, CanvasPlanConflict{l.node, l.port, other.name, l.connection}, CanvasPlanConflict{other.node, other.port, l.name, other.connection})
				break
			}
		}
		for _, network := range l.networks {
			key := fmt.Sprintf("%s:%d/%s", l.node, l.port, network)
			if _, found := claimed[key]; !found {
				claimed[key] = l
			}
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
				continue
			}
			if !networksOverlap(l.networks, inboundNetworks(o)) {
				continue
			}
			if stale.inbound[o.ID] && o.Protocol == l.protocol && l.edge != nil {
				l.edge.adopt = o
				delete(stale.inbound, o.ID)
				continue
			}
			conflicts = append(conflicts, CanvasPlanConflict{l.node, l.port, o.Name, l.connection})
		}
	}
	return conflicts, nil
}

// PlanCanvasScheme only reads storage. Supplying layout substitutes a draft in
// memory, never saves it. It shares build, ownership/diff, listener and stale
// reference checks with publish; it does not run Save*, Snapshot or Deploy.
func (s *Service) PlanCanvasScheme(ctx context.Context, schemeID string, layout *CanvasLayout) (CanvasPlanResult, error) {
	scheme, err := s.Repository.CanvasScheme(ctx, schemeID)
	if err != nil {
		return CanvasPlanResult{}, err
	}
	if layout != nil {
		scheme.Layout = *layout
	}
	result := CanvasPlanResult{OK: true, Servers: []CanvasPlanServer{}, Connections: []CanvasPlanConnection{}, Conflicts: []CanvasPlanConflict{}, Warnings: []string{}}
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
	stale, err := s.canvasStaleObjects(ctx, plan)
	if err != nil {
		return result, err
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
