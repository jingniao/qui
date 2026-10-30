package proxy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

// Publishing a server scheme translates the drawing into the objects this
// module already owns (an internal relay inbound on every target, the outbound
// on every source, a proxy chain for a path of two edges and the entry inbound's
// default outbound) and then hands the resulting node snapshots to the
// Deployment module's own batches. There is no second deployment path: the
// canvas only decides which objects exist and in which order their nodes are
// published (exit, then relay, then entry).
//
// Every object a scheme generates carries the scheme's name as a name prefix
// that also names the binding it implements, for example
// "[Tokyo-relay] Node A → Node B (trojan:8443) 中转入口". Re-publishing looks
// the objects up by that name and updates them, and an object of this scheme
// whose name is no longer desired is removed, so a drawing can be published
// again and again without duplicating anything.

// canvasTunnels maps the tunnel protocol a connection names to the protocol the
// official cores actually carry between two managed nodes. "tls" is Trojan over
// TLS: one credential, TLS by default in SaveInbound. Gost, Brook and nftables
// have no supported implementation (D1, AGENTS section 23) and are refused here
// even though a drawing may name them.
var canvasTunnels = map[string]string{"tls": "trojan", "wireguard": "wireguard"}

// CanvasPublishNode is one node's result in a canvas publication.
type CanvasPublishNode struct {
	NodeID   string `json:"nodeId"`
	Name     string `json:"name"`
	Revision int64  `json:"revision"`
	Status   string `json:"status"`
}

// CanvasPublishResult is what the canvas shows after publishing: the batch (or
// the single revision) and each node's revision and status.
type CanvasPublishResult struct {
	BatchID  string              `json:"batchId,omitempty"`
	Nodes    []CanvasPublishNode `json:"nodes"`
	Warnings []string            `json:"warnings"`
}

// CanvasDeployer is implemented by the Deployment module. The proxy module
// cannot import it (Deployment imports proxy for snapshots), so the panel wires
// this interface in.
type CanvasDeployer interface {
	PublishCanvas(ctx context.Context, name, author string, snapshots []NodeSnapshot) (CanvasPublishResult, error)
}

// canvasEdge is one directed connection between two managed nodes together with
// the objects it needs.
type canvasEdge struct {
	connectionID string
	sourceNode   string
	targetNode   string
	protocol     string
	port         int
	tcp          bool
	udp          bool
	inboundName  string
	outboundName string
	inboundID    string
	outboundID   string
	// adopt is a stale inbound of the same scheme on this listener and protocol
	// that the edge takes over instead of creating a new one.
	adopt Inbound
}

// canvasChain is a path of two edges that becomes one proxy chain.
type canvasChain struct {
	name      string
	nodes     []string
	outbounds []*canvasEdge
	id        string
}

// canvasEntry is a self-hosted inbound the scheme routes: its default outbound
// becomes the outbound or chain the drawing names. Exactly one of edge and chain
// is set; with neither, the inbound keeps its current default outbound.
type canvasEntry struct {
	existing Inbound
	edge     *canvasEdge
	chain    *canvasChain
}

// canvasPlan is the complete translation of one drawing.
type canvasPlan struct {
	forwards           []InboundInput
	forwardConnections []string
	prefix             string
	edges              []*canvasEdge
	chains             []canvasChain
	entries            []canvasEntry
	inbounds           map[string]bool
	outbounds          map[string]bool
	chainName          map[string]bool
	nodes              []string
}

func (p *canvasPlan) node(nodeID string) {
	for _, existing := range p.nodes {
		if existing == nodeID {
			return
		}
	}
	p.nodes = append(p.nodes, nodeID)
}

// canvasObjectPrefix is the name prefix every object generated for a scheme
// carries. Scheme names are unique (SaveCanvasScheme refuses a duplicate), so
// one scheme can never prune another scheme's objects.
func canvasObjectPrefix(name string) string { return "[" + name + "] " }

