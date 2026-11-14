package proxy

import (
	"context"
	"strings"
	"testing"
)

type canvasStatusDeployer struct {
	canvasDeployRecorder
	batch CanvasStatusBatch
}

func (d *canvasStatusDeployer) CanvasBatch(context.Context, string) (CanvasStatusBatch, error) {
	return d.batch, nil
}

func TestCanvasStatusDraftAndReadOnly(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, nil))
	p.database.SetMaxOpenConns(1)
	for _, tc := range []struct {
		name, published, updated string
		dirty                    bool
	}{
		{"never", "", "2026-11-14T00:00:00Z", true},
		{"synced", "2026-11-14T01:00:00Z", "2026-11-14T00:00:00Z", false},
		{"modified", "2026-11-14T00:00:00Z", "2026-11-14T01:00:00Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := p.database.Exec("UPDATE canvas_schemes SET published_at=?,updated_at=? WHERE id=?", tc.published, tc.updated, scheme.ID); err != nil {
				t.Fatal(err)
			}
			before := quickCanvasChanges(t, p)
			status, err := p.service.CanvasSchemeStatus(p.ctx, scheme.ID)
			if err != nil {
				t.Fatal(err)
			}
			if status.Draft.Dirty != tc.dirty || status.Probe.Status != "untested" || status.Subscription.Ready || len(status.Entries) != 0 {
				t.Fatalf("status %+v", status)
			}
			if !strings.Contains(strings.Join(status.Subscription.Reasons, "；"), "用户入口未绑定") {
				t.Fatalf("missing entry reason: %+v", status.Subscription)
			}
			if after := quickCanvasChanges(t, p); before != after {
				t.Fatalf("read wrote database: %d -> %d", before, after)
			}
		})
	}
}

func TestCanvasStatusBatchAndConfirmation(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, nil))
	if _, err := p.database.Exec("UPDATE canvas_schemes SET published_batch_id='batch',published_at=updated_at WHERE id=?", scheme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(10,'Owner','status','success','now',X'00')"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("INSERT INTO node_runtime_state(node_id,revision) VALUES(?,10)", p.nodeA); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"success", "failed", "deploying", "rolling_back", "rolled_back", "rollback_failed"} {
		t.Run(state, func(t *testing.T) {
			p.service.Deploy = &canvasStatusDeployer{batch: CanvasStatusBatch{ID: "batch", Status: state, Items: []CanvasStatusBatchItem{{NodeID: p.nodeA, Revision: 10, Status: "success"}, {NodeID: p.nodeB, Revision: 11, Status: "pending"}}}}
			status, err := p.service.CanvasSchemeStatus(p.ctx, scheme.ID)
			if err != nil {
				t.Fatal(err)
			}
			if status.Batch.Status != state || len(status.Nodes) != 2 || !status.Nodes[0].Confirmed || status.Nodes[1].Confirmed || status.Nodes[1].ExpectedRevision != 11 || status.Batch.Items[0].NodeName != "Node A" {
				t.Fatalf("status %+v", status)
			}
			if got := strings.Contains(strings.Join(status.Subscription.Reasons, "；"), "部署批次尚未成功"); got != (state != "success") {
				t.Fatalf("batch reason: %+v", status.Subscription)
			}
		})
	}
}

