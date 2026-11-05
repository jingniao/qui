package proxy

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"

	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

// A GOST tunnel is a port_forward connection whose end-to-end hop is a GOST relay
// on TLS with a client CA: the entry node keeps forwarding to its configured
// targets, but every service dials them through the exit node's relay listener.
// It is the only way the canvas uses the GOST core for a tunnel; encrypted
// user-proxy hops keep using the existing Trojan tunnel.
type canvasRelay struct {
	tunnelID     string
	connectionID string
	entryNode    string
	exitNode     string
	name         string
	port         int
	allocated    bool
	inboundID    string
	material     gostMaterial
}

// canvasGostRelay validates one GOST port_forward connection and prepares its
// exit-side listener. Both endpoints must run the GOST core and report settings
// contract 3, because both halves of the tunnel are rendered from it.
func (s *Service) canvasGostRelay(ctx context.Context, connection CanvasConnection, sourceNode, targetNode, name, prefix string) (*canvasRelay, error) {
	tunnel, err := s.Repository.canvasTunnel(ctx, connection.TunnelID)
	if err != nil {
		return nil, err
	}
	if !tunnel.gostRelay() {
		return nil, fmt.Errorf("连接 %q：GOST 中继需要 GOST 类型的共享隧道", connection.ID)
	}
	if tunnel.material == nil {
		// The set is issued when the tunnel is saved as gost. Planning never
		// invents one: a publication that followed the preview would deploy
		// credentials the Owner never saw, and a set that is unreadable or expired
		// has to be replaced instead of silently regenerated.
		return nil, fmt.Errorf("连接 %q：GOST 中继凭据不可用（%v），请先轮换隧道凭据再发布", connection.ID, ErrGostTunnelMaterial)
	}
	for _, nodeID := range []string{sourceNode, targetNode} {
		if err := s.requireGostRelay(ctx, nodeID); err != nil {
			return nil, fmt.Errorf("连接 %q：%w", connection.ID, err)
		}
	}
	digest := sha256.Sum256([]byte("gost-relay:" + connection.TunnelID))
	relay := &canvasRelay{
		tunnelID:     connection.TunnelID,
		connectionID: connection.ID,
		entryNode:    sourceNode,
		exitNode:     targetNode,
		name:         fmt.Sprintf("%srelay %x%s", prefix, digest[:8], canvasInboundSuffix),
		material:     *tunnel.material,
	}
	if tunnel.allocatedPort != nil {
		relay.port = *tunnel.allocatedPort
	}
	return relay, nil
}

// requireGostRelay refuses an endpoint that cannot render either half of a GOST
// tunnel. The contract version is required from the capability report, never
// inferred from an Agent version string.
func (s *Service) requireGostRelay(ctx context.Context, nodeID string) error {
	n, err := s.Nodes.Get(ctx, nodeID)
	if err != nil {
		return err
	}
	if !n.Enabled || n.DeletedAt != "" {
		return errors.New("服务器不可用")
	}
	for _, core := range node.EnabledCores(n.Capabilities) {
		if core.Name != "gost" {
			continue
		}
		if core.GostContractVersion < desiredstate.GostRelayContractVersion {
			return errors.New("node does not advertise the gost relay contract (version 3); update it first")
		}
		if !core.ForwardInboundSupported {
			return errors.New("node does not advertise forward inbound support for gost")
		}
		return nil
	}
	return errors.New("node does not advertise the gost core")
}

// gostRelayPublicHost is the address the entry node dials: the exit node's public
// IP. A node without one cannot host a relay, because the other side would have
// nothing to dial.
func (s *Service) gostRelayPublicHost(ctx context.Context, nodeID string) (string, error) {
	n, err := s.Nodes.Get(ctx, nodeID)
	if err != nil {
		return "", err
	}
	if n.PublicIP == "" {
		return "", errors.New("中转服务器没有公网 IP，无法作为 GOST 中继")
	}
	return n.PublicIP, nil
}

// hopFor renders the entry half of the tunnel from the sealed material.
func (r *canvasRelay) hopFor(host string) desiredstate.GostHop {
	return desiredstate.GostHop{
		Host:          host,
		Port:          r.port,
		ServerName:    r.material.ServerName,
		CACertPEM:     r.material.CACertPEM,
		ClientCertPEM: r.material.ClientCertPEM,
		ClientKeyPEM:  r.material.ClientKeyPEM,
	}
}

// relayInput is the exit listener: a relay handler on a TLS listener that requires
// the client certificate of the entry node. It forwards nothing itself, so it has
// no target pool and never enters the node's traffic totals. It listens on the
// wildcard address of the family the entry node dials, so an IPv6 endpoint is
// reachable at all.
func (r *canvasRelay) relayInput(host string) InboundInput {
	listen := "0.0.0.0"
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		listen = "::"
	}
	return InboundInput{
		Core:            "gost",
		Name:            r.name,
		NodeID:          r.exitNode,
		Protocol:        "forward",
		Listen:          listen,
		Port:            r.port,
		Enabled:         true,
		DefaultOutbound: "direct",
		AllowedUserIDs:  []string{},
		Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{
			Mode:  desiredstate.GostForwardModeRelay,
			Relay: &desiredstate.GostRelayTLS{CertificatePEM: r.material.ServerCertPEM, PrivateKeyPEM: r.material.ServerKeyPEM, ClientCAPEM: r.material.CACertPEM},
		}},
	}
}

