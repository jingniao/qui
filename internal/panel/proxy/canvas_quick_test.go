package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
)

func quickCanvasFixture(t *testing.T) (*publishPanel, QuickCanvasInput) {
	t.Helper()
	p := newPublishPanel(t)
	p.service.EntryGroups = p.service.Nodes.(*node.Repository)
	for _, nodeID := range []string{p.nodeA, p.nodeB, p.nodeC} {
		entryPlanRange(t, p, nodeID, 20000, 20010)
	}
	exit, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "Exit", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	entryPlanGroup(t, p, "exit-group", "subscription", exit.ID)
	p.audit = &canvasAudit{}
	h := &Handler{Service: p.service, Users: p.users, Audit: p.audit, Auth: &auth.Handler{Service: &auth.Service{Repository: &auth.Repository{Database: p.database}, Settings: canvasSettings{}, SessionDuration: time.Hour}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	mux := http.NewServeMux()
	h.Register(mux)
	p.mux = mux
	return p, QuickCanvasInput{RequestID: "quick-request", EntryNodeID: p.nodeA, ExitInboundID: exit.ID, EntryProtocol: "socks5", EntryGroups: QuickCanvasEntryGroups{Mode: "exit_groups"}}
}

func quickCanvasChanges(t *testing.T, p *publishPanel) int {
	t.Helper()
	var count int
	if err := p.database.QueryRow("SELECT total_changes()").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestQuickCanvasPlanReadOnlyAndEntries(t *testing.T) {
	p, input := quickCanvasFixture(t)
	owner, csrf := p.session("Owner")
	p.database.SetMaxOpenConns(1)
	before := quickCanvasChanges(t, p)
	response := p.call("POST", "/api/v1/relay-schemes/quick/plan", input, owner, csrf)
	var result CanvasPlanResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.OK || len(result.Entries) != 1 || len(result.Servers) != 2 {
		t.Fatalf("plan: %d %s", response.Code, response.Body.String())
	}
	entry := result.Entries[0]
	if entry.NodeID != p.nodeA || entry.Protocol != "socks5" || entry.Mode != "create" || entry.Port != 20000 || entry.Name != "Node A → Node B" || len(entry.Groups) != 1 || entry.Groups[0].ID != "exit-group" {
		t.Fatalf("entry preview: %+v", entry)
	}
	if after := quickCanvasChanges(t, p); before != after {
		t.Fatalf("plan wrote database: %d -> %d", before, after)
	}
	if p.schemeCount() != 0 || len(p.audit.actions) != 0 || p.deployer.calls != 0 {
		t.Fatal("plan saved, audited or published")
	}
}

func TestQuickCanvasCreatesAndPublishesEntry(t *testing.T) {
	p, input := quickCanvasFixture(t)
	owner, csrf := p.session("Owner")
	response := p.call("POST", "/api/v1/relay-schemes/quick", input, owner, csrf)
	var result QuickCanvasResult
	if response.Code != 202 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.SchemeID == "" || len(result.Publication.Entries) != 1 {
		t.Fatalf("quick: %d %s", response.Code, response.Body.String())
	}
	scheme, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
	if err != nil || scheme.Name != "Node A → Node B" || scheme.Execution != "server" || scheme.PublishedAt == "" {
		t.Fatalf("scheme: %+v, %v", scheme, err)
	}
	if len(scheme.Layout.Instances) != 2 || len(scheme.Layout.Connections) != 1 || scheme.Layout.Instances[0].EntryBinding == nil || scheme.Layout.Instances[0].Ref != "server:"+p.nodeA || scheme.Layout.Instances[1].Ref != "inbound:"+input.ExitInboundID {
		t.Fatalf("layout: %+v", scheme.Layout)
	}
	connection := scheme.Layout.Connections[0]
	if connection.Type != "proxy_chain" || connection.TunnelProtocol != "tls" || !connection.SupportTCP || connection.SupportUDP {
		t.Fatalf("connection defaults: %+v", connection)
	}
	entry, err := p.service.Repository.Inbound(p.ctx, result.Publication.Entries[0].InboundID, true)
	if err != nil || entry.NodeID != p.nodeA || entry.InternalRelay || !entry.SubscriptionVisible || entry.DefaultOutbound == "direct" || entry.Port != 20000 {
		t.Fatalf("created entry: %+v, %v", entry, err)
	}
	group, err := p.service.EntryGroups.Group(p.ctx, "exit-group")
	if err != nil || len(group.InboundIDs) != 2 || !slices.Contains(group.InboundIDs, entry.ID) || !slices.Contains(group.InboundIDs, input.ExitInboundID) {
		t.Fatalf("group: %+v, %v", group, err)
	}
	if p.deployer.calls != 1 || !reflect.DeepEqual(p.deployer.order, [][]string{{p.nodeB, p.nodeA}}) || len(result.Publication.Nodes) != 2 {
		t.Fatalf("publication: %+v, calls %d order %v", result.Publication, p.deployer.calls, p.deployer.order)
	}
}

func TestQuickCanvasRequestIDIsIdempotent(t *testing.T) {
	p, input := quickCanvasFixture(t)
	first, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil || second.SchemeID != first.SchemeID || len(second.Publication.Entries) != 1 || second.Publication.Entries[0].InboundID != first.Publication.Entries[0].InboundID {
		t.Fatalf("repeat: %+v, %v; first %+v", second, err, first)
	}
	if p.schemeCount() != 1 || p.deployer.calls != 1 {
		t.Fatalf("repeat created or published: schemes %d, calls %d", p.schemeCount(), p.deployer.calls)
	}
	var bindings int
	if err := p.database.QueryRow("SELECT count(*) FROM canvas_entry_bindings").Scan(&bindings); err != nil || bindings != 1 {
		t.Fatalf("bindings %d, %v", bindings, err)
	}
}

func TestQuickCanvasConcurrentRequestIDIsIdempotent(t *testing.T) {
	p, input := quickCanvasFixture(t)
	var wg sync.WaitGroup
	results := make([]QuickCanvasResult, 2)
	errors := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errors[i] = p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
		}()
	}
	wg.Wait()
	if errors[0] != nil || errors[1] != nil || results[0].SchemeID == "" || results[0].SchemeID != results[1].SchemeID || p.schemeCount() != 1 || p.deployer.calls != 1 {
		t.Fatalf("concurrent repeats: %+v, %v, schemes %d, calls %d", results, errors, p.schemeCount(), p.deployer.calls)
	}
}

func TestQuickCanvasMiddleServerAndGroupModes(t *testing.T) {
	for _, mode := range []string{"", "exit_groups", "explicit", "none"} {
		t.Run(mode, func(t *testing.T) {
			p, input := quickCanvasFixture(t)
			input.MiddleNodeIDs = []string{p.nodeC}
			input.EntryGroups.Mode = mode
			if mode == "explicit" {
				entryPlanGroup(t, p, "selected", "mixed", p.inbound.ID)
				input.EntryGroups.GroupIDs = []string{"selected"}
			}
			result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
			if err != nil || len(result.Publication.Entries) != 1 {
				t.Fatalf("quick: %+v, %v", result, err)
			}
			entry := result.Publication.Entries[0]
			if !strings.HasPrefix(entry.Route, "chain:") || !reflect.DeepEqual(p.deployer.order, [][]string{{p.nodeB, p.nodeC, p.nodeA}}) {
				t.Fatalf("middle route %+v, order %v", entry, p.deployer.order)
			}
			groups, err := p.service.canvasEntryGroups(p.ctx, entry.InboundID, false)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "none" {
				if len(groups) != 0 {
					t.Fatalf("none mode groups: %+v", groups)
				}
			} else {
				want := "exit-group"
				if mode == "explicit" {
					want = "selected"
				}
				if len(groups) != 1 || groups[0].ID != want {
					t.Fatalf("groups: %+v, want %s", groups, want)
				}
			}
		})
	}
}

func TestQuickCanvasPlanRejectsUnsupportedProtocolAndExhaustedPorts(t *testing.T) {
	for _, kind := range []string{"unsupported protocol", "exhausted ports"} {
		t.Run(kind, func(t *testing.T) {
			p, input := quickCanvasFixture(t)
			if kind == "unsupported protocol" {
				input.EntryProtocol = "hysteria2"
			} else {
				entryPlanRange(t, p, p.nodeA, 1080, 1080)
			}
			p.database.SetMaxOpenConns(1)
			before := quickCanvasChanges(t, p)
			result, err := p.service.PlanQuickCanvas(p.ctx, input)
			if err != nil || result.OK || result.Message == "" {
				t.Fatalf("invalid plan: %+v, %v", result, err)
			}
			if _, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users); err == nil {
				t.Fatal("invalid quick request accepted")
			}
			if after := quickCanvasChanges(t, p); after != before {
				t.Fatalf("invalid request wrote database: %d -> %d", before, after)
			}
			if p.schemeCount() != 0 || p.deployer.calls != 0 {
				t.Fatal("invalid request saved or published")
			}
		})
	}
}

