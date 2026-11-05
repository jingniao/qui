package proxy

import (
	"encoding/json"
	"strings"
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
	// Existing GOST drawings remain publishable until M1 migrates them.
	scheme, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "gost relay", Execution: "server", Layout: layout})
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
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := p.service.canvasSnapshots(p.ctx, plan, p.users)
	if err != nil || len(snapshots) != 2 || snapshots[0].State.NodeID != p.nodeB || snapshots[1].State.NodeID != p.nodeA {
		t.Fatalf("gost order %+v %v", snapshots, err)
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

// TestCanvasSnapshotsOrdersBothDependencies covers an ordinary relay to C and a
// GOST tunnel from A to B in the same publication: the expanded set is ordered
// once, so the shared target C is published before the GOST exit B and before the
// entry A.
func TestCanvasSnapshotsOrdersBothDependencies(t *testing.T) {
	p, scheme, _ := gostRelayPanel(t)
	relayC, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "relay C", NodeID: p.nodeC, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, AllowedUserIDs: []string{}, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	for _, nodeID := range []string{p.nodeA, p.nodeB} {
		if _, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "to C " + nodeID, NodeID: nodeID, Protocol: "trojan", Enabled: true, TargetInboundID: relayC.ID, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"}); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := p.service.canvasSnapshots(p.ctx, plan, p.users)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{}
	for _, snapshot := range snapshots {
		order = append(order, snapshot.State.NodeID)
	}
	if len(order) != 3 || order[0] != p.nodeC || order[1] != p.nodeB || order[2] != p.nodeA {
		t.Fatalf("publication order %v is not C, B, A", order)
	}
}

// TestUnusableGostMaterialIsRefusedInsteadOfRegenerated keeps credential issuance
// on the save path: a plan, which the read-only preview endpoint also reaches,
// refuses a tunnel whose sealed set is missing or unreadable and leaves the
// replacement to an explicit rotation.
func TestUnusableGostMaterialIsRefusedInsteadOfRegenerated(t *testing.T) {
	p, scheme, tunnel := gostRelayPanel(t)
	if _, err := p.database.Exec("UPDATE canvas_tunnels SET gost_material=NULL WHERE id=?", tunnel.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.OK || !strings.Contains(plan.Message, "轮换") {
		t.Fatalf("plan %+v", plan)
	}
	var sealed []byte
	if err := p.database.QueryRow("SELECT gost_material FROM canvas_tunnels WHERE id=?", tunnel.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if len(sealed) != 0 {
		t.Fatal("the plan issued a credential set")
	}
}

// An inbound the canvas generated for a GOST tunnel is still editable through the
// ordinary API - disable it, change its settings - but only with the material the
// tunnel wrote, and it cannot be renamed: a drawing knows its objects by the names
// it generated.
func TestGeneratedRelayInboundKeepsItsTunnelMaterial(t *testing.T) {
	p, scheme, _ := gostRelayPanel(t)
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	exit, entry := generatedRelay(t, p, scheme.Name)
	stored := exit.Settings.Forward.Relay
	// The API returns the record with the private halves masked, and the editor
	// sends it back unchanged.
	disabled := inboundInputOf(exit, exit.DefaultOutbound)
	disabled.Enabled = false
	disabled.Settings = mask(exit.Settings)
	saved, err := p.service.SaveInbound(p.ctx, exit.ID, disabled)
	if err != nil {
		t.Fatalf("an ordinary save of a generated relay was refused: %v", err)
	}
	if saved.Enabled || saved.Name != exit.Name {
		t.Fatalf("the save was not applied: %+v", saved)
	}
	renamed := inboundInputOf(exit, exit.DefaultOutbound)
	renamed.Name = "renamed relay"
	if _, err := p.service.SaveInbound(p.ctx, exit.ID, renamed); err == nil {
		t.Fatal("the ordinary API renamed a canvas-owned relay")
	}
	storedRelay, err := p.service.Repository.Inbound(p.ctx, exit.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if storedRelay.Settings.Forward.Relay == nil || *storedRelay.Settings.Forward.Relay != *stored {
		t.Fatalf("the tunnel material changed: %+v", storedRelay.Settings.Forward.Relay)
	}
	// A record that brings its own material is still refused, for the relay half
	// and for the hops of the entry half alike.
	foreign, err := newGostMaterial("tunnel-other", 1)
	if err != nil {
		t.Fatal(err)
	}
	changed := inboundInputOf(exit, exit.DefaultOutbound)
	changed.Settings = desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Mode: desiredstate.GostForwardModeRelay,
		Relay: &desiredstate.GostRelayTLS{CertificatePEM: foreign.ServerCertPEM, PrivateKeyPEM: foreign.ServerKeyPEM, ClientCAPEM: foreign.CACertPEM}}}
	if _, err := p.service.SaveInbound(p.ctx, exit.ID, changed); err == nil {
		t.Fatal("the ordinary API replaced the material of a generated relay")
	}
	hops := inboundInputOf(entry, entry.DefaultOutbound)
	hops.Settings = desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "203.0.113.9", Port: 8443, Network: "tcp",
		Hops: []desiredstate.GostHop{{Host: "198.51.100.7", Port: 23001, ServerName: foreign.ServerName, CACertPEM: foreign.CACertPEM, ClientCertPEM: foreign.ClientCertPEM, ClientKeyPEM: foreign.ClientKeyPEM}}}}
	if _, err := p.service.SaveInbound(p.ctx, entry.ID, hops); err == nil {
		t.Fatal("the ordinary API replaced the hops of a generated forward")
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

// The publication order counts only the tunnels this drawing publishes. A draft
// drawing the opposite direction keeps its own tunnel in the library, and that
// relationship must neither order nor cycle the batch that goes A → B.
func TestUnusedReverseGostDraftDoesNotBlockPublication(t *testing.T) {
	p, scheme, _ := gostRelayPanel(t)
	reverse := CanvasConnection{ID: "relay-back", Source: "i-b", Target: "i-a", Type: "port_forward", TunnelProtocol: "gost",
		ListenPort: canvasTestPort(23300), TargetHost: "192.0.2.99", TargetPort: canvasTestPort(8443), SupportTCP: true, SupportUDP: true}
	if _, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "reverse draft", Execution: "server",
		Layout: canvasTestLayout(p, canvasTestSet{A: p.nodeB, B: p.nodeA}, "", []CanvasConnection{reverse})}); err != nil {
		t.Fatal(err)
	}
	if tunnels, err := p.service.Repository.CanvasTunnels(p.ctx); err != nil || len(tunnels) != 2 {
		t.Fatalf("tunnels %+v %v", tunnels, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatalf("an unused reverse draft blocked the publication: %v", err)
	}
	if order := p.deployer.order[0]; len(order) != 2 || order[0] != p.nodeB || order[1] != p.nodeA {
		t.Fatalf("publication order %v is not exit then entry", order)
	}
}

// The entry dials the exit's advertised address, so an IPv6 exit needs a relay
// listener of the same family; a v4 wildcard would never answer the dial.
func TestGostRelayIPv6ExitListensOnTheSameFamily(t *testing.T) {
	p, scheme, _ := gostRelayPanel(t)
	if _, err := p.database.Exec("UPDATE nodes SET public_ip=? WHERE id=?", "2001:db8::11", p.nodeB); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	exit, entry := generatedRelay(t, p, scheme.Name)
	if exit.Listen != "::" {
		t.Fatalf("relay listener of an IPv6 exit listens on %q", exit.Listen)
	}
	if entry.Settings.Forward.Hops[0].Host != "2001:db8::11" {
		t.Fatalf("entry hop dials %q", entry.Settings.Forward.Hops[0].Host)
	}
}

// GOST A→B and B→C in one drawing, plus an ordinary relay C→D outside it whose
// target RelaySnapshots placed before C: resolving B's dependency must not pull
// C ahead of D. A dependency cycle is still refused.
func TestCanvasPublishOrderKeepsTheBaseOrderOfUnrelatedTargets(t *testing.T) {
	ordered, err := canvasPublishOrder([]string{"B", "A", "D", "C"}, map[string][]string{"A": {"B"}, "B": {"C"}})
	if err != nil || strings.Join(ordered, ",") != "D,C,B,A" {
		t.Fatalf("order %v %v", ordered, err)
	}
	if _, err := canvasPublishOrder([]string{"A", "B"}, map[string][]string{"A": {"B"}, "B": {"A"}}); err == nil {
		t.Fatal("a dependency cycle was ordered")
	}
}

// renameOf is the stored record under another name, as the inbound editor sends
// it.
func renameOf(inbound Inbound, name string) InboundInput {
	input := inboundInputOf(inbound, inbound.DefaultOutbound)
	input.Name = name
	return input
}

// A drawing owns its inbounds by identity, so renaming one in the inbound editor
// would detach it from the canvas. The editor refuses that rename, and the stored
// name, the ownership and the tunnel material stay as they are.
func TestCanvasOwnedInboundNameIsReadOnlyInTheEditor(t *testing.T) {
	p, scheme, _ := gostRelayPanel(t)
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	exit, entry := generatedRelay(t, p, scheme.Name)
	for _, inbound := range []Inbound{exit, entry} {
		owner, err := p.service.Repository.objectOwner(p.ctx, "inbound", inbound.ID)
		if err != nil || owner == "" {
			t.Fatalf("inbound %q has no owner: %q %v", inbound.Name, owner, err)
		}
		if _, err := p.service.SaveInbound(p.ctx, inbound.ID, renameOf(inbound, inbound.Name+" 改")); err == nil {
			t.Fatalf("the editor renamed the canvas-owned inbound %q", inbound.Name)
		}
		stored, err := p.service.Repository.Inbound(p.ctx, inbound.ID, true)
		if err != nil || stored.Name != inbound.Name {
			t.Fatalf("stored name %q, expected %q (%v)", stored.Name, inbound.Name, err)
		}
		if owner, err = p.service.Repository.objectOwner(p.ctx, "inbound", inbound.ID); err != nil || owner == "" {
			t.Fatalf("the refused rename dropped the ownership of %q: %q %v", inbound.Name, owner, err)
		}
		// The same record without a new name is still the Owner's to save, and it
		// keeps the material the tunnel owns.
		before, err := p.service.Repository.Inbound(p.ctx, inbound.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := p.service.SaveInbound(p.ctx, inbound.ID, renameOf(inbound, inbound.Name))
		if err != nil {
			t.Fatalf("an unchanged save of %q was refused: %v", inbound.Name, err)
		}
		after, err := p.service.Repository.Inbound(p.ctx, saved.ID, true)
		if err != nil {
			t.Fatal(err)
		}
		if after.Name != before.Name {
			t.Fatalf("an unchanged save renamed %q to %q", before.Name, after.Name)
		}
		if !sameRelayMaterial(after.Settings.Forward, before.Settings.Forward) {
			t.Fatalf("an unchanged save of %q changed the tunnel material: mode %q, %d hops", before.Name, after.Settings.Forward.Mode, len(after.Settings.Forward.Hops))
		}
	}
	// Nothing looks stale to the next plan of the same drawing.
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !plan.OK {
		t.Fatalf("plan %+v %v", plan, err)
	}
	for _, server := range plan.Servers {
		if len(server.Removes) != 0 {
			t.Fatalf("the plan removes %+v", server.Removes)
		}
	}
}