// relayPortTaken collects the numeric ports the exit node already reserves plus
// every listener this plan adds on it. Ports are node-global, so a relay competes
// with forwards and with the other cores exactly like any inbound.
func (s *Service) relayPortTaken(ctx context.Context, plan *canvasPlan, relay *canvasRelay) (map[int]bool, error) {
	taken := map[int]bool{}
	owned, err := s.canvasOwnedInbounds(ctx, plan)
	if err != nil {
		return nil, err
	}
	exclude := []PortOwner{}
	for _, inbound := range owned {
		if inbound.Name == relay.name {
			exclude = append(exclude, PortOwner{Kind: "inbound", ID: inbound.ID})
		}
	}
	occupied, err := s.Repository.portOccupants(ctx, s.Repository.Database, relay.exitNode, exclude)
	if err != nil {
		return nil, err
	}
	for port := range occupied {
		taken[port] = true
	}
	for _, edge := range plan.edges {
		if edge.targetNode == relay.exitNode {
			taken[edge.port] = true
		}
	}
	for _, forward := range plan.forwards {
		if forward.NodeID == relay.exitNode {
			for _, port := range desiredstate.ListenPorts(forward.Port, forward.Settings) {
				taken[port] = true
			}
		}
	}
	for _, other := range plan.relays {
		if other != relay && other.exitNode == relay.exitNode && other.port != 0 {
			taken[other.port] = true
		}
	}
	return taken, nil
}

// allocateRelayPort keeps the port a published tunnel already uses and otherwise
// takes the first free number of the exit node's configured range.
func (s *Service) allocateRelayPort(ctx context.Context, plan *canvasPlan, relay *canvasRelay) error {
	if relay.port != 0 {
		return nil
	}
	taken, err := s.relayPortTaken(ctx, plan, relay)
	if err != nil {
		return err
	}
	ports, err := allocatePorts(ctx, s.Repository.Database, relay.exitNode, 1, nil, taken)
	if err != nil {
		return fmt.Errorf("服务器端口范围内没有可用的 GOST 中继端口: %w", err)
	}
	relay.port, relay.allocated = ports[0], true
	return nil
}

// relayFor finds the GOST relay of one connection, or nil for a plain forward.
func (p *canvasPlan) relayFor(connectionID string) *canvasRelay {
	for _, relay := range p.relays {
		if relay.connectionID == connectionID {
			return relay
		}
	}
	return nil
}

// canvasEnsureRelay creates or updates the exit listener of one GOST tunnel and
// records the port it took, so the next publication of any drawing that shares
// the tunnel keeps the same listener.
func (s *Service) canvasEnsureRelay(ctx context.Context, plan *canvasPlan, relay *canvasRelay, undo *canvasUndo) error {
	existing, err := s.Repository.canvasInboundNamed(ctx, relay.exitNode, relay.name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	host, err := s.gostRelayPublicHost(ctx, relay.exitNode)
	if err != nil {
		return err
	}
	saved, err := s.saveGeneratedInbound(ctx, existing.ID, relay.relayInput(host))
	if err != nil {
		return err
	}
	if err := s.ownCanvasObject(ctx, plan, "inbound", saved.ID, relay.name, undo); err != nil {
		return err
	}
	if existing.ID == "" {
		undo.add(func(ctx context.Context) error { return s.Repository.DeleteInbound(ctx, saved.ID) })
	} else {
		previous := existing
		undo.add(func(ctx context.Context) error {
			_, err := s.saveGeneratedInbound(ctx, previous.ID, inboundInputOf(previous, previous.DefaultOutbound))
			return err
		})
	}
	relay.inboundID = saved.ID
	if !relay.allocated {
		return nil
	}
	result, err := s.Repository.Database.ExecContext(ctx, "UPDATE canvas_tunnels SET allocated_port=? WHERE id=? AND updated_at=?", relay.port, relay.tunnelID, plan.tunnelVersions[relay.tunnelID])
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	tunnelID := relay.tunnelID
	undo.add(func(ctx context.Context) error {
		_, err := s.Repository.Database.ExecContext(ctx, "UPDATE canvas_tunnels SET allocated_port=NULL WHERE id=?", tunnelID)
		return err
	})
	return nil
}

// canvasPublishOrder sorts the nodes of one publication so that every dependency
// is published before the node that dials it: the target of a relay edge this
// drawing publishes, and the GOST relay exit an entry forward hops through.
// before[node] lists those dependencies, and the caller derives them from the
// plan, so an outbound stored elsewhere never orders or cycles a batch. Each step
// takes the earliest node, in the order RelaySnapshots produced, whose
// dependencies are already placed, so satisfying one dependency never pulls an
// unrelated node ahead of the relay target that order put before it.
func canvasPublishOrder(nodeIDs []string, before map[string][]string) ([]string, error) {
	ordered := make([]string, 0, len(nodeIDs))
	placed := map[string]bool{}
	for len(ordered) < len(nodeIDs) {
		progressed := false
		for _, nodeID := range nodeIDs {
			if placed[nodeID] {
				continue
			}
			ready := true
			for _, dependency := range before[nodeID] {
				ready = ready && placed[dependency]
			}
			if ready {
				placed[nodeID] = true
				ordered = append(ordered, nodeID)
				progressed = true
				break
			}
		}
		if !progressed {
			return nil, errors.New("服务器之间的中转依赖形成了环路，无法确定发布顺序")
		}
	}
	return ordered, nil
}