// canvasEdgeNames derives the object names of one edge. The protocol and port
// are part of the name on purpose: changing either on the canvas then produces a
// new object and the old one is pruned, instead of colliding with an object
// whose node, protocol, port or relay target is immutable.
// The suffixes every generated name ends with. Together with the scheme prefix
// and the object's shape (an internal relay inbound, a relay outbound) they are
// how publishing recognises its own objects, so an object the Owner named by
// hand is never taken for a generated one.
const (
	canvasInboundSuffix  = " 中转入口"
	canvasOutboundSuffix = " 中转出站"
	canvasChainMarker    = " 链路 ("
)

func canvasEdgeNames(prefix, source, target, protocol string, port int) (string, string) {
	binding := fmt.Sprintf("%s → %s (%s:%d)", source, target, protocol, port)
	return prefix + binding + canvasInboundSuffix, prefix + binding + canvasOutboundSuffix
}

// canvasDefaultPort is the port an edge uses when the drawing names none.
func canvasDefaultPort(protocol string) int {
	if protocol == "wireguard" {
		return 51820
	}
	return 8443
}

func canvasInstanceNode(instance CanvasInstance) string {
	ref, err := parseCanvasRef(instance.Ref)
	if err != nil || ref.kind != "server" {
		return ""
	}
	return ref.id
}

