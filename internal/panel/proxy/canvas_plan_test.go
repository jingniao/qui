package proxy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"proxycontrol/pkg/contracts/desiredstate"
	"reflect"
	"strings"
	"testing"
)

func enableCanvasForwardFor(t *testing.T, p *publishPanel, nodeID string) {
	t.Helper()
	n, err := p.service.Nodes.Get(p.ctx, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	n.Capabilities.ForwardInboundSupported = true
	raw, _ := json.Marshal(n.Capabilities)
	if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), nodeID); err != nil {
		t.Fatal(err)
	}
}
func enableCanvasForward(t *testing.T, p *publishPanel) {
	t.Helper()
	enableCanvasForwardFor(t, p, p.nodeA)
}
func forwardCanvasLayout(p *publishPanel) CanvasLayout {
	return canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{{ID: "f", Source: "i-a", Target: "i-b", Type: "port_forward", ListenPort: canvasTestPort(12345), TargetPort: canvasTestPort(443), TargetHost: "example.com", SupportTCP: true}})
}
func savePlanScheme(t *testing.T, p *publishPanel, layout CanvasLayout) CanvasScheme {
	t.Helper()
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "plan", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	return scheme
}
func TestCanvasConnectionForwardValidation(t *testing.T) {
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "a", Ref: "server:a", Role: "entry"}, {InstanceID: "b", Ref: "server:b", Role: "exit"}}, Connections: []CanvasConnection{{ID: "f", Source: "a", Target: "b", Type: "port_forward"}}}
	for _, host := range []string{"", "example.com", "a-b.example.com", "192.0.2.1", "2001:db8::1", "bad/host", "https://example.com", "a:443", "bad host", "-bad.example", "a..b"} {
		layout.Connections[0].TargetHost = host
		expected := host == "" || desiredstate.ValidateSettings("forward", desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: host, Port: 1, Network: "tcp"}}, true) == nil
		if got := layout.validate() == nil; got != expected {
			t.Fatalf("host %q: valid=%v want %v", host, got, expected)
		}
	}
	layout.Connections[0].TargetHost = ""
	for _, port := range []int{0, -1, 1, 65535, 65536} {
		for _, target := range []bool{false, true} {
			layout.Connections[0].ListenPort = nil
			layout.Connections[0].TargetPort = nil
			if target {
				layout.Connections[0].TargetPort = &port
			} else {
				layout.Connections[0].ListenPort = &port
			}
			if got := layout.validate() == nil; got != (port >= 1 && port <= 65535) {
				t.Fatalf("port %d accepted=%v", port, got)
			}
		}
	}
	for _, kind := range []string{"tunnel", "proxy_chain"} {
		for _, field := range []string{"listen", "target", "host"} {
			c := CanvasConnection{ID: "f", Source: "a", Target: "b", Type: kind}
			switch field {
			case "listen":
				c.ListenPort = canvasTestPort(80)
			case "target":
				c.TargetPort = canvasTestPort(80)
			case "host":
				c.TargetHost = "example.com"
			}
			layout.Connections[0] = c
			if layout.validate() == nil {
				t.Fatalf("%s accepted %s", kind, field)
			}
		}
	}
}
func TestCanvasOldForwardPlanByteIdentical(t *testing.T) {
	p := newPublishPanel(t)
	enableCanvasForward(t, p)
	layout := forwardCanvasLayout(p)
	layout.Instances[0].ListenPort = canvasTestPort(12345)
	layout.Instances[1].TargetPort = canvasTestPort(443)
	c := &layout.Connections[0]
	c.ListenPort = nil
	c.TargetPort = nil
	c.TargetHost = ""
	scheme := CanvasScheme{Name: "old", Execution: "server", Layout: layout}
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	n, err := p.service.Nodes.Get(p.ctx, p.nodeB)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("f:" + p.nodeA + ":" + p.nodeB))
	// Frozen pre-U5a construction, including digest input and all plan input fields.
	old := []InboundInput{{Name: fmt.Sprintf("[old] forward %x%s", digest[:8], canvasInboundSuffix), NodeID: p.nodeA, Protocol: "forward", Listen: "0.0.0.0", Port: 12345, Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: n.PublicIP, Port: 443, Network: "tcp"}}}}
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(plan.forwards)
	if string(before) != string(after) {
		t.Fatalf("old plan changed\n%s\n%s", before, after)
	}
	layout.Connections[0].ListenPort = canvasTestPort(23456)
	layout.Connections[0].TargetPort = canvasTestPort(9443)
	layout.Connections[0].TargetHost = "custom.example.com"
	plan, err = p.service.canvasBuildPlan(p.ctx, CanvasScheme{Name: "old", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if plan.forwards[0].Port != 23456 || plan.forwards[0].Settings.Forward.Port != 9443 || plan.forwards[0].Settings.Forward.Host != "custom.example.com" || plan.forwards[0].Name != old[0].Name {
		t.Fatal(plan.forwards)
	}
}
func TestCanvasPlanDistinctForwardsConflictsAndPublish(t *testing.T) {
	p := newPublishPanel(t)
	enableCanvasForward(t, p)
	layout := forwardCanvasLayout(p)
	second := layout.Connections[0]
	second.ID = "f2"
	second.ListenPort = canvasTestPort(12346)
	layout.Connections = append(layout.Connections, second)
	scheme := savePlanScheme(t, p, layout)
	result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !result.OK || len(result.Servers) != 1 || len(result.Servers[0].Creates) != 2 {
		t.Fatalf("plan %+v %v", result, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	inbounds, _, _ := p.publishedObjects(t, scheme.Name)
	for _, object := range result.Servers[0].Creates {
		found := false
		for _, in := range inbounds {
			if object.Name == in.Name && object.Port == in.Port && object.Protocol == in.Protocol {
				found = true
			}
		}
		if !found {
			t.Fatalf("plan object missing from publication: %+v", object)
		}
	}
	again, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !again.OK || len(again.Servers[0].Creates) != 0 || len(again.Servers[0].Updates) != 2 {
		t.Fatal(again, err)
	}
	duplicate := layout
	duplicate.Connections = append([]CanvasConnection{}, layout.Connections...)
	duplicate.Connections[1].ListenPort = canvasTestPort(12345)
	conflict, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &duplicate)
	if err != nil || conflict.OK || len(conflict.Conflicts) < 2 || conflict.Connections[0].Status != "error" || conflict.Connections[1].Status != "error" {
		t.Fatal(conflict, err)
	}
	built, err := p.service.canvasBuildPlan(p.ctx, CanvasScheme{Name: scheme.Name, Execution: "server", Layout: duplicate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.canvasApplyPlan(p.ctx, built); err == nil {
		t.Fatal("publish accepted duplicate")
	}
	_, err = p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Owner inbound", NodeID: p.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 12347, Enabled: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	duplicate.Connections[1].ListenPort = canvasTestPort(12347)
	conflict, err = p.service.PlanCanvasScheme(p.ctx, scheme.ID, &duplicate)
	if err != nil || conflict.OK || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].With != "Owner inbound" || conflict.Conflicts[0].ConnectionID != "f2" {
		t.Fatal(conflict, err)
	}
	built, err = p.service.canvasBuildPlan(p.ctx, CanvasScheme{Name: scheme.Name, Execution: "server", Layout: duplicate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.canvasApplyPlan(p.ctx, built); err == nil {
		t.Fatal("publish accepted occupied port")
	}
	empty := CanvasLayout{}
	removed, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &empty)
	if err != nil || !removed.OK || len(removed.Servers[0].Removes) != 2 {
		t.Fatal(removed, err)
	}
}
func TestCanvasPlanEndpointAuthOptionalBodyAndNoWrites(t *testing.T) {
	p := newCanvasPanel(t)
	owner, csrf := p.session("Owner")
	viewer, vcsrf := p.session("Viewer")
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "a", Ref: "server:" + p.nodeA, Role: "entry"}, {InstanceID: "b", Ref: "server:" + p.nodeB, Role: "exit"}}, Connections: []CanvasConnection{{ID: "ab", Source: "a", Target: "b", Type: "tunnel", SupportTCP: true}}}
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "endpoint", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/canvas-schemes/" + scheme.ID + "/plan"
	for _, response := range []int{p.call("POST", path, nil, viewer, vcsrf).Code, p.call("POST", path, nil, owner, "").Code, p.call("POST", path, nil, nil, "").Code} {
		if response != 403 && response != 401 {
			t.Fatal(response)
		}
	}
	before := map[string]int{}
	for _, table := range []string{"inbounds", "outbounds", "proxy_chains", "relay_credentials", "config_revisions", "deployment_batches", "admin_operations", "canvas_schemes"} {
		var count int
		if err := p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		before[table] = count
	}
	unsaved := layout
	unsaved.Connections = append([]CanvasConnection{}, layout.Connections...)
	unsaved.Connections[0].TunnelPort = canvasTestPort(9443)
	for _, body := range []any{nil, map[string]any{}, map[string]any{"layout": unsaved}} {
		response := p.call("POST", path, body, owner, csrf)
		var result CanvasPlanResult
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.OK {
			t.Fatalf("%d %s", response.Code, response.Body.String())
		}
	}
	for table, count := range before {
		var after int
		if err := p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&after); err != nil || after != count {
			t.Fatalf("plan wrote %s: %d -> %d, %v", table, count, after, err)
		}
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil || !reflect.DeepEqual(stored, scheme) || len(p.audit.actions) != 0 {
		t.Fatal("scheme or audit changed", err)
	}
	for _, body := range []string{`{"layout":{"extra":true}}`, `{"extra":true}`, `{} {}`, `{`} {
		if r := p.call("POST", path, body, owner, csrf); r.Code != 400 {
			t.Fatal(r.Code, body)
		}
	}
	if r := p.call("POST", "/api/v1/canvas-schemes/missing/plan", nil, owner, csrf); r.Code != 404 {
		t.Fatal(r.Code)
	}
}
func TestCanvasPlanConnectionErrorMapping(t *testing.T) {
	for _, name := range []string{"gost", "brook", "nftables", "missing-ports", "import", "cycle", "nonlinear", "missing-node"} {
		t.Run(name, func(t *testing.T) {
			p := newPublishPanel(t)
			enableCanvasForward(t, p)
			layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
			scheme := savePlanScheme(t, p, layout)
			expected := layout.Connections[0].ID
			switch name {
			case "gost", "brook", "nftables":
				layout.Connections[0].TunnelProtocol = name
			case "missing-ports":
				layout.Connections[0].Type = "port_forward"
			case "import":
				layout.Instances[0].Ref = "import:external"
			case "missing-node":
				layout.Instances[0].Ref = "server:missing"
			case "cycle":
				layout.Connections = append(layout.Connections, canvasTestHop("i-c", "i-a", 10443))
			case "nonlinear":
				layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "i-d", Ref: "server:" + p.nodeC, Role: "exit"})
				layout.Connections = append(layout.Connections, canvasTestHop("i-b", "i-d", 10443))
			}
			result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &layout)
			if err != nil || result.OK {
				t.Fatal(result, err)
			}
			found := false
			for _, c := range result.Connections {
				if c.ID == expected && c.Status == "error" && c.Message != "" {
					found = true
				}
			}
			if !found {
				t.Fatal(result)
			}
			if name == "gost" && !strings.Contains(result.Connections[0].Message, "暂不支持") {
				t.Fatal(result)
			}
			if name == "gost" && result.Connections[1].Status != "ok" {
				t.Fatal("unrelated valid row hidden", result)
			}
		})
	}
}
func TestCanvasTunnelProxyChainEquivalent(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme := CanvasScheme{Name: "alias", Execution: "server", Layout: layout}
	tunnel, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	scheme.Layout.Connections[0].Type = "proxy_chain"
	chain, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil || !reflect.DeepEqual(tunnel, chain) {
		t.Fatal(tunnel, chain, err)
	}
	client := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "a", Ref: "import:a", Role: "entry"}, {InstanceID: "b", Ref: "import:b", Role: "exit"}}, Connections: []CanvasConnection{{ID: "ab", Source: "a", Target: "b", Type: "tunnel"}}}
	if err := ValidateClientLayout(client); err != nil {
		t.Fatal(err)
	}
	client.Connections[0].Type = "proxy_chain"
	if err := ValidateClientLayout(client); err != nil {
		t.Fatal(err)
	}
	scheme = savePlanScheme(t, p, layout)
	if _, err := p.database.Exec("UPDATE canvas_schemes SET execution='client',layout_json=json_set(layout_json,'$.connections',json((SELECT json_group_array(json_remove(value,'$.tunnelId')) FROM json_each(canvas_schemes.layout_json,'$.connections')))) WHERE id=?", scheme.ID); err != nil {
		t.Fatal(err)
	}
	result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || result.OK || !strings.Contains(result.Message, "客户端方案") {
		t.Fatal(result, err)
	}
}

