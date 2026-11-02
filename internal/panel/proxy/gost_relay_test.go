package proxy

import (
	"encoding/json"
	"testing"

	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// gostRelayPanel is the fixture both G3 panel checks share: two nodes that report
// the GOST relay contract, a port range on the exit node for the relay listener and
// one port_forward connection whose end-to-end hop is a GOST tunnel.
func gostRelayPanel(t *testing.T) (*publishPanel, CanvasScheme, CanvasTunnel) {
	t.Helper()
	p := newPublishPanel(t)
	for _, nodeID := range []string{p.nodeA, p.nodeB} {
		n, err := p.service.Nodes.Get(p.ctx, nodeID)
		if err != nil {
			t.Fatal(err)
		}
		n.Capabilities.MultiCoreSupported = true
		// The canvas checks the node-level flag before the per-core report.
		n.Capabilities.ForwardInboundSupported = true
		n.Capabilities.Cores = append(n.Capabilities.Cores, &pb.CoreCapability{
			Name: "gost", Version: "3.3.2", ForwardInboundSupported: true, GostContractVersion: desiredstate.GostRelayContractVersion,
			Protocols: []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true, Tcp: true, Udp: true}},
		})
		raw, err := json.Marshal(n.Capabilities)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), nodeID); err != nil {
			t.Fatal(err)
		}
	}
	// The relay listener takes its port from the exit node's allocation range.
	if _, err := p.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,23000,23010)", p.nodeB); err != nil {
		t.Fatal(err)
	}
	connection := CanvasConnection{ID: "relay-edge", Source: "i-a", Target: "i-b", Type: "port_forward", TunnelProtocol: "gost",
		ListenPort: canvasTestPort(23200), TargetHost: "192.0.2.99", TargetPort: canvasTestPort(8443), SupportTCP: true, SupportUDP: true}
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{connection})
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "gost relay", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	tunnels, err := p.service.Repository.CanvasTunnels(p.ctx)
	if err != nil || len(tunnels) != 1 {
		t.Fatalf("tunnels %+v %v", tunnels, err)
	}
	return p, scheme, tunnels[0]
}

// generatedRelay returns the two halves a publication generated: the relay listener
// on the exit node and the forward on the entry node.
func generatedRelay(t *testing.T, p *publishPanel, name string) (Inbound, Inbound) {
	t.Helper()
	inbounds, _, _ := p.publishedObjects(t, name)
	exit, entry := Inbound{}, Inbound{}
	for _, inbound := range inbounds {
		switch {
		case inbound.NodeID == p.nodeB && inbound.Settings.Forward.RelayMode():
			exit = inbound
		case inbound.NodeID == p.nodeA && len(inbound.Settings.Forward.Hops) > 0:
			entry = inbound
		}
	}
	if exit.ID == "" || entry.ID == "" {
		t.Fatalf("publication generated %+v", inbounds)
	}
	return exit, entry
}