// canvasBuildPlan turns the drawing into the objects and the publication order.
// This is the server execution path. Client schemes use a separate client-only
// snapshot; imported nodes, port forwarding and unsupported tunnel protocols
// cannot enter a managed-node publication here.
func (s *Service) canvasBuildPlan(ctx context.Context, scheme CanvasScheme) (*canvasPlan, error) {
	if scheme.Execution != canvasExecutionServer {
		return nil, errors.New("客户端方案暂不支持发布")
	}
	layout := scheme.Layout
	if err := layout.validate(); err != nil {
		return nil, err
	}
	if err := s.validateRefs(ctx, layout); err != nil {
		return nil, err
	}
	instances := map[string]CanvasInstance{}
	for _, instance := range layout.Instances {
		instances[instance.InstanceID] = instance
	}
	plan := &canvasPlan{prefix: canvasObjectPrefix(scheme.Name), inbounds: map[string]bool{}, outbounds: map[string]bool{}, chainName: map[string]bool{}}
	incoming, outgoing := map[string][]*canvasEdge{}, map[string][]*canvasEdge{}
	for _, connection := range layout.Connections {
		sourceNode := canvasInstanceNode(instances[connection.Source])
		targetNode := canvasInstanceNode(instances[connection.Target])
		if sourceNode == "" || targetNode == "" {
			return nil, fmt.Errorf("连接 %q 只能在两个受管服务器之间发布（客户端方案与导入节点在 U5 支持）", connection.ID)
		}
		if connection.Type == "port_forward" {
			source, target := instances[connection.Source], instances[connection.Target]
			listenPort, targetPort := connection.ListenPort, connection.TargetPort
			if listenPort == nil {
				listenPort = source.ListenPort
			}
			if targetPort == nil {
				targetPort = target.TargetPort
			}
			if listenPort == nil || targetPort == nil || (!connection.SupportTCP && !connection.SupportUDP) {
				return nil, fmt.Errorf("连接 %q：端口转发需要源监听端口、目标端口和 TCP/UDP", connection.ID)
			}
			n, err := s.Nodes.Get(ctx, targetNode)
			if err != nil {
				return nil, err
			}
			network := "tcp"
			if connection.SupportUDP {
				network = "udp"
				if connection.SupportTCP {
					network = "tcp,udp"
				}
			}
			digest := sha256.Sum256([]byte(connection.ID + ":" + sourceNode + ":" + targetNode))
			name := fmt.Sprintf("%sforward %x%s", plan.prefix, digest[:8], canvasInboundSuffix)
			host := connection.TargetHost
			if host == "" {
				host = n.PublicIP
			}
			input := InboundInput{Name: name, NodeID: sourceNode, Protocol: "forward", Listen: "0.0.0.0", Port: *listenPort, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: host, Port: *targetPort, Network: network}}}
			if err := ValidateInbound(input); err != nil {
				return nil, err
			}
			sourceInfo, err := s.Nodes.Get(ctx, sourceNode)
			if err != nil {
				return nil, err
			}
			if !sourceInfo.Enabled {
				return nil, fmt.Errorf("连接 %q：node missing or disabled", connection.ID)
			}
			if !sourceInfo.Capabilities.GetForwardInboundSupported() {
				return nil, fmt.Errorf("连接 %q：node Agent/core does not advertise forward inbound support; upgrade it first", connection.ID)
			}
			if err := ValidateCoreProtocol(sourceInfo.Capabilities.GetCoreName(), "forward", true); err != nil {
				return nil, fmt.Errorf("连接 %q：%w", connection.ID, err)
			}
			plan.forwards = append(plan.forwards, input)
			plan.forwardConnections = append(plan.forwardConnections, connection.ID)
			plan.inbounds[name] = true
			plan.node(sourceNode)
			continue
		}
		tunnel := connection.TunnelProtocol
		if tunnel == "" {
			tunnel = "tls"
		}
		protocol, ok := canvasTunnels[tunnel]
		if !ok {
			return nil, fmt.Errorf("连接 %q：隧道协议 %s 暂不支持", connection.ID, tunnel)
		}
		if !connection.SupportTCP && !connection.SupportUDP {
			return nil, fmt.Errorf("连接 %q 至少需要支持 TCP 或 UDP", connection.ID)
		}
		port := canvasDefaultPort(protocol)
		if connection.TunnelPort != nil {
			port = *connection.TunnelPort
		}
		edge := &canvasEdge{connectionID: connection.ID, sourceNode: sourceNode, targetNode: targetNode, protocol: protocol, port: port, tcp: connection.SupportTCP, udp: connection.SupportUDP}
		plan.edges = append(plan.edges, edge)
		incoming[targetNode] = append(incoming[targetNode], edge)
		outgoing[sourceNode] = append(outgoing[sourceNode], edge)
		plan.node(sourceNode)
		plan.node(targetNode)
	}
	if cycle := canvasCycle(plan.edges); cycle != "" {
		return nil, fmt.Errorf("方案中的中转连接形成了环路（经过连接 %q），请删除其中一条", cycle)
	}
	names, err := s.canvasNodeNames(ctx, plan.nodes)
	if err != nil {
		return nil, err
	}
	for _, edge := range plan.edges {
		edge.inboundName, edge.outboundName = canvasEdgeNames(plan.prefix, names[edge.sourceNode], names[edge.targetNode], edge.protocol, edge.port)
		plan.inbounds[edge.inboundName] = true
		plan.outbounds[edge.outboundName] = true
	}
	// A path of two edges becomes a proxy chain on its entry node. A node that
	// starts a path has no incoming edge; a path is at most three nodes and never
	// revisits a node, which is what the chain validator accepts.
	for _, start := range plan.sources(incoming) {
		for _, first := range outgoing[start] {
			path, ok := plan.linear(first, outgoing, map[string]bool{start: true})
			if !ok {
				return nil, errors.New("方案中的中转路径必须是线性链路（每个节点至多一个后继，最多三跳）")
			}
			if len(path) < 2 {
				continue
			}
			chain := canvasChain{outbounds: path}
			chain.nodes = append(chain.nodes, path[0].sourceNode)
			labels := make([]string, 0, len(path))
			for _, edge := range path {
				chain.nodes = append(chain.nodes, edge.targetNode)
				labels = append(labels, fmt.Sprintf("%s:%d", edge.protocol, edge.port))
			}
			chain.name = plan.prefix + strings.Join(nodeNames(names, chain.nodes), " → ") + canvasChainMarker + strings.Join(labels, ", ") + ")"
			plan.chains = append(plan.chains, chain)
			plan.chainName[chain.name] = true
		}
	}
	// Entry inbounds the drawing routes: a self-hosted inbound on the node a path
	// starts from.
	for _, instance := range layout.Instances {
		ref, err := parseCanvasRef(instance.Ref)
		if err != nil || ref.kind != "inbound" {
			continue
		}
		// Revealed on purpose: publishing rewrites the inbound through
		// SaveInbound, which needs its real settings to round-trip them.
		inbound, err := s.Repository.Inbound(ctx, ref.id, true)
		if err != nil {
			return nil, err
		}
		plan.node(inbound.NodeID)
		entry := canvasEntry{existing: inbound}
		if chain, ok := plan.chainFor(inbound.NodeID); ok {
			entry.chain = chain
		} else if edges := plan.edgesFrom(inbound.NodeID); len(edges) > 1 {
			// Without a chain, one route per entry inbound is what the renderer can
			// express; two would mean silently picking one of them.
			return nil, fmt.Errorf("入站 %q 所在的服务器有多个出站目标，请改用两跳链路或为每个出口单独建一个方案", inbound.Name)
		} else if len(edges) == 1 {
			entry.edge = edges[0]
		}
		plan.entries = append(plan.entries, entry)
	}
	return plan, nil
}