func TestCanvasPlanRelayDiffMatchesPublishedObjects(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
	scheme := savePlanScheme(t, p, layout)
	planned, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !planned.OK {
		t.Fatal(planned, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	ins, outs, chains := p.publishedObjects(t, scheme.Name)
	names := map[string]bool{}
	for _, in := range ins {
		names[in.Name] = true
	}
	for _, out := range outs {
		names[out.Name] = true
	}
	for _, chain := range chains {
		names[chain.Name] = true
	}
	creates := 0
	for _, server := range planned.Servers {
		for _, object := range server.Creates {
			creates++
			if !names[object.Name] {
				t.Fatalf("planned object missing: %+v", object)
			}
		}
	}
	if creates != 5 {
		t.Fatal(planned)
	}
	again, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !again.OK {
		t.Fatal(again, err)
	}
	updates := 0
	for _, server := range again.Servers {
		if len(server.Creates) != 0 || len(server.Removes) != 0 {
			t.Fatal(again)
		}
		for _, object := range server.Updates {
			updates++
			if object.Name == p.inbound.Name {
				t.Fatal("unchanged entry route reported as update")
			}
		}
	}
	if updates != 5 {
		t.Fatal(again)
	}
	// Entry-only schemes still publish a snapshot despite no changed objects.
	entryOnly := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "in", Ref: "inbound:" + p.inbound.ID, Role: "entry"}}}
	isolated, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "isolated", Execution: "server", Layout: entryOnly})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.service.PlanCanvasScheme(p.ctx, isolated.ID, nil)
	if err != nil || !result.OK || len(result.Servers) != 1 {
		t.Fatal(result, err)
	}
}

