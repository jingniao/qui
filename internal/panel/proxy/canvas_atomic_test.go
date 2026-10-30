package proxy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

// publishScheme saves a drawing and publishes it.
func (p *publishPanel) publishScheme(t *testing.T, name string, layout CanvasLayout) (CanvasScheme, CanvasPublishResult, error) {
	t.Helper()
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: name, Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
	return scheme, result, err
}

// redraw replaces the drawing of a stored scheme.
func (p *publishPanel) redraw(t *testing.T, scheme CanvasScheme, layout CanvasLayout) {
	t.Helper()
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: stored.Name, Execution: "server", Layout: layout, UpdatedAt: stored.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
}

func (p *publishPanel) entryDefault(t *testing.T) string {
	t.Helper()
	entry, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	return entry.DefaultOutbound
}

// A refused re-publication must leave the previous publication exactly as it
// was: its objects and the entry inbound's route. Before the fix the stale
// objects were removed and the entry moved to Direct before the listener check
// refused the drawing.
func TestCanvasRefusedRepublicationKeepsThePreviousObjects(t *testing.T) {
	p := newPublishPanel(t)
	scheme, _, err := p.publishScheme(t, "s", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	inbounds, outbounds, _ := p.publishedObjects(t, "s")
	if len(inbounds) != 1 || len(outbounds) != 1 || p.entryDefault(t) != outbounds[0].ID {
		t.Fatalf("first publication: %d inbounds, %d outbounds, entry %q", len(inbounds), len(outbounds), p.entryDefault(t))
	}
	if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Other", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 9443, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	p.redraw(t, scheme, canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 9443)}))
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "占用") {
		t.Fatalf("publish error %v", err)
	}
	after, afterOut, _ := p.publishedObjects(t, "s")
	if len(after) != 1 || after[0].ID != inbounds[0].ID || len(afterOut) != 1 || afterOut[0].ID != outbounds[0].ID {
		t.Fatalf("refused publication changed the objects: %d inbounds, %d outbounds", len(after), len(afterOut))
	}
	if got := p.entryDefault(t); got != outbounds[0].ID {
		t.Fatalf("refused publication moved the entry to %q", got)
	}
}

// Two arrows into one server on one port are refused before anything exists.
func TestCanvasSharedListenerIsRefusedBeforeCreating(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-c", "i-b", 8443)})
	_, _, err := p.publishScheme(t, "fan-in", layout)
	if err == nil || !strings.Contains(err.Error(), "同一端口") {
		t.Fatalf("publish error %v", err)
	}
	if inbounds, outbounds, chains := p.publishedObjects(t, "fan-in"); len(inbounds)+len(outbounds)+len(chains) != 0 || p.deployer.calls != 0 {
		t.Fatalf("a refused publication left %d/%d/%d objects and %d deployments", len(inbounds), len(outbounds), len(chains), p.deployer.calls)
	}
}

// A failure after part of the drawing was created undoes that part: here the
// exit server is disabled, so its relay inbound cannot be saved after the
// relay's inbound already was.
func TestCanvasFailureHalfwayUndoesTheCreatedObjects(t *testing.T) {
	p := newPublishPanel(t)
	if err := (&node.Repository{Database: p.database}).Update(p.ctx, p.nodeC, "Node C", false); err != nil {
		t.Fatal(err)
	}
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
	if _, _, err := p.publishScheme(t, "halfway", layout); err == nil {
		t.Fatal("a drawing through a disabled server was published")
	}
	if inbounds, outbounds, chains := p.publishedObjects(t, "halfway"); len(inbounds)+len(outbounds)+len(chains) != 0 {
		t.Fatalf("the failed publication left %d inbounds, %d outbounds, %d chains", len(inbounds), len(outbounds), len(chains))
	}
	if got := p.entryDefault(t); got != "direct" {
		t.Fatalf("entry default %q after a failed publication", got)
	}
}