func nodeNames(names map[string]string, ids []string) []string {
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, names[id])
	}
	return result
}

// sources lists the nodes an edge leaves but never enters, in the order the
// drawing adds them, so a publication is reproducible.
func (p *canvasPlan) sources(incoming map[string][]*canvasEdge) []string {
	result := []string{}
	for _, nodeID := range p.nodes {
		if len(incoming[nodeID]) == 0 {
			result = append(result, nodeID)
		}
	}
	return result
}

// linear walks one outgoing edge chain. It stops when the path reaches a node
// without a successor; a node with two successors is a fan-out the proxy chains
// cannot express, and a fourth node exceeds the two-hop limit.
func (p *canvasPlan) linear(first *canvasEdge, outgoing map[string][]*canvasEdge, seen map[string]bool) ([]*canvasEdge, bool) {
	path := []*canvasEdge{first}
	node := first.targetNode
	seen[node] = true
	for {
		next := outgoing[node]
		if len(next) == 0 {
			return path, true
		}
		if len(next) > 1 || len(path) == 2 || seen[next[0].targetNode] {
			return path, false
		}
		path = append(path, next[0])
		node = next[0].targetNode
		seen[node] = true
	}
}

func (p *canvasPlan) chainFor(nodeID string) (*canvasChain, bool) {
	for index := range p.chains {
		if p.chains[index].nodes[0] == nodeID {
			return &p.chains[index], true
		}
	}
	return nil, false
}

func (p *canvasPlan) edgesFrom(nodeID string) []*canvasEdge {
	result := []*canvasEdge{}
	for _, edge := range p.edges {
		if edge.sourceNode == nodeID {
			result = append(result, edge)
		}
	}
	return result
}

func (s *Service) canvasNodeNames(ctx context.Context, ids []string) (map[string]string, error) {
	names := map[string]string{}
	for _, nodeID := range ids {
		n, err := s.Nodes.Get(ctx, nodeID)
		if err != nil || n.DeletedAt != "" {
			return nil, errors.New("方案引用的服务器不存在或已删除")
		}
		names[nodeID] = n.Name
	}
	return names, nil
}

// canvasCycle reports a connection on a directed cycle between servers, or "".
// Relays that loop back would forward traffic around in circles; the proxy
// module refuses such relays too, but only one save at a time, after part of the
// drawing was already created.
func canvasCycle(edges []*canvasEdge) string {
	outgoing := map[string][]*canvasEdge{}
	for _, edge := range edges {
		outgoing[edge.sourceNode] = append(outgoing[edge.sourceNode], edge)
	}
	const (
		unvisited = iota
		active
		done
	)
	state := map[string]int{}
	var visit func(string) string
	visit = func(nodeID string) string {
		state[nodeID] = active
		for _, edge := range outgoing[nodeID] {
			switch state[edge.targetNode] {
			case active:
				return edge.connectionID
			case unvisited:
				if found := visit(edge.targetNode); found != "" {
					return found
				}
			}
		}
		state[nodeID] = done
		return ""
	}
	for _, edge := range edges {
		if state[edge.sourceNode] == unvisited {
			if found := visit(edge.sourceNode); found != "" {
				return found
			}
		}
	}
	return ""
}
