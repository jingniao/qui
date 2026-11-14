package proxy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestCanvasEntryRouteSummaryManagedTunnelAndRule(t *testing.T) {
	for _, kind := range []string{"tunnel", "rule"} {
		t.Run(kind, func(t *testing.T) {
			p := newPublishPanel(t)
			tunnelTestRange(t, p.canvasPanel, p.nodeA, p.nodeB)
			tunnel, err := p.service.SaveTunnel(p.ctx, "", tunnelTestInput(p.nodeA, p.nodeB))
			if err != nil {
				t.Fatal(err)
			}
			ownerID := tunnel.ID
			if kind == "rule" {
				if _, err := p.database.Exec(`INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_host,target_port,subscription_visible,enabled,status,created_at,updated_at)
 VALUES('summary-rule','Summary rule',?,20009,'manual','tcp',?,'manual','192.0.2.1',443,0,1,'draft','now','now')`, p.nodeA, tunnel.ID); err != nil {
					t.Fatal(err)
				}
				ownerID = "summary-rule"
			}
			if _, err := p.database.Exec("INSERT INTO tunnel_objects VALUES('inbound',?,?,?)", p.inbound.ID, kind, ownerID); err != nil {
				t.Fatal(err)
			}
			entry, err := p.service.Repository.Inbound(p.ctx, p.inbound.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			items := []Inbound{entry}
			if err := p.service.Repository.ProjectInboundRoutes(p.ctx, items); err != nil {
				t.Fatal(err)
			}
			got := items[0].RouteSummary
			if got.Type != "tunnel" || got.ExitNodeID != p.nodeB || !reflect.DeepEqual(got.Path, []string{"Node A", "Node B"}) {
				t.Fatalf("managed route: %+v", got)
			}
		})
	}
}

func TestCanvasEntryRouteSummaryKinds(t *testing.T) {
	p := newPublishPanel(t)
	items := []Inbound{}
	for _, route := range []string{"", "direct", "block", "ordinary-outbound", "chain:chain-id"} {
		entry := p.inbound
		entry.DefaultOutbound = route
		items = append(items, entry)
	}
	if err := p.service.Repository.ProjectInboundRoutes(p.ctx, items); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"direct", "direct", "direct", "custom", "custom"} {
		if items[i].RouteSummary.Type != want || !reflect.DeepEqual(items[i].RouteSummary.Path, []string{"Node A"}) {
			t.Fatalf("route %q: %+v", items[i].DefaultOutbound, items[i].RouteSummary)
		}
	}
	if p.inbound.RouteSummary != nil {
		t.Fatal("projection changed original inbound")
	}
}

func TestCanvasEntryRouteSummaryOutboundProtocols(t *testing.T) {
	p := newPublishPanel(t)
	for _, protocol := range []string{"direct", "block", "socks5"} {
		outbound, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: protocol, NodeID: p.nodeA, Protocol: protocol, Enabled: true, Address: "192.0.2.20", Port: 1080, SupportTCP: true, FailurePolicy: "Block"})
		if err != nil {
			t.Fatal(err)
		}
		entry := p.inbound
		entry.DefaultOutbound = outbound.ID
		items := []Inbound{entry}
		if err := p.service.Repository.ProjectInboundRoutes(p.ctx, items); err != nil {
			t.Fatal(err)
		}
		want := "direct"
		if protocol == "socks5" {
			want = "custom"
		}
		if items[0].RouteSummary.Type != want {
			t.Fatalf("%s outbound: %+v", protocol, items[0].RouteSummary)
		}
	}
}

func TestCanvasEntryRouteSummaryRelayAndDetached(t *testing.T) {
	p := newPublishPanel(t)
	layout := CanvasLayout{Instances: []CanvasInstance{
		{InstanceID: "entry", Ref: "inbound:" + p.inbound.ID, Role: "entry"},
		{InstanceID: "middle", Ref: "server:" + p.nodeB, Role: "relay"},
		{InstanceID: "exit", Ref: "server:" + p.nodeC, Role: "exit"},
	}, Connections: []CanvasConnection{{ID: "ab", Source: "entry", Target: "middle", Type: "proxy_chain"}, {ID: "bc", Source: "middle", Target: "exit", Type: "proxy_chain"}}}
	scheme, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "Three hops", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: "binding", SchemeID: &scheme.ID, InstanceID: "entry", InboundID: p.inbound.ID, Mode: "reuse", OriginalDefaultOutbound: "direct", State: "applied"}); err != nil {
		t.Fatal(err)
	}
	items := []Inbound{p.inbound}
	if err := p.service.Repository.ProjectInboundRoutes(p.ctx, items); err != nil {
		t.Fatal(err)
	}
	got := items[0].RouteSummary
	if got.Type != "relay" || got.SchemeID != scheme.ID || got.SchemeName != "Three hops" || got.BindingMode != "reuse" || got.ExitNodeID != p.nodeC || !reflect.DeepEqual(got.Path, []string{"Node A", "Node B", "Node C"}) {
		t.Fatalf("summary: %+v", got)
	}
	if _, err := p.database.ExecContext(p.ctx, "UPDATE canvas_entry_bindings SET scheme_id=NULL,state='detached' WHERE id='binding'"); err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.ProjectInboundRoutes(p.ctx, items); err != nil {
		t.Fatal(err)
	}
	got = items[0].RouteSummary
	if got.Type != "relay" || got.SchemeID != "" || got.SchemeName != "" || got.ExitNodeID != "" || !reflect.DeepEqual(got.Path, []string{"Node A"}) {
		t.Fatalf("detached: %+v", got)
	}
}