// Listener ports are reserved by number on a node, exactly as the Agent's
// DesiredState validation does: a UDP listener cannot take a port an enabled TCP
// inbound of the same node already listens on, and a shared port between two
// listeners of the plan conflicts too.
func TestCanvasPlanReservesListenerPortNumber(t *testing.T) {
	p := newPublishPanel(t)
	enableCanvasForwardFor(t, p, p.nodeA)
	if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "TCP owner", NodeID: p.nodeA, Protocol: "socks5", Listen: "0.0.0.0", Port: 12348, Enabled: true, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	layout := forwardCanvasLayout(p)
	layout.Connections[0].SupportTCP = false
	layout.Connections[0].SupportUDP = true
	layout.Connections[0].ListenPort = canvasTestPort(12348)
	scheme := savePlanScheme(t, p, layout)
	conflict, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || conflict.OK || len(conflict.Conflicts) != 1 {
		t.Fatalf("UDP listener on the TCP port: %+v %v", conflict, err)
	}
	if c := conflict.Conflicts[0]; c.With != "TCP owner" || c.Reason != "listener" || c.Port != 12348 || c.ConnectionID != layout.Connections[0].ID {
		t.Fatalf("conflict %+v", c)
	}
	if conflict.Connections[0].Status != "error" || conflict.Connections[0].Message == "" {
		t.Fatalf("connection row %+v", conflict.Connections[0])
	}
	// The Agent validates enabled listeners only, so a disabled inbound on that
	// port reserves nothing.
	if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "off", NodeID: p.nodeA, Protocol: "forward", Listen: "0.0.0.0", Port: 12349, Enabled: false, DefaultOutbound: "direct",
		Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "1.1.1.1", Port: 53, Network: "udp"}}}); err != nil {
		t.Fatal(err)
	}
	layout.Connections[0].ListenPort = canvasTestPort(12349)
	free, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &layout)
	if err != nil || !free.OK || len(free.Conflicts) != 0 {
		t.Fatalf("disabled inbound reserved a port: %+v %v", free, err)
	}
}