// TestGostTunnelPublishesTheExitBeforeTheEntry covers the deployment order of a
// GOST tunnel: the relay listener has to serve before the entry forward moves onto
// the tunnel's credentials, and a rotation therefore redeploys both halves in that
// order.
func TestGostTunnelPublishesTheExitBeforeTheEntry(t *testing.T) {
	p, scheme, tunnel := gostRelayPanel(t)
	ordered, err := p.service.gostOrder(p.ctx, []string{p.nodeA, p.nodeB})
	if err != nil || len(ordered) != 2 || ordered[0] != p.nodeB || ordered[1] != p.nodeA {
		t.Fatalf("gost order %v %v", ordered, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if order := p.deployer.order[0]; len(order) != 2 || order[0] != p.nodeB || order[1] != p.nodeA {
		t.Fatalf("publication order %v is not exit then entry", order)
	}
	// The inline tunnel was created with the drawing, so its credential set is the
	// first generation once the plan had to resolve it.
	if tunnel, err = p.service.Repository.canvasTunnel(p.ctx, tunnel.ID); err != nil {
		t.Fatal(err)
	}
	if tunnel.Credentials == nil || tunnel.Credentials.Generation != 1 {
		t.Fatalf("tunnel credentials %+v", tunnel.Credentials)
	}
	exit, entry := generatedRelay(t, p, scheme.Name)
	exitNode, err := p.service.Nodes.Get(p.ctx, p.nodeB)
	if err != nil {
		t.Fatal(err)
	}
	relayPort := exit.Port
	if entry.Settings.Forward.Hops[0].Port != relayPort || entry.Settings.Forward.Hops[0].Host != exitNode.PublicIP {
		t.Fatalf("entry hop %+v does not dial the relay listener on port %d", entry.Settings.Forward.Hops[0], relayPort)
	}
	if exit.Settings.Forward.Relay.CertificatePEM == "" || entry.Settings.Forward.Hops[0].ClientCertPEM == "" {
		t.Fatal("the publication did not carry the tunnel credentials")
	}

	// Rotating replaces the whole set: a new generation and fingerprint under the
	// same synthetic server name, and the tunnel is modified until it is published
	// again.
	rotated, err := p.service.RotateCanvasTunnelCredentials(p.ctx, tunnel.ID, tunnel.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Credentials == nil || rotated.Credentials.Generation != tunnel.Credentials.Generation+1 {
		t.Fatalf("rotation %+v", rotated.Credentials)
	}
	if rotated.Credentials.Fingerprint == tunnel.Credentials.Fingerprint || rotated.Credentials.ServerName != tunnel.Credentials.ServerName {
		t.Fatalf("rotation kept the old set: %+v", rotated.Credentials)
	}
	if rotated.PublishStatus != "modified" {
		t.Fatalf("rotation left the tunnel %q", rotated.PublishStatus)
	}
	// The next publication is what installs the new set, and it still deploys the
	// exit first.
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if order := p.deployer.order[1]; len(order) != 2 || order[0] != p.nodeB || order[1] != p.nodeA {
		t.Fatalf("rotation published %v, not exit then entry", order)
	}
	row, err := p.service.Repository.canvasTunnel(p.ctx, tunnel.ID)
	if err != nil || row.material == nil {
		t.Fatalf("sealed material %v", err)
	}
	exit, entry = generatedRelay(t, p, scheme.Name)
	if exit.Settings.Forward.Relay.CertificatePEM != row.material.ServerCertPEM {
		t.Fatal("the relay listener kept the old server certificate")
	}
	if entry.Settings.Forward.Hops[0].ClientCertPEM != row.material.ClientCertPEM || entry.Settings.Forward.Hops[0].CACertPEM != row.material.CACertPEM {
		t.Fatal("the entry hop kept the old client credentials")
	}
}

// When the two servers already share an ordinary relay, publishing a GOST tunnel
// validates the whole relay component, including the GOST halves. Each inbound
// is judged by its own core: the GOST relay and forward are not in the primary
// Xray core's protocol list (v1.3.0-rc.3 hosts: "associated node does not support
// an enabled inbound protocol").
func TestGostTunnelPublishesBesideAnExistingRelay(t *testing.T) {
	p, scheme, _ := gostRelayPanel(t)
	relayB, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "relay B", NodeID: p.nodeB, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, AllowedUserIDs: []string{}, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "to B", NodeID: p.nodeA, Protocol: "trojan", Enabled: true, TargetInboundID: relayB.ID, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.RelaySnapshots(p.ctx, p.nodeA, p.users); err != nil {
		t.Fatal(err)
	}
}

// Chain and relay checks ask every core of a node, not only the primary's list.
func TestSupportsAsksEveryCore(t *testing.T) {
	n := node.Node{Capabilities: &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{
		{Name: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true}}},
		{Name: "sing-box", Protocols: []*pb.ProtocolCapability{{Protocol: "hysteria2", Inbound: true, Outbound: true, Udp: true}}},
	}}}
	if !supports(n, "hysteria2", false, false, true) || !supports(n, "trojan", true, true, false) {
		t.Fatal("a protocol of one of the node's cores was refused")
	}
	if supports(n, "hysteria2", false, true, false) || supports(n, "vless", true, true, false) {
		t.Fatal("a capability no core reports was accepted")
	}
}