func TestCanvasStatusSubscriptionConditions(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	if err := p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: "binding", SchemeID: &scheme.ID, InstanceID: "i-a", InboundID: p.inbound.ID, Mode: "reuse", State: "applied", OriginalDefaultOutbound: "direct", AppliedDefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	entryPlanGroup(t, p, "status-group", "subscription", p.inbound.ID)
	if _, err := p.database.Exec("UPDATE canvas_schemes SET published_batch_id='batch',published_at=updated_at WHERE id=?", scheme.ID); err != nil {
		t.Fatal(err)
	}
	p.service.Deploy = &canvasStatusDeployer{batch: CanvasStatusBatch{ID: "batch", Status: "success", Items: []CanvasStatusBatchItem{}}}
	for _, tc := range []struct{ name, sql, undo, reason string }{
		{"ready", "", "", ""},
		{"disabled", "UPDATE inbounds SET enabled=0", "UPDATE inbounds SET enabled=1", "未启用"},
		{"hidden", "UPDATE inbounds SET subscription_visible=0", "UPDATE inbounds SET subscription_visible=1", "订阅不可见"},
		{"group-purpose", "UPDATE node_groups SET purpose='relay'", "UPDATE node_groups SET purpose='subscription'", "分组用途"},
		{"group-disabled", "UPDATE node_groups SET enabled=0", "UPDATE node_groups SET enabled=1", "分组未启用"},
		{"connect-host", "UPDATE nodes SET public_ip='',connect_host='entry.example'", "UPDATE nodes SET public_ip='192.0.2.10',connect_host=''", ""},
		{"address", "UPDATE nodes SET public_ip='',connect_host=''", "UPDATE nodes SET public_ip='192.0.2.10'", "客户端连接地址"},
		{"no-group", "DELETE FROM node_group_inbounds", "", "未加入"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.sql != "" {
				if _, err := p.database.Exec(tc.sql); err != nil {
					t.Fatal(err)
				}
			}
			status, err := p.service.CanvasSchemeStatus(p.ctx, scheme.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason == "" && !status.Subscription.Ready || tc.reason != "" && (status.Subscription.Ready || !strings.Contains(strings.Join(status.Subscription.Reasons, "；"), tc.reason)) {
				t.Fatalf("subscription %+v", status.Subscription)
			}
			if len(status.Entries) != 1 || status.Entries[0].InboundID != p.inbound.ID || status.Entries[0].Route != "direct" {
				t.Fatalf("entries %+v", status.Entries)
			}
			if tc.undo != "" {
				if _, err := p.database.Exec(tc.undo); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestCanvasStatusHTTPPermissions(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, nil))
	owner, _ := p.session("Owner")
	viewer, _ := p.session("Viewer")
	for _, tc := range []struct {
		role string
		want int
	}{{"Owner", 200}, {"Viewer", 403}} {
		cookie := owner
		if tc.role == "Viewer" {
			cookie = viewer
		}
		response := p.call("GET", "/api/v1/canvas-schemes/"+scheme.ID+"/status", nil, cookie, "")
		if response.Code != tc.want {
			t.Fatalf("%s: %d %s", tc.role, response.Code, response.Body.String())
		}
	}
}

func TestCanvasStatusProbeMatchesRecordedEntryTCP(t *testing.T) {
	p := newPublishPanel(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, nil))
	if err := p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: "probe-binding", SchemeID: &scheme.ID, InstanceID: "i-a", InboundID: p.inbound.ID, Mode: "reuse", State: "applied", OriginalDefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("INSERT INTO probe_targets(id,label,host,port) VALUES('probe-target','entry','changed.example',443)"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, command, at, result string }{
		{"matching", `{"addresses":["192.0.2.10"],"port":1080}`, "2026-11-14T00:00:00Z", `{"attempted":3,"lost":0,"median_ms":5,"code":"complete"}`},
		{"newest", `{"addresses":["192.0.2.10"],"port":1080}`, "2026-11-14T01:00:00Z", `{"attempted":3,"lost":3,"code":"complete"}`},
		{"other-port", `{"addresses":["192.0.2.10"],"port":443}`, "2026-11-14T02:00:00Z", `{"attempted":3,"lost":0}`},
		{"other-address", `{"addresses":["192.0.2.99"],"port":1080}`, "2026-11-14T03:00:00Z", `{"attempted":3,"lost":0}`},
		{"mixed-addresses", `{"addresses":["192.0.2.10","192.0.2.99"],"port":1080}`, "2026-11-14T04:00:00Z", `{"attempted":3,"lost":0,"code":"complete"}`},
	} {
		if _, err := p.database.Exec("INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,result_json,created_at,deadline,status) VALUES(?,?,'probe-target',?,?,?,?,'complete')", tc.id, p.nodeB, tc.command, tc.result, tc.at, tc.at); err != nil {
			t.Fatal(err)
		}
	}
	before := quickCanvasChanges(t, p)
	status, err := p.service.CanvasSchemeStatus(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Probe.Status != "unreachable" || status.Probe.At != "2026-11-14T01:00:00Z" || len(status.Probe.Results) != 1 || status.Probe.Results[0].Lost != 3 || status.Probe.Results[0].NodeID != p.nodeB {
		t.Fatalf("probe %+v", status.Probe)
	}
	if after := quickCanvasChanges(t, p); after != before {
		t.Fatalf("probe read wrote database: %d -> %d", before, after)
	}
	for _, tc := range []struct{ result, want string }{
		{`{"attempted":3,"lost":0,"code":"complete"}`, "reachable"},
		{`{"attempted":0,"lost":0,"code":"complete"}`, "incomplete"},
		{`{"attempted":1,"lost":0,"code":"cancelled"}`, "incomplete"},
		{`{"attempted":0,"lost":0,"code":"invalid"}`, "incomplete"},
	} {
		if _, err := p.database.Exec("UPDATE node_tcp_probes SET result_json=? WHERE id='newest'", tc.result); err != nil {
			t.Fatal(err)
		}
		status, err := p.service.CanvasSchemeStatus(p.ctx, scheme.ID)
		if err != nil || status.Probe.Status != tc.want {
			t.Fatalf("probe %+v: %v", status.Probe, err)
		}
	}
	if _, err := p.database.Exec("UPDATE nodes SET connect_host='entry.example' WHERE id=?", p.nodeA); err != nil {
		t.Fatal(err)
	}
	status, err = p.service.CanvasSchemeStatus(p.ctx, scheme.ID)
	if err != nil || status.Probe.Status != "untested" {
		t.Fatalf("domain was guessed: %+v, %v", status.Probe, err)
	}
}