// A drawing the canvas accepts must also render a DesiredState the Agent
// accepts: the planner and desiredstate.Validate apply the same numeric rule, so
// a UDP listener blocks the relay number and the accepted drawing publishes.
func TestCanvasPlanRelayPortMatchesDesiredState(t *testing.T) {
	p := newPublishPanel(t)
	enableCanvasForwardFor(t, p, p.nodeB)
	occupant, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "UDP forward", NodeID: p.nodeB, Protocol: "forward", Listen: "0.0.0.0", Port: 8443, Enabled: true, DefaultOutbound: "direct",
		Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "1.1.1.1", Port: 53, Network: "udp"}}})
	if err != nil {
		t.Fatal(err)
	}
	// The Trojan relay the canvas generates on nodeB listens on TCP 8443.
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	scheme := savePlanScheme(t, p, layout)
	result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || result.OK || len(result.Conflicts) != 1 || result.Conflicts[0].With != "UDP forward" {
		t.Fatalf("relay on the UDP number: %+v %v", result, err)
	}
	// Saving that listener without the planner is refused by the same rule.
	_, err = p.service.SaveInbound(p.ctx, "", InboundInput{Name: "relay", NodeID: p.nodeB, Protocol: "trojan", Listen: "0.0.0.0", Port: 8443, Enabled: true, InternalRelay: true, DefaultOutbound: "direct"})
	if err == nil || !strings.Contains(err.Error(), "inbound port") {
		t.Fatalf("second enabled listener on 8443: %v", err)
	}
	// With the number free the same drawing is accepted and publishes a node state
	// that passes the Agent's own desiredstate validation.
	if err := p.service.Repository.DeleteInbound(p.ctx, occupant.ID); err != nil {
		t.Fatal(err)
	}
	if accepted, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil); err != nil || !accepted.OK {
		t.Fatalf("free port rejected: %+v %v", accepted, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.Snapshot(p.ctx, p.nodeB, p.users); err != nil {
		t.Fatalf("DesiredState rejected the published relay: %v", err)
	}
}