type quickCanvasFailedAccess struct{ err error }

func (r quickCanvasFailedAccess) AccessForGroups(context.Context, []string) ([]user.Access, error) {
	return nil, r.err
}

func TestQuickCanvasFailureRemovesSchemeAndMaterializedObjects(t *testing.T) {
	for _, phase := range []string{"snapshot", "publication"} {
		t.Run(phase, func(t *testing.T) {
			p, input := quickCanvasFixture(t)
			injected := errors.New("快速创建故障注入：" + phase)
			var users AccessReader = p.users
			if phase == "snapshot" {
				users = quickCanvasFailedAccess{err: injected}
			} else {
				p.deployer.err = injected
			}
			_, err := p.service.QuickCanvas(p.ctx, input, "Owner", users)
			if !errors.Is(err, injected) {
				t.Fatalf("original error lost: %v", err)
			}
			for _, table := range []string{"canvas_schemes", "canvas_entry_bindings", "outbounds", "proxy_chains", "canvas_tunnel_objects", "canvas_tunnels"} {
				var count int
				if err := p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("%s count %d, %v", table, count, err)
				}
			}
			var count int
			if err := p.database.QueryRow("SELECT count(*) FROM inbounds").Scan(&count); err != nil || count != 2 {
				t.Fatalf("inbounds count %d, %v", count, err)
			}
			group, err := p.service.EntryGroups.Group(p.ctx, "exit-group")
			if err != nil || !reflect.DeepEqual(group.InboundIDs, []string{input.ExitInboundID}) {
				t.Fatalf("group not restored: %+v, %v", group, err)
			}
			p.deployer.err = nil
			result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
			if err != nil || result.SchemeID == "" || len(result.Publication.Entries) != 1 || p.schemeCount() != 1 {
				t.Fatalf("retry after failure: %+v, %v", result, err)
			}
		})
	}
}

