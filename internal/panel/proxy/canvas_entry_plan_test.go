package proxy

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func entryPlanLayout(p *publishPanel, binding *CanvasEntryBinding) CanvasLayout {
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443)})
	layout.Instances[0].EntryBinding = binding
	layout.Instances[1].Role = "exit"
	return layout
}

func TestCanvasEntryPlanCreatePreview(t *testing.T) {
	p := newPublishPanel(t)
	if _, err := p.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,20000,20002)", p.nodeA); err != nil {
		t.Fatal(err)
	}
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"})
	scheme := savePlanScheme(t, p, layout)
	p.database.SetMaxOpenConns(1)
	var before int
	if err := p.database.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !result.OK {
		t.Fatalf("plan %+v, %v", result, err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Entries []struct {
			InstanceID string `json:"instanceId"`
			Mode       string `json:"mode"`
			NodeID     string `json:"nodeId"`
			NodeName   string `json:"nodeName"`
			Protocol   string `json:"protocol"`
			Port       int    `json:"port"`
			Name       string `json:"name"`
			Route      string `json:"route"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Entries) != 1 {
		t.Fatalf("wanted one pending user entry, got %s", raw)
	}
	e := response.Entries[0]
	if e.InstanceID != "i-a" || e.Mode != "create" || e.NodeID != p.nodeA || e.NodeName != "Node A" || e.Protocol != "socks5" || e.Port != 20000 || e.Name != "Node A → Node B" || e.Route == "" {
		t.Fatalf("pending entry %+v", e)
	}
	var after int
	if err := p.database.QueryRow("SELECT total_changes()").Scan(&after); err != nil || after != before {
		t.Fatalf("preview wrote database: total_changes %d -> %d, %v", before, after, err)
	}
}

func entryPlanRange(t *testing.T, p *publishPanel, nodeID string, first, last int) {
	t.Helper()
	if _, err := p.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET port_range_start=excluded.port_range_start,port_range_end=excluded.port_range_end", nodeID, first, last); err != nil {
		t.Fatal(err)
	}
}

func entryPlanResult(t *testing.T, p *publishPanel, layout CanvasLayout) CanvasPlanResult {
	t.Helper()
	scheme := savePlanScheme(t, p, layout)
	result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !result.OK {
		t.Fatalf("plan %+v, %v", result, err)
	}
	return result
}

func TestCanvasEntryPlanCreateProtocolCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name     string
		protocol string
		core     string
		caps     *pb.Capabilities
		wantOK   bool
	}{
		{name: "primary supported", protocol: "socks5", wantOK: true},
		{name: "not advertised", protocol: "vless"},
		{name: "core unsupported", protocol: "hysteria2"},
		{name: "unknown protocol without protocol report", protocol: "unknown", caps: &pb.Capabilities{CoreName: "xray"}},
		{name: "unreported selected core", protocol: "socks5", core: "sing-box"},
		{name: "selected enabled core", protocol: "hysteria2", core: "sing-box", caps: &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray"}, {Name: "sing-box", Protocols: []*pb.ProtocolCapability{{Protocol: "hysteria2", Inbound: true, Udp: true}}}}}, wantOK: true},
		{name: "selected core absent from enabled report", protocol: "hysteria2", core: "sing-box", caps: &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPublishPanel(t)
			if tc.caps != nil {
				raw, err := json.Marshal(tc.caps)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), p.nodeA); err != nil {
					t.Fatal(err)
				}
			}
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: tc.protocol, Core: tc.core, PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
			result, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "capabilities", Execution: "server", Layout: layout})
			if err != nil || result.OK != tc.wantOK {
				t.Fatalf("plan %+v, %v; want ok=%v", result, err, tc.wantOK)
			}
			if !tc.wantOK && strings.IndexFunc(result.Message, func(r rune) bool { return unicode.Is(unicode.Han, r) }) < 0 {
				t.Fatalf("expected Chinese protocol error: %+v", result)
			}
		})
	}
}

func TestCanvasEntryPlanBindingPublication(t *testing.T) {
	for _, mode := range []string{"create", "reuse"} {
		t.Run(mode, func(t *testing.T) {
			p := newPublishPanel(t)
			binding := &CanvasEntryBinding{Mode: mode, Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
			if mode == "reuse" {
				binding.InboundID = p.inbound.ID
			}
			scheme := savePlanScheme(t, p, entryPlanLayout(p, binding))
			result, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Entries) != 1 || result.Entries[0].Mode != mode || result.Entries[0].InboundID == "" || result.Entries[0].Route == "" || result.Entries[0].Route == "direct" {
				t.Fatalf("published entry missing: %+v", result)
			}
			repeated, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
			if err != nil || len(repeated.Entries) != 1 || repeated.Entries[0].InboundID != result.Entries[0].InboundID || repeated.Entries[0].Route != result.Entries[0].Route {
				t.Fatalf("repeated publication %+v, %v", repeated, err)
			}
		})
	}
}

func TestCanvasEntryPlanAutoPortSharesListenerReservations(t *testing.T) {
	p := newPublishPanel(t)
	entryPlanRange(t, p, p.nodeB, 20000, 20002)
	if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "disabled reservation", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 20000, Enabled: false, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	first := canvasTestHop("i-a", "i-b", 0)
	first.TunnelPort = nil
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{first, canvasTestHop("i-b", "i-c", 9443)})
	layout.Instances[1].Role = "entry"
	layout.Instances[1].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"}
	result := entryPlanResult(t, p, layout)
	if len(result.Entries) != 1 || result.Entries[0].NodeID != p.nodeB || result.Entries[0].Port != 20002 {
		t.Fatalf("entry reused reserved listener: %+v", result)
	}
	found := false
	for _, server := range result.Servers {
		if server.NodeID == p.nodeB {
			for _, object := range server.Creates {
				if object.Kind == "inbound" && object.Protocol == "trojan" {
					found = true
					if object.Port != 20001 {
						t.Fatalf("relay allocation changed: %+v", object)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("internal relay listener missing")
	}
}

func TestCanvasEntryPlanAutoPortNoRangeOrExhausted(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(map[bool]string{false: "no range", true: "exhausted"}[exhausted], func(t *testing.T) {
			p := newPublishPanel(t)
			if exhausted {
				entryPlanRange(t, p, p.nodeA, 1080, 1080)
			}
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"})
			_, err := p.service.canvasBuildPlan(p.ctx, CanvasScheme{Name: "range", Execution: "server", Layout: layout})
			var failure *tunnelError
			if !errors.As(err, &failure) || failure.Code != "no_free_port" {
				t.Fatalf("want no_free_port without default port fallback, got %v", err)
			}
		})
	}
}

func TestCanvasEntryPlanManualPortValidation(t *testing.T) {
	for _, port := range []int{0, 65536, 1080} {
		t.Run(strconv.Itoa(port), func(t *testing.T) {
			p := newPublishPanel(t)
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(port), GroupMode: "none"})
			result, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "manual", Execution: "server", Layout: layout})
			if err != nil || result.OK {
				t.Fatalf("invalid or occupied port %d accepted: %+v, %v", port, result, err)
			}
		})
	}
	for _, port := range []int{1, 65535} {
		p := newPublishPanel(t)
		result := entryPlanResult(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(port), GroupMode: "none"}))
		if len(result.Entries) != 1 || result.Entries[0].Port != port {
			t.Fatalf("valid boundary port %d: %+v", port, result)
		}
	}
}

func TestCanvasEntryPlanManualPortConflictsWithinPlan(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 20000), canvasTestHop("i-b", "i-c", 9443)})
	layout.Instances[1].Role = "entry"
	layout.Instances[1].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
	result, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "manual conflict", Execution: "server", Layout: layout})
	if err != nil || result.OK {
		t.Fatalf("same-plan listener conflict accepted: %+v, %v", result, err)
	}
}

func TestCanvasEntryPlanManualPortReservedBeforeAutoRelay(t *testing.T) {
	p := newPublishPanel(t)
	entryPlanRange(t, p, p.nodeB, 20000, 20001)
	first := canvasTestHop("i-a", "i-b", 0)
	first.TunnelPort = nil
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{first, canvasTestHop("i-b", "i-c", 9443)})
	layout.Instances[1].Role = "entry"
	layout.Instances[1].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
	result := entryPlanResult(t, p, layout)
	if len(result.Entries) != 1 || result.Entries[0].Port != 20000 {
		t.Fatalf("manual entry %+v", result.Entries)
	}
	found := false
	for _, server := range result.Servers {
		if server.NodeID == p.nodeB {
			for _, object := range server.Creates {
				if object.Protocol == "trojan" && object.Port == 20001 {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("auto relay did not avoid manual entry: %+v", result)
	}
}

func TestCanvasEntryPlanCreateNameAndRoute(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(map[bool]string{false: "one hop", true: "two hops"}[multi], func(t *testing.T) {
			p := newPublishPanel(t)
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
			wantName, wantRoute := "Node A → Node B", "[plan] Node A → Node B (trojan:8443) 中转出站"
			if multi {
				layout.Instances[1].Role = "relay"
				layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "i-c", Ref: "server:" + p.nodeC, Role: "exit"})
				layout.Connections = append(layout.Connections, canvasTestHop("i-b", "i-c", 9443))
				wantName, wantRoute = "Node A → Node B → Node C", "[plan] Node A → Node B → Node C 链路 (trojan:8443, trojan:9443)"
			}
			result := entryPlanResult(t, p, layout)
			if len(result.Entries) != 1 || result.Entries[0].Name != wantName || result.Entries[0].Route != wantRoute {
				t.Fatalf("entry name/route %+v", result.Entries)
			}
		})
	}
}

func TestCanvasEntryPlanExistingRouteIDs(t *testing.T) {
	for _, multi := range []bool{false, true} {
		t.Run(map[bool]string{false: "outbound", true: "chain"}[multi], func(t *testing.T) {
			p := newPublishPanel(t)
			layout := entryPlanLayout(p, nil)
			if multi {
				layout.Instances[1].Role = "relay"
				layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "i-c", Ref: "server:" + p.nodeC, Role: "exit"})
				layout.Connections = append(layout.Connections, canvasTestHop("i-b", "i-c", 9443))
			}
			scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "existing route", Execution: "server", Layout: layout})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			_, outbounds, chains := p.publishedObjects(t, scheme.Name)
			wantRoute := ""
			if multi {
				if len(chains) != 1 {
					t.Fatalf("chains %+v", chains)
				}
				wantRoute = "chain:" + chains[0].ID
			} else {
				if len(outbounds) != 1 {
					t.Fatalf("outbounds %+v", outbounds)
				}
				wantRoute = outbounds[0].ID
			}
			layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
			result, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, &layout)
			if err != nil || !result.OK || len(result.Entries) != 1 || result.Entries[0].Route != wantRoute {
				t.Fatalf("existing route preview %+v, %v; want %s", result, err, wantRoute)
			}
		})
	}
}

func entryPlanGroup(t *testing.T, p *publishPanel, id, purpose string, inboundIDs ...string) {
	t.Helper()
	if _, err := p.database.Exec("INSERT INTO node_groups(id,name,purpose,created_at) VALUES(?,?,?,'now')", id, "Group "+id, purpose); err != nil {
		t.Fatal(err)
	}
	for _, inboundID := range inboundIDs {
		if _, err := p.database.Exec("INSERT INTO node_group_inbounds(group_id,inbound_id) VALUES(?,?)", id, inboundID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCanvasEntryPlanCreateGroups(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		ids, want []string
	}{
		{mode: "exit_groups", want: []string{"mixed", "subscription"}},
		{mode: "explicit", ids: []string{"selected"}, want: []string{"selected"}},
		{mode: "none", want: []string{}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			p := newPublishPanel(t)
			exit, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Exit", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			for _, purpose := range []string{"subscription", "mixed", "relay", "egress"} {
				entryPlanGroup(t, p, purpose, purpose, exit.ID)
			}
			entryPlanGroup(t, p, "selected", "subscription")
			entryPlanGroup(t, p, "unrelated", "subscription", p.inbound.ID)
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: tc.mode, GroupIDs: tc.ids})
			layout.Instances[1].Ref = "inbound:" + exit.ID
			result := entryPlanResult(t, p, layout)
			if len(result.Entries) != 1 {
				t.Fatalf("entries %+v", result.Entries)
			}
			got := []string{}
			for _, group := range result.Entries[0].Groups {
				if group.Name != "Group "+group.ID {
					t.Fatalf("group %+v", group)
				}
				got = append(got, group.ID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("groups %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("missing explicit group", func(t *testing.T) {
		p := newPublishPanel(t)
		layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "explicit", GroupIDs: []string{"missing"}})
		result, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "groups", Execution: "server", Layout: layout})
		if err != nil || result.OK {
			t.Fatalf("missing group accepted: %+v, %v", result, err)
		}
	})
}

func TestCanvasEntryPlanReuse(t *testing.T) {
	p := newPublishPanel(t)
	if _, err := p.database.Exec("UPDATE inbounds SET default_outbound='block' WHERE id=?", p.inbound.ID); err != nil {
		t.Fatal(err)
	}
	entryPlanGroup(t, p, "existing", "mixed", p.inbound.ID)
	entryPlanGroup(t, p, "unselected", "subscription")
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"})
	result := entryPlanResult(t, p, layout)
	if len(result.Entries) != 1 {
		t.Fatalf("entries %+v", result.Entries)
	}
	e := result.Entries[0]
	if e.InstanceID != "i-a" || e.Mode != "reuse" || e.NodeID != p.nodeA || e.InboundID != p.inbound.ID || e.Protocol != "socks5" || e.Port != 1080 || e.Name != "Entry" || e.PreviousRoute != "block" || e.Route != "[plan] Node A → Node B (trojan:8443) 中转出站" {
		t.Fatalf("reuse preview %+v", e)
	}
	if len(e.Groups) != 1 || e.Groups[0].ID != "existing" || e.Groups[0].Name != "Group existing" {
		t.Fatalf("reuse groups %+v", e.Groups)
	}
}

func TestCanvasEntryPlanReuseRejectsInvalidInbound(t *testing.T) {
	for _, kind := range []string{"internal relay", "other node", "deleted", "unsupported core"} {
		t.Run(kind, func(t *testing.T) {
			p := newPublishPanel(t)
			var err error
			switch kind {
			case "internal relay":
				_, err = p.database.Exec("UPDATE inbounds SET internal_relay=1 WHERE id=?", p.inbound.ID)
			case "other node":
				_, err = p.database.Exec("UPDATE inbounds SET node_id=? WHERE id=?", p.nodeB, p.inbound.ID)
			case "deleted":
				err = p.service.Repository.DeleteInbound(p.ctx, p.inbound.ID)
			case "unsupported core":
				_, err = p.database.Exec("UPDATE inbounds SET core='sing-box' WHERE id=?", p.inbound.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"})
			result, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "reuse", Execution: "server", Layout: layout})
			if err != nil || result.OK {
				t.Fatalf("invalid reuse accepted: %+v, %v", result, err)
			}
		})
	}
}

func TestCanvasEntryPlanDraftReadOnlyAndLegacyEquivalent(t *testing.T) {
	p := newPublishPanel(t)
	p.database.SetMaxOpenConns(1)
	layout := entryPlanLayout(p, nil)
	layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "old-entry", Ref: "inbound:" + p.inbound.ID, Role: "entry"})
	scheme := savePlanScheme(t, p, layout)
	before, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || !before.OK {
		t.Fatalf("stored plan %+v, %v", before, err)
	}
	var changes int
	if err := p.database.QueryRow("SELECT total_changes()").Scan(&changes); err != nil {
		t.Fatal(err)
	}
	after, err := p.service.PlanCanvas(p.ctx, scheme)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("draft changed legacy result\nbefore %+v\nafter %+v\n%v", before, after, err)
	}
	if len(after.Entries) != 0 {
		t.Fatalf("unbound legacy entry became managed: %+v", after.Entries)
	}
	unsaved := CanvasScheme{Name: "unsaved", Execution: "server", Layout: entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})}
	result, err := p.service.PlanCanvas(p.ctx, unsaved)
	if err != nil || !result.OK || len(result.Entries) != 1 {
		t.Fatalf("unsaved plan %+v, %v", result, err)
	}
	var next int
	if err := p.database.QueryRow("SELECT total_changes()").Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != changes {
		t.Fatalf("planning wrote database: total_changes %d -> %d", changes, next)
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil || !reflect.DeepEqual(stored, scheme) {
		t.Fatalf("planning changed stored scheme: %+v, %v", stored, err)
	}
}

func TestCanvasEntryPlanSharedPendingEntries(t *testing.T) {
	for _, withOwnEntry := range []bool{false, true} {
		t.Run(strconv.FormatBool(withOwnEntry), func(t *testing.T) {
			p := newPublishPanel(t)
			entryPlanRange(t, p, p.nodeA, 20000, 20002)
			layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
			layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"}
			other := savePlanScheme(t, p, layout)
			current := CanvasScheme{Name: "shared", Execution: "server", Layout: CanvasLayout{Instances: append([]CanvasInstance(nil), other.Layout.Instances[:2]...), Connections: append([]CanvasConnection(nil), other.Layout.Connections[:1]...)}}
			if !withOwnEntry {
				current.Layout.Instances[0].EntryBinding = nil
			}
			result, err := p.service.PlanCanvas(p.ctx, current)
			want := 0
			if withOwnEntry {
				want = 1
			}
			if err != nil || !result.OK || len(result.Entries) != want {
				t.Fatalf("shared entries %+v, %v; want %d", result, err, want)
			}
			for _, entry := range result.Entries {
				if entry.Port != 20000 || entry.NodeID != p.nodeA {
					t.Fatalf("other pending entry reserved a port: %+v", result.Entries)
				}
			}
			for _, server := range result.Servers {
				if server.NodeID == "" {
					t.Fatal("shared pending entry added an empty server")
				}
			}
		})
	}
}

func TestCanvasEntryPlanSharedReuseStaysWithOwningScheme(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{canvasTestHop("i-a", "i-b", 8443), canvasTestHop("i-b", "i-c", 9443)})
	layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}
	other := savePlanScheme(t, p, layout)
	current := CanvasScheme{Name: "shared", Execution: "server", Layout: CanvasLayout{Instances: append([]CanvasInstance(nil), other.Layout.Instances[:2]...), Connections: append([]CanvasConnection(nil), other.Layout.Connections[:1]...)}}
	current.Layout.Instances[0].EntryBinding = nil
	result, err := p.service.PlanCanvas(p.ctx, current)
	if err != nil || !result.OK || len(result.Entries) != 0 {
		t.Fatalf("other scheme's reuse entered preview: %+v, %v", result, err)
	}
	for _, server := range result.Servers {
		for _, object := range server.Updates {
			if object.Kind == "inbound" && object.Name == p.inbound.Name {
				t.Fatalf("other scheme's pending reuse changed routing: %+v", object)
			}
		}
	}
}

func TestCanvasEntryPlanSharedPendingManualPortNotReserved(t *testing.T) {
	p := newPublishPanel(t)
	entryPlanRange(t, p, p.nodeB, 20000, 20001)
	first := canvasTestHop("i-a", "i-b", 0)
	first.TunnelPort = nil
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB, C: p.nodeC}, "", []CanvasConnection{first, canvasTestHop("i-b", "i-c", 9443)})
	layout.Instances[1].Role = "entry"
	layout.Instances[1].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
	other := savePlanScheme(t, p, layout)
	current := CanvasScheme{Name: "shared", Execution: "server", Layout: CanvasLayout{Instances: append([]CanvasInstance(nil), other.Layout.Instances[:2]...), Connections: append([]CanvasConnection(nil), other.Layout.Connections[:1]...)}}
	current.Layout.Instances[1].EntryBinding = nil
	result, err := p.service.PlanCanvas(p.ctx, current)
	if err != nil || !result.OK || len(result.Entries) != 0 {
		t.Fatalf("shared preview %+v, %v", result, err)
	}
	for _, server := range result.Servers {
		if server.NodeID == p.nodeB {
			for _, object := range server.Creates {
				if object.Kind == "inbound" && object.Protocol == "trojan" && object.Port == 20000 {
					return
				}
			}
		}
	}
	t.Fatalf("other pending entry reserved the first relay port: %+v", result.Servers)
}

func TestCanvasEntryPlanHTTPEntries(t *testing.T) {
	p := newCanvasPanel(t)
	owner, csrf := p.session("Owner")
	caps, err := json.Marshal(&pb.Capabilities{CoreName: "xray"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(caps), p.nodeA); err != nil {
		t.Fatal(err)
	}
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "a", Ref: "server:" + p.nodeA, Role: "entry"}, {InstanceID: "b", Ref: "server:" + p.nodeB, Role: "exit"}}, Connections: []CanvasConnection{canvasTestHop("a", "b", 8443)}}
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "entry endpoint", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
	response := p.call("POST", "/api/v1/canvas-schemes/"+scheme.ID+"/plan", map[string]any{"layout": layout}, owner, csrf)
	var result CanvasPlanResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.OK || len(result.Entries) != 1 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	entry := result.Entries[0]
	if entry.NodeID != p.nodeA || entry.Mode != "create" || entry.Port != 20000 || entry.Name != "Node A → Node B" {
		t.Fatalf("HTTP entry %+v", entry)
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil || !reflect.DeepEqual(stored, scheme) || len(p.audit.actions) != 0 {
		t.Fatalf("planning changed scheme or audit: %+v, %v", stored, err)
	}
}