func TestCanvasEntryRouteSummaryBatchQueries(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "canvas_entry_projection.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	queries := 0
	ast.Inspect(file, func(n ast.Node) bool {
		if loop, ok := n.(*ast.RangeStmt); ok {
			ast.Inspect(loop.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "QueryContext" {
					t.Error("batch query is inside a per-entry loop")
				}
				return true
			})
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "QueryRowContext" {
			t.Error("per-entry query is forbidden")
		}
		if sel.Sel.Name == "QueryContext" {
			queries++
		}
		return true
	})
	if queries != 2 {
		t.Fatalf("want one reference query and one binding/scheme query, got %d", queries)
	}
}

func TestCanvasEntryRoutePathIgnoresUnrelatedBranch(t *testing.T) {
	layout := CanvasLayout{
		Instances:   []CanvasInstance{{InstanceID: "entry", Ref: "server:a"}, {InstanceID: "exit", Ref: "server:b"}, {InstanceID: "other", Ref: "server:c"}},
		Connections: []CanvasConnection{{Source: "entry", Target: "exit"}, {Source: "other", Target: "entry"}, {Source: "other", Target: "exit"}},
	}
	refs := map[string]canvasRouteRef{"server:a": {nodeID: "a", name: "A"}, "server:b": {nodeID: "b", name: "B"}, "server:c": {nodeID: "c", name: "C"}}
	path, exit, err := canvasRoutePath(layout, "entry", "a", refs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(path, []string{"A", "B"}) || exit != "b" {
		t.Fatalf("path=%v exit=%q", path, exit)
	}
}

func TestCanvasEntryRoutePathRejectsMissingReference(t *testing.T) {
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "server:a"}, {InstanceID: "exit", Ref: "inbound:deleted"}}, Connections: []CanvasConnection{{Source: "entry", Target: "exit"}}}
	refs := map[string]canvasRouteRef{"server:a": {nodeID: "a", name: "A"}}
	if _, _, err := canvasRoutePath(layout, "entry", "a", refs); err == nil {
		t.Fatal("missing exit reference produced a route")
	}
	if _, _, err := canvasRoutePath(layout, "entry", "missing", refs); err == nil {
		t.Fatal("missing entry server produced a route")
	}
}

func TestCanvasEntryRoutePathExternalOutbound(t *testing.T) {
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "server:a"}, {InstanceID: "exit", Ref: "outbound:external"}}, Connections: []CanvasConnection{{Source: "entry", Target: "exit"}}}
	refs := map[string]canvasRouteRef{"server:a": {nodeID: "a", name: "A"}, "outbound:external": {nodeID: "a", name: "A", protocol: "socks5"}}
	path, exit, err := canvasRoutePath(layout, "entry", "a", refs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(path, []string{"A"}) || exit != "a" {
		t.Fatalf("path=%v exit=%q", path, exit)
	}
}

// A drawing whose route cannot be resolved marks only its own entry; the list
// still answers.
func TestCanvasEntryRouteSummaryBrokenDrawingDoesNotFailList(t *testing.T) {
	p := newPublishPanel(t)
	layout := CanvasLayout{Instances: []CanvasInstance{
		{InstanceID: "entry", Ref: "inbound:" + p.inbound.ID, Role: "entry"},
		{InstanceID: "exit", Ref: "server:" + p.nodeB, Role: "exit"},
	}, Connections: []CanvasConnection{{ID: "ab", Source: "entry", Target: "exit", Type: "proxy_chain"}}}
	scheme, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "Broken", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: "binding", SchemeID: &scheme.ID, InstanceID: "entry", InboundID: p.inbound.ID, Mode: "reuse", OriginalDefaultOutbound: "direct", State: "applied"}); err != nil {
		t.Fatal(err)
	}
	// The exit server's reference disappears from the drawing's point of view.
	if _, err := p.database.ExecContext(p.ctx, "UPDATE canvas_schemes SET layout_json=replace(layout_json,?,?) WHERE id=?", "server:"+p.nodeB, "server:missing-node", scheme.ID); err != nil {
		t.Fatal(err)
	}
	items := []Inbound{p.inbound}
	if err := p.service.Repository.ProjectInboundRoutes(p.ctx, items); err != nil {
		t.Fatalf("a broken drawing failed the list: %v", err)
	}
	got := items[0].RouteSummary
	if got.Type != "relay" || !got.PathIncomplete || !reflect.DeepEqual(got.Path, []string{"Node A"}) {
		t.Fatalf("summary: %+v", got)
	}
}