func TestQuickCanvasPlanRejectsTooManyHopsAndRepeatedNodes(t *testing.T) {
	for _, kind := range []string{"too many hops", "entry repeated", "exit repeated"} {
		t.Run(kind, func(t *testing.T) {
			p, input := quickCanvasFixture(t)
			switch kind {
			case "too many hops":
				extra, _, _, err := p.service.Nodes.(*node.Repository).CreateEnrollment(p.ctx, "Node D", time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=(SELECT capabilities_json FROM nodes WHERE id=?),public_ip='192.0.2.14' WHERE id=?", p.nodeC, extra.ID); err != nil {
					t.Fatal(err)
				}
				entryPlanRange(t, p, extra.ID, 20000, 20010)
				input.MiddleNodeIDs = []string{p.nodeC, extra.ID}
			case "entry repeated":
				input.MiddleNodeIDs = []string{p.nodeA}
			case "exit repeated":
				input.MiddleNodeIDs = []string{p.nodeB}
			}
			p.database.SetMaxOpenConns(1)
			before := quickCanvasChanges(t, p)
			result, err := p.service.PlanQuickCanvas(p.ctx, input)
			if err == nil && result.OK {
				t.Fatalf("invalid path accepted: %+v", result)
			}
			if after := quickCanvasChanges(t, p); after != before {
				t.Fatalf("invalid plan wrote database: %d -> %d", before, after)
			}
		})
	}
}