// A new connection creates a distinct tunnel and cannot take an owned listener.
func TestCanvasRewiredSourceRefusesOtherTunnelListener(t *testing.T) {
	p := newPublishPanel(t)
	scheme, _, err := p.publishScheme(t, "rewire", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := p.publishedObjects(t, "rewire")
	p.redraw(t, scheme, canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-c", "i-b", 8443)}))
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil {
		t.Fatal("another tunnel's listener taken over")
	}
	inbounds, outbounds, _ := p.publishedObjects(t, "rewire")
	if len(inbounds) != 1 || inbounds[0].ID != first[0].ID || len(outbounds) != 1 || outbounds[0].NodeID != p.nodeA {
		t.Fatal(inbounds, outbounds)
	}
}

// Directed loops between servers are refused.
func TestCanvasCycleIsRefused(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-a", 9443)})
	if _, _, err := p.publishScheme(t, "loop", layout); err == nil || !strings.Contains(err.Error(), "环路") {
		t.Fatalf("publish error %v", err)
	}
}

// Objects the Owner named by hand with the scheme's bracket prefix are neither
// updated nor pruned, and a scheme name cannot contain brackets.
func TestCanvasLeavesHandNamedObjectsAlone(t *testing.T) {
	p := newPublishPanel(t)
	manual, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "[keep] my own 中转入口", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 7000, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.publishScheme(t, "keep", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Repository.Inbound(p.ctx, manual.ID, false); err != nil {
		t.Fatalf("a hand-made inbound was pruned: %v", err)
	}
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "a] b", Execution: "server", Layout: CanvasLayout{}}); err == nil {
		t.Fatal("a scheme name with a bracket was accepted")
	}
}

// canvasPolicies stands in for the policy module: one target is in use.
type canvasPolicies struct{ used string }

func (canvasPolicies) ForNode(context.Context, string) ([]desiredstate.Policy, error) {
	return nil, nil
}
func (c canvasPolicies) UsesProxyTarget(_ context.Context, id string) (bool, error) {
	return id == c.used, nil
}

// A stale object a routing policy still uses is not removed: the publication is
// refused up front, with the previous objects intact.
func TestCanvasKeepsObjectsARoutingPolicyUses(t *testing.T) {
	p := newPublishPanel(t)
	scheme, _, err := p.publishScheme(t, "policy", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	_, outbounds, _ := p.publishedObjects(t, "policy")
	p.service.Policies = canvasPolicies{used: outbounds[0].ID}
	p.redraw(t, scheme, canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", nil))
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "分流规则") {
		t.Fatalf("publish error %v", err)
	}
	if _, after, _ := p.publishedObjects(t, "policy"); len(after) != 1 {
		t.Fatalf("an outbound a policy uses was removed")
	}
}

// canonicalState describes a node state without ids, secrets or names, so the
// state the canvas produces can be compared with the one the editors produce.
func canonicalState(state desiredstate.State, names map[string]string) string {
	outboundKind := map[string]string{"direct": "direct", "block": "block"}
	lines := []string{}
	for _, outbound := range state.Outbounds {
		target := names[outbound.TargetNodeID]
		outboundKind[outbound.ID] = "outbound→" + target
		lines = append(lines, fmt.Sprintf("outbound %s target=%s port=%d enabled=%t tcp=%t udp=%t failure=%s security=%s transport=%s", outbound.Protocol, target, outbound.Port, outbound.Enabled, outbound.SupportTCP, outbound.SupportUDP, outbound.FailurePolicy, outbound.Settings.Security, outbound.Settings.Transport))
	}
	for _, chain := range state.ProxyChains {
		nodes := []string{}
		for _, id := range chain.NodeIDs {
			nodes = append(nodes, names[id])
		}
		outboundKind["chain:"+chain.ID] = "chain " + strings.Join(nodes, "→")
		lines = append(lines, fmt.Sprintf("chain %s hops=%d failover=%s final=%s", strings.Join(nodes, "→"), len(chain.OutboundIDs), chain.Failover, chain.FinalOutboundID))
	}
	for _, inbound := range state.Inbounds {
		lines = append(lines, fmt.Sprintf("inbound %s port=%d enabled=%t visible=%t default=%s security=%s", inbound.Protocol, inbound.Port, inbound.Enabled, inbound.SubscriptionVisible, outboundKind[inbound.DefaultOutbound], inbound.Settings.Security))
	}
	internal := 0
	for _, credential := range state.Credentials {
		if credential.Internal {
			internal++
		}
	}
	lines = append(lines, fmt.Sprintf("internal credentials=%d", internal))
	sort.Strings(lines)
	return names[state.NodeID] + ":\n" + strings.Join(lines, "\n")
}

func (p *publishPanel) canonicalSnapshots(t *testing.T) string {
	t.Helper()
	names := map[string]string{p.nodeA: "A", p.nodeB: "B", p.nodeC: "C"}
	snapshots, err := p.service.RelaySnapshots(p.ctx, p.nodeA, p.users)
	if err != nil {
		t.Fatal(err)
	}
	parts := []string{}
	for _, snapshot := range snapshots {
		parts = append(parts, canonicalState(snapshot.State, names))
	}
	return strings.Join(parts, "\n---\n")
}

// AU2.2: a canvas drawing A→B and A→B→C produces the same node states, in the
// same publication order, as the inbound, outbound and chain editors do when the
// Owner builds the same relay by hand.
func TestCanvasMatchesTheEditors(t *testing.T) {
	for _, twoHops := range []bool{false, true} {
		t.Run(fmt.Sprintf("two hops %t", twoHops), func(t *testing.T) {
			editors := newPublishPanel(t)
			relayB, err := editors.service.SaveInbound(editors.ctx, "", InboundInput{Name: "relay B", NodeID: editors.nodeB, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, AllowedUserIDs: []string{}, DefaultOutbound: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			toB, err := editors.service.SaveOutbound(editors.ctx, "", OutboundInput{Name: "to B", NodeID: editors.nodeA, Protocol: "trojan", Enabled: true, TargetInboundID: relayB.ID, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
			if err != nil {
				t.Fatal(err)
			}
			route := toB.ID
			connections := []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}
			if twoHops {
				relayC, err := editors.service.SaveInbound(editors.ctx, "", InboundInput{Name: "relay C", NodeID: editors.nodeC, Protocol: "trojan", Listen: "0.0.0.0", Port: 9443, Enabled: true, InternalRelay: true, AllowedUserIDs: []string{}, DefaultOutbound: "direct"})
				if err != nil {
					t.Fatal(err)
				}
				toC, err := editors.service.SaveOutbound(editors.ctx, "", OutboundInput{Name: "to C", NodeID: editors.nodeB, Protocol: "trojan", Enabled: true, TargetInboundID: relayC.ID, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
				if err != nil {
					t.Fatal(err)
				}
				chain, err := editors.service.SaveChain(editors.ctx, "", ChainInput{Name: "A→B→C", NodeIDs: []string{editors.nodeA, editors.nodeB, editors.nodeC}, OutboundIDs: []string{toB.ID, toC.ID}, FinalOutboundID: "direct", SupportTCP: true, SupportUDP: true, Enabled: true, FailurePolicy: "Block"})
				if err != nil {
					t.Fatal(err)
				}
				route = "chain:" + chain.ID
				connections = append(connections, canvasTestHop("i-b", "i-c", 9443))
			}
			if _, err := editors.service.SaveInbound(editors.ctx, editors.inbound.ID, inboundInputOf(editors.inbound, route)); err != nil {
				t.Fatal(err)
			}

			canvas := newPublishPanel(t)
			set := canvasTestSet{A: canvas.nodeA, B: canvas.nodeB}
			if twoHops {
				set.C = canvas.nodeC
			}
			if _, _, err := canvas.publishScheme(t, "same", canvasTestLayout(canvas, set, canvas.inbound.ID, connections)); err != nil {
				t.Fatal(err)
			}
			want, got := editors.canonicalSnapshots(t), canvas.canonicalSnapshots(t)
			if want != got {
				t.Fatalf("canvas states differ from the editors'\neditors:\n%s\n\ncanvas:\n%s", want, got)
			}
		})
	}
}

// Removing every arrow still publishes the nodes that lose objects, so the
// removal reaches them instead of staying a draft.
func TestCanvasRemovalAloneIsPublished(t *testing.T) {
	p := newPublishPanel(t)
	scheme, _, err := p.publishScheme(t, "empty", canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}))
	if err != nil {
		t.Fatal(err)
	}
	p.redraw(t, scheme, canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", nil))
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	order := p.deployer.order[len(p.deployer.order)-1]
	if len(order) != 2 {
		t.Fatalf("removal published to %v", order)
	}
	if inbounds, outbounds, _ := p.publishedObjects(t, "empty"); len(inbounds)+len(outbounds) != 0 {
		t.Fatal("objects of a removed arrow remain")
	}
}
