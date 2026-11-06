package proxy

import (
	"reflect"
	"strings"
	"testing"
)

func TestCanvasPlanRelayCycleMatchesPublish(t *testing.T) {
	for _, tc := range []struct {
		name    string
		relays  [][2]int
		enabled bool
		twoHops bool
		wantOK  bool
	}{
		{"existing reverse relay", [][2]int{{1, 0}}, true, true, false},
		{"disabled reverse relay", [][2]int{{1, 0}}, false, true, true},
		{"existing indirect return path", [][2]int{{1, 2}, {2, 0}}, true, false, false},
		{"new edges complete existing return path", [][2]int{{2, 0}}, true, true, false},
		{"unrelated relay", [][2]int{{2, 1}}, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPublishPanel(t)
			nodes := []string{p.nodeA, p.nodeB, p.nodeC}
			for i, relay := range tc.relays {
				in, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "existing relay", NodeID: nodes[relay[1]], Protocol: "trojan", Listen: "0.0.0.0", Port: 18000 + i, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "existing relay", NodeID: nodes[relay[0]], Protocol: "trojan", Enabled: tc.enabled, TargetInboundID: in.ID, SupportTCP: true, SupportUDP: true}); err != nil {
					t.Fatal(err)
				}
			}
			connections := []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)}
			if tc.twoHops {
				connections = append(connections, canvasTestHop("i-b", "i-c", 9443))
			}
			layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, p.inbound.ID, connections)
			scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: tc.name, Execution: "server", Layout: layout})
			if err != nil {
				t.Fatal(err)
			}
			beforeIn, _, err := p.service.Repository.Inbounds(p.ctx, "", 1, 10000, true)
			if err != nil {
				t.Fatal(err)
			}
			beforeOut, _, err := p.service.Repository.Outbounds(p.ctx, "", 1, 10000, true)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
			if err != nil || plan.OK != tc.wantOK {
				t.Fatalf("plan %+v, error %v; want OK %v", plan, err, tc.wantOK)
			}
			if !tc.wantOK {
				if !strings.Contains(plan.Message, "relay outbound would create a routing loop") {
					t.Fatalf("missing loop diagnostic: %+v", plan)
				}
				found := false
				for _, connection := range plan.Connections {
					found = found || connection.Status == "error" && strings.Contains(connection.Message, "routing loop")
				}
				if !found {
					t.Fatalf("loop not mapped to connection: %+v", plan)
				}
			}
			afterIn, _, err := p.service.Repository.Inbounds(p.ctx, "", 1, 10000, true)
			if err != nil {
				t.Fatal(err)
			}
			afterOut, _, err := p.service.Repository.Outbounds(p.ctx, "", 1, 10000, true)
			if err != nil || !reflect.DeepEqual(beforeIn, afterIn) || !reflect.DeepEqual(beforeOut, afterOut) || p.deployer.calls != 0 {
				t.Fatalf("preflight changed proxy objects or deployed: %v", err)
			}
			_, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
			if (err == nil) != tc.wantOK {
				t.Fatalf("publish error %v; want success %v", err, tc.wantOK)
			}
			if !tc.wantOK {
				if !strings.Contains(err.Error(), "routing loop") || p.deployer.calls != 0 {
					t.Fatalf("unexpected publish result: %v, calls %d", err, p.deployer.calls)
				}
				in, _, inErr := p.service.Repository.Inbounds(p.ctx, "", 1, 10000, true)
				out, _, outErr := p.service.Repository.Outbounds(p.ctx, "", 1, 10000, true)
				if inErr != nil || outErr != nil || !reflect.DeepEqual(beforeIn, in) || !reflect.DeepEqual(beforeOut, out) {
					t.Fatal("rejected publication changed proxy objects", inErr, outErr)
				}
			}
		})
	}
}

func TestCanvasPlanStaleRelayRemainsUntilEnsureCompletes(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeB, B: p.nodeA}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme, _, err := p.publishScheme(t, "rewire", layout)
	if err != nil {
		t.Fatal(err)
	}
	layout = canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 9443)})
	scheme, err = p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: scheme.Name, Execution: "server", Layout: layout, UpdatedAt: scheme.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || plan.OK || !strings.Contains(plan.Message, "routing loop") {
		t.Fatalf("stale reverse edge omitted: %+v, %v", plan, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "routing loop") {
		t.Fatalf("publish accepted stale reverse edge: %v", err)
	}
	if p.deployer.calls != 1 {
		t.Fatalf("rejected rewire deployed: %d calls", p.deployer.calls)
	}
}

func TestCanvasPlanReenablingOwnedRelaysChecksCombinedGraph(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
	scheme, _, err := p.publishScheme(t, "reenable", layout)
	if err != nil {
		t.Fatal(err)
	}
	_, outbounds, chains := p.publishedObjects(t, scheme.Name)
	for _, chain := range chains {
		if err := p.service.DeleteChain(p.ctx, chain.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, out := range outbounds {
		if _, err := p.service.SaveOutbound(p.ctx, out.ID, OutboundInput{Name: out.Name, NodeID: out.NodeID, Protocol: out.Protocol, Enabled: false, TargetInboundID: out.TargetInboundID, SupportTCP: true, SupportUDP: true}); err != nil {
			t.Fatal(err)
		}
	}
	in, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "return relay", NodeID: p.nodeA, Protocol: "trojan", Listen: "0.0.0.0", Port: 18000, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "return relay", NodeID: p.nodeC, Protocol: "trojan", Enabled: true, TargetInboundID: in.ID, SupportTCP: true, SupportUDP: true}); err != nil {
		t.Fatal(err)
	}
	// Each owned edge is harmless in the current graph on its own. Re-enabling
	// both as canvasEnsure would do closes C -> A -> B -> C.
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || plan.OK || !strings.Contains(plan.Message, "routing loop") {
		t.Fatalf("combined owned edges omitted: %+v, %v", plan, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "routing loop") {
		t.Fatalf("publish accepted combined owned edges: %v", err)
	}
	for _, out := range outbounds {
		current, err := p.service.Repository.Outbound(p.ctx, out.ID, false)
		if err != nil || current.Enabled {
			t.Fatal("rejected publication enabled an owned relay", current, err)
		}
	}
	if p.deployer.calls != 1 {
		t.Fatalf("rejected reenable deployed: %d calls", p.deployer.calls)
	}
}