type quickCanvasFailedAgent struct{ *canvasDeployRecorder }

func (d quickCanvasFailedAgent) PublishCanvas(ctx context.Context, name, author string, snapshots []NodeSnapshot) (CanvasPublishResult, error) {
	result, err := d.canvasDeployRecorder.PublishCanvas(ctx, name, author, snapshots)
	if err != nil {
		return result, err
	}
	result.BatchID = "quick-failed-agent-batch"
	result.Nodes[0].Status = "failed"
	return result, nil
}

func TestQuickCanvasAgentFailureKeepsSchemeAndBatch(t *testing.T) {
	p, input := quickCanvasFixture(t)
	p.service.Deploy = quickCanvasFailedAgent{p.deployer}
	result, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil || result.SchemeID == "" || result.Publication.BatchID != "quick-failed-agent-batch" || len(result.Publication.Nodes) == 0 || result.Publication.Nodes[0].Status != "failed" {
		t.Fatalf("agent failure result: %+v, %v", result, err)
	}
	scheme, err := p.service.Repository.CanvasScheme(p.ctx, result.SchemeID)
	if err != nil || scheme.PublishedBatchID != result.Publication.BatchID || p.schemeCount() != 1 || len(result.Publication.Entries) != 1 {
		t.Fatalf("failed batch lost scheme: %+v, %v", scheme, err)
	}
	if _, err := p.service.Repository.Inbound(p.ctx, result.Publication.Entries[0].InboundID, false); err != nil {
		t.Fatalf("failed Agent deleted materialized entry: %v", err)
	}
}

type quickCanvasStartedBatch struct {
	panel *publishPanel
	err   error
}

func (d quickCanvasStartedBatch) PublishCanvas(ctx context.Context, name, author string, snapshots []NodeSnapshot) (CanvasPublishResult, error) {
	p := d.panel
	result, err := p.deployer.PublishCanvas(ctx, name, author, snapshots)
	if err != nil {
		return result, err
	}
	result.BatchID = "quick-started-batch"
	if _, err := p.database.ExecContext(ctx, "INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at) VALUES(?,'',?,?,'rolling_back','now')", result.BatchID, name, author); err != nil {
		return result, err
	}
	for index, snapshot := range snapshots {
		data, err := json.Marshal(snapshot.State)
		if err != nil {
			return result, err
		}
		sealed, err := p.service.Repository.Vault.Seal(data)
		if err != nil {
			return result, err
		}
		row, err := p.database.ExecContext(ctx, "INSERT INTO config_revisions(author,summary,status,created_at,encrypted_state) VALUES(?,'quick','pending','now',?)", author, sealed)
		if err != nil {
			return result, err
		}
		revision, err := row.LastInsertId()
		if err != nil {
			return result, err
		}
		if _, err := p.database.ExecContext(ctx, "INSERT INTO deployment_batch_items(batch_id,position,node_id,revision,status) VALUES(?,?,?,?,'rollback_pending')", result.BatchID, index, snapshot.State.NodeID, revision); err != nil {
			return result, err
		}
		result.Nodes[index].Revision = revision
		result.Nodes[index].Status = "rollback_pending"
	}
	return result, d.err
}

func TestQuickCanvasStartedBatchErrorKeepsSchemeAndReplaysCurrentStatus(t *testing.T) {
	p, input := quickCanvasFixture(t)
	injected := errors.New("批次启动后连接中断")
	p.service.Deploy = quickCanvasStartedBatch{panel: p, err: injected}
	first, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil || first.SchemeID == "" || first.Publication.BatchID != "quick-started-batch" || len(first.Publication.Entries) != 1 || len(first.Warnings) == 0 || !strings.Contains(strings.Join(first.Warnings, ";"), injected.Error()) {
		t.Fatalf("started batch error: %+v, %v", first, err)
	}
	if p.schemeCount() != 1 {
		t.Fatal("started batch lost scheme")
	}
	if _, err := p.database.Exec("UPDATE deployment_batch_items SET status='rolled_back' WHERE batch_id=?", first.Publication.BatchID); err != nil {
		t.Fatal(err)
	}
	second, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil || second.SchemeID != first.SchemeID || second.Publication.BatchID != first.Publication.BatchID || len(second.Publication.Nodes) != 2 || len(second.Publication.Entries) != 1 || second.Publication.Entries[0].InboundID != first.Publication.Entries[0].InboundID || p.deployer.calls != 1 {
		t.Fatalf("replayed batch: %+v, %v, calls %d", second, err, p.deployer.calls)
	}
	for _, item := range second.Publication.Nodes {
		if item.Status != "rolled_back" || item.Revision == 0 {
			t.Fatalf("stale replay item: %+v", item)
		}
	}
}

type quickCanvasDraftBatch struct {
	panel *publishPanel
	err   error
}

func (d quickCanvasDraftBatch) PublishCanvas(ctx context.Context, name, author string, snapshots []NodeSnapshot) (CanvasPublishResult, error) {
	_, err := d.panel.database.ExecContext(ctx, "INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at) VALUES('quick-draft-batch','',?,?,'draft','now')", name, author)
	if err != nil {
		return CanvasPublishResult{}, err
	}
	return CanvasPublishResult{BatchID: "quick-draft-batch"}, d.err
}

func TestQuickCanvasDraftBatchFailureCleansUp(t *testing.T) {
	p, input := quickCanvasFixture(t)
	injected := errors.New("批次启动校验失败")
	p.service.Deploy = quickCanvasDraftBatch{panel: p, err: injected}
	_, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if !errors.Is(err, injected) {
		t.Fatalf("original error lost: %v", err)
	}
	for _, table := range []string{"canvas_schemes", "canvas_entry_bindings", "canvas_tunnels", "outbounds", "deployment_batches"} {
		var count int
		if err := p.database.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count %d: %v", table, count, err)
		}
	}
}

func TestQuickCanvasRequiresRequestIDAndEntryProtocol(t *testing.T) {
	for _, field := range []string{"requestId", "entryProtocol"} {
		t.Run(field, func(t *testing.T) {
			p, input := quickCanvasFixture(t)
			if field == "requestId" {
				input.RequestID = ""
			} else {
				input.EntryProtocol = ""
			}
			owner, csrf := p.session("Owner")
			for _, path := range []string{"/api/v1/relay-schemes/quick/plan", "/api/v1/relay-schemes/quick"} {
				response := p.call("POST", path, input, owner, csrf)
				if response.Code != 400 && !(path == "/api/v1/relay-schemes/quick/plan" && response.Code == 200 && strings.Contains(response.Body.String(), `"ok":false`)) {
					t.Fatalf("missing %s: %d %s", field, response.Code, response.Body.String())
				}
			}
			if p.schemeCount() != 0 || p.deployer.calls != 0 {
				t.Fatal("missing required field created or published")
			}
		})
	}
}

func TestQuickCanvasHTTPRejectsViewerCSRFAndUnknownFields(t *testing.T) {
	p := newCanvasPanel(t)
	owner, csrf := p.session("Owner")
	viewer, viewerCSRF := p.session("Viewer")
	for _, path := range []string{"/api/v1/relay-schemes/quick/plan", "/api/v1/relay-schemes/quick"} {
		t.Run(path, func(t *testing.T) {
			if response := p.call("POST", path, `{}`, viewer, viewerCSRF); response.Code != 403 {
				t.Fatalf("Viewer: %d %s", response.Code, response.Body.String())
			}
			if response := p.call("POST", path, `{}`, owner, ""); response.Code != 403 {
				t.Fatalf("CSRF: %d %s", response.Code, response.Body.String())
			}
			if response := p.call("POST", path, `{"unknown":true}`, owner, csrf); response.Code != 400 {
				t.Fatalf("unknown field: %d %s", response.Code, response.Body.String())
			}
			if response := p.call("POST", path, `{"entryGroups":{"mode":"none","unknown":true}}`, owner, csrf); response.Code != 400 {
				t.Fatalf("nested unknown field: %d %s", response.Code, response.Body.String())
			}
		})
	}
	if p.schemeCount() != 0 || len(p.audit.actions) != 0 {
		t.Fatal("rejected requests wrote schemes or audit")
	}
}
