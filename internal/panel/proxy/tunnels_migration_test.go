package proxy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func plainMigrationPanel(t *testing.T) (*publishPanel, CanvasScheme, CanvasTunnel) {
	t.Helper()
	p := newPublishPanel(t)
	caps, _ := json.Marshal(&pb.Capabilities{CoreName: "xray", ForwardInboundSupported: true})
	if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(caps), p.nodeA); err != nil {
		t.Fatal(err)
	}
	c := CanvasConnection{ID: "old-forward", Source: "i-a", Target: "i-b", Type: "port_forward", SupportTCP: true, SupportUDP: true, ListenPort: canvasTestPort(23200), TargetHost: "example.com", TargetPort: canvasTestPort(8443)}
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{c})
	scheme, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "plain old", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	tunnels, err := p.service.Repository.CanvasTunnels(p.ctx)
	if err != nil || len(tunnels) != 1 {
		t.Fatal(tunnels, err)
	}
	return p, scheme, tunnels[0]
}

func TestCanvasForwardMigrationPreservesPublication(t *testing.T) {
	for _, gost := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "gost"}[gost], func(t *testing.T) {
			fixture := plainMigrationPanel
			if gost {
				fixture = gostRelayPanel
			}
			p, scheme, legacy := fixture(t)
			if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			before := migrationSnapshots(t, p)
			var sealedBefore []byte
			if err := p.database.QueryRow("SELECT gost_material FROM canvas_tunnels WHERE id=?", legacy.ID).Scan(&sealedBefore); err != nil {
				t.Fatal(err)
			}
			r := p.service.Repository
			if err := r.ReconcileCanvasForwards(p.ctx); err != nil {
				t.Fatal(err)
			}
			rule, err := r.TunnelRule(p.ctx, legacy.ID)
			if err != nil || rule.Status != "published" || rule.PublishedVersion != rule.DraftVersion || rule.SubscriptionVisible || rule.PortMode != "manual" || rule.Network != "both" || *rule.ListenPort != 23200 || rule.Name != legacy.Name || rule.Target.Mode != "manual" {
				t.Fatal(rule, err)
			}
			if gost {
				tunnel, err := r.Tunnel(p.ctx, legacy.ID)
				if err != nil || tunnel.Status != "published" || !reflect.DeepEqual(sealedBefore, tunnel.sealedMaterial) || *tunnel.Hops[1].ListenPort != 23000 || tunnel.Hops[1].PortMode != "manual" {
					t.Fatal(tunnel, err)
				}
				material, err := r.openGostMaterial(tunnel.ID, tunnel.sealedMaterial)
				if err != nil || material.Next != nil {
					t.Fatal("old single-leg material was changed", err)
				}
			}
			var claims, objects, canvas int
			for query, dst := range map[string]*int{"SELECT count(*) FROM port_claims": &claims, "SELECT count(*) FROM tunnel_objects": &objects, "SELECT count(*) FROM canvas_tunnel_objects": &canvas} {
				if err = p.database.QueryRow(query).Scan(dst); err != nil {
					t.Fatal(err)
				}
			}
			expected := 1
			if gost {
				expected = 2
			}
			if claims != expected || objects != expected || canvas != 0 {
				t.Fatal(claims, objects, canvas)
			}
			if err = ValidateTunnels(p.ctx, p.database, r.Vault); err != nil {
				t.Fatal(err)
			}
			publication, err := p.service.buildTunnelPublication(p.ctx, PublishSelection{RuleIDs: []string{legacy.ID}})
			if err != nil || !publication.view.OK {
				t.Fatal(publication.view, err)
			}
			for _, server := range publication.view.Servers {
				if len(server.Changes) != 0 {
					t.Fatal(server)
				}
			}
			p.service.Deploy = &relayReceipt{apply: func() error {
				if migrationSnapshots(t, p) != before {
					t.Fatal("publish changed materialized desired state")
				}
				return nil
			}}
			if _, err = p.service.PublishTunnels(p.ctx, PublishSelection{RuleIDs: []string{legacy.ID}}, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			p.service.Deploy = p.deployer
			if got := migrationSnapshots(t, p); got != before {
				t.Fatalf("republish changed desired state\nbefore %s\nafter %s", before, got)
			}
			calls := p.deployer.calls
			if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			if p.deployer.calls != calls || migrationSnapshots(t, p) != before {
				t.Fatal("canvas republish was not a no-op")
			}
			stored, err := r.CanvasScheme(p.ctx, scheme.ID)
			if err != nil || len(stored.Layout.Connections) != 0 || len(stored.Layout.Instances) != 2 {
				t.Fatal(stored, err)
			}
			if err = r.ReconcileCanvasForwards(p.ctx); err != nil {
				t.Fatal(err)
			}
			report, err := r.TunnelMigrationReport(p.ctx)
			if err != nil || len(report) != 0 {
				t.Fatal(report, err)
			}
		})
	}
}

func TestCanvasForwardMigrationTransactionRollback(t *testing.T) {
	p, scheme, _ := plainMigrationPanel(t)
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	before := migrationSnapshots(t, p)
	if _, err := p.database.Exec("CREATE TRIGGER fail_forward_reconcile BEFORE UPDATE OF done ON canvas_forward_reconcile BEGIN SELECT RAISE(ABORT,'simulated transaction failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.ReconcileCanvasForwards(p.ctx); err == nil {
		t.Fatal("failure was ignored")
	}
	var done, rules, owners int
	if err := p.database.QueryRow("SELECT done,(SELECT count(*) FROM tunnel_rules),(SELECT count(*) FROM canvas_tunnel_objects) FROM canvas_forward_reconcile").Scan(&done, &rules, &owners); err != nil {
		t.Fatal(err)
	}
	if done != 0 || rules != 0 || owners != 1 || migrationSnapshots(t, p) != before {
		t.Fatal(done, rules, owners)
	}
	if _, err := p.database.Exec("DROP TRIGGER fail_forward_reconcile"); err != nil {
		t.Fatal(err)
	}
	if err := p.service.Repository.ReconcileCanvasForwards(p.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasForwardMigrationSharedAndMixedSchemes(t *testing.T) {
	p, scheme, legacy := plainMigrationPanel(t)
	n, err := p.service.Nodes.Get(p.ctx, p.nodeB)
	if err != nil {
		t.Fatal(err)
	}
	n.Capabilities.ForwardInboundSupported = true
	caps, _ := json.Marshal(n.Capabilities)
	if _, err = p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(caps), p.nodeA); err != nil {
		t.Fatal(err)
	}
	layout := scheme.Layout
	layout.Connections = append(append([]CanvasConnection{}, layout.Connections...), canvasTestHop("i-a", "i-b", 8444))
	scheme, err = p.service.Repository.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: scheme.Name, Execution: "server", UpdatedAt: scheme.UpdatedAt, Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	sharedLayout := scheme.Layout
	sharedLayout.Connections = sharedLayout.Connections[:1]
	shared, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "shared forward", Execution: "server", Layout: sharedLayout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, shared.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE canvas_schemes SET client_published_layout=layout_json"); err != nil {
		t.Fatal(err)
	}
	before := migrationSnapshots(t, p)
	if err = p.service.Repository.ReconcileCanvasForwards(p.ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{scheme.ID, shared.ID} {
		var remaining int
		if err = p.database.QueryRow("SELECT count(*) FROM canvas_schemes s,json_each(s.client_published_layout,'$.connections') c WHERE s.id=? AND json_extract(c.value,'$.tunnelId')=?", id, legacy.ID).Scan(&remaining); err != nil || remaining != 0 {
			t.Fatal(remaining, err)
		}
		calls := p.deployer.calls
		if _, err = p.service.PublishCanvasScheme(p.ctx, id, "Owner", p.users); err != nil {
			t.Fatal(err)
		}
		if p.deployer.calls != calls || migrationSnapshots(t, p) != before {
			t.Fatal("shared/mixed scheme changed running state")
		}
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil || len(stored.Layout.Connections) != 1 || stored.Layout.Connections[0].Type != "tunnel" {
		t.Fatal(stored, err)
	}
}

func TestTunnelMigrationArchiveValidation(t *testing.T) {
	for _, bad := range []string{"marker", "owner", "claim", "snapshot"} {
		t.Run(bad, func(t *testing.T) {
			p, scheme, legacy := plainMigrationPanel(t)
			if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			r := p.service.Repository
			if err := r.ReconcileCanvasForwards(p.ctx); err != nil {
				t.Fatal(err)
			}
			query := "DELETE FROM canvas_forward_reconcile"
			switch bad {
			case "owner":
				query = "UPDATE tunnel_objects SET owner_id='missing'"
			case "claim":
				query = "UPDATE port_claims SET port=23456"
			case "snapshot":
				query = "UPDATE tunnel_rules SET published_snapshot='{broken'"
			}
			if _, err := p.database.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := ValidateTunnels(p.ctx, p.database, r.Vault); err == nil {
				t.Fatal("invalid archive accepted", legacy.ID)
			}
		})
	}
}

func TestCanvasForwardMigrationLeavesUnsafeCases(t *testing.T) {
	for _, reason := range []string{"unpublished", "unowned", "path", "conflict", "missing", "dirty"} {
		t.Run(reason, func(t *testing.T) {
			p, scheme, legacy := plainMigrationPanel(t)
			if reason != "unpublished" {
				if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
					t.Fatal(err)
				}
			}
			switch reason {
			case "unowned":
				if _, err := p.database.Exec("DELETE FROM canvas_tunnel_objects"); err != nil {
					t.Fatal(err)
				}
			case "path":
				scheme.Layout.Connections[0].TunnelID = legacy.ID
				scheme.Layout.Instances[1].Ref = "server:" + p.nodeC
				data, _ := json.Marshal(scheme.Layout)
				if _, err := p.database.Exec("INSERT INTO canvas_schemes(id,name,execution,layout_json,created_at,updated_at) VALUES('ambiguous','ambiguous','server',?,'now','now')", string(data)); err != nil {
					t.Fatal(err)
				}
				if _, err := p.database.Exec("UPDATE canvas_schemes SET published_at=updated_at WHERE name='ambiguous'"); err != nil {
					t.Fatal(err)
				}
				if _, err := p.database.Exec("INSERT INTO canvas_tunnel_publications SELECT id,? FROM canvas_schemes WHERE name='ambiguous'", legacy.ID); err != nil {
					t.Fatal(err)
				}
			case "conflict":
				if _, err := p.database.Exec("INSERT INTO port_claims VALUES(?,23200,'canvas',?)", p.nodeA, "other-owner"); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if _, err := p.database.Exec("UPDATE nodes SET deleted_at='deleted' WHERE id=?", p.nodeB); err != nil {
					t.Fatal(err)
				}
			case "dirty":
				if _, err := p.database.Exec("UPDATE canvas_tunnels SET updated_at='changed'"); err != nil {
					t.Fatal(err)
				}
			}
			before := migrationSnapshots(t, p)
			if err := p.service.Repository.ReconcileCanvasForwards(p.ctx); err != nil {
				t.Fatal(err)
			}
			if migrationSnapshots(t, p) != before {
				t.Fatal("unsafe case changed running objects")
			}
			report, err := p.service.Repository.TunnelMigrationReport(p.ctx)
			if err != nil || len(report) != 1 || report[0].TunnelID != legacy.ID || report[0].Reason == "" {
				t.Fatal(report, err)
			}
			var count int
			if err = p.database.QueryRow("SELECT count(*) FROM tunnel_rules").Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
			if err = p.service.Repository.ReconcileCanvasForwards(p.ctx); err != nil {
				t.Fatal(err)
			}
			report2, err := p.service.Repository.TunnelMigrationReport(p.ctx)
			if err != nil || !reflect.DeepEqual(report, report2) {
				t.Fatal(report2, err)
			}
		})
	}
}

func TestTunnelMigrationReportOwnerOnly(t *testing.T) {
	p := newCanvasPanel(t)
	if _, err := p.database.Exec("INSERT INTO tunnel_migration_report VALUES('old','旧连接','方案尚未发布')"); err != nil {
		t.Fatal(err)
	}
	owner, _ := p.session("Owner")
	viewer, _ := p.session("Viewer")
	response := p.call("GET", "/api/v1/tunnels/migration-report", nil, owner, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "方案尚未发布") {
		t.Fatal(response.Code, response.Body.String())
	}
	assertTunnelHTTP(t, p.call("GET", "/api/v1/tunnels/migration-report", nil, viewer, ""), 403, "")
}

func TestCanvasForwardMigrationLinkedTarget(t *testing.T) {
	for _, match := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-port-other-host", true: "published-inbound"}[match], func(t *testing.T) {
			p, scheme, legacy := plainMigrationPanel(t)
			target, err := p.service.Repository.SaveInbound(p.ctx, "", InboundInput{Name: "target", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 8443, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			if match {
				var c CanvasConnection
				if err = json.Unmarshal([]byte(`{}`), &c); err != nil {
					t.Fatal(err)
				}
				c = scheme.Layout.Connections[0]
				c.TargetHost = "192.0.2.11"
				data, _ := json.Marshal(c)
				if _, err = p.database.Exec("UPDATE canvas_tunnels SET connection_json=? WHERE id=?", string(data), legacy.ID); err != nil {
					t.Fatal(err)
				}
			}
			// Store a successfully applied target revision, as deployment does.
			state, err := p.service.Snapshot(p.ctx, p.nodeB, p.users)
			if err != nil {
				t.Fatal(err)
			}
			state.Revision = 1
			state.Inbounds = []desiredstate.Inbound{{ID: target.ID, Protocol: "socks5", Listen: "0.0.0.0", Port: 8443, Enabled: true, DefaultOutbound: "direct"}}
			state.Users = []desiredstate.User{{ID: "published-user", Enabled: true}}
			state.Credentials = []desiredstate.Credential{{ID: "published-credential", UserID: "published-user", DeviceID: "published-device", Protocol: "socks5", Secret: "test-password", Username: "test-user"}}
			state.Inbounds[0].CredentialIDs = []string{"published-credential"}
			data, _ := json.Marshal(state)
			sealed, err := p.service.Repository.Vault.Seal(data)
			if err != nil {
				t.Fatal(err)
			}
			result, err := p.database.Exec("INSERT INTO config_revisions(author,summary,status,created_at,encrypted_state) VALUES('Owner','target','success','now',?)", sealed)
			if err != nil {
				t.Fatal(err)
			}
			revision, _ := result.LastInsertId()
			if _, err = p.database.Exec("INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?)", p.nodeB, revision); err != nil {
				t.Fatal(err)
			}
			if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			if err = p.service.Repository.ReconcileCanvasForwards(p.ctx); err != nil {
				t.Fatal(err)
			}
			rule, err := p.service.Repository.TunnelRule(p.ctx, legacy.ID)
			if err != nil {
				t.Fatal(err)
			}
			if match && (rule.Target.Mode != "linked" || rule.Target.InboundID != target.ID) || !match && rule.Target.Mode != "manual" {
				t.Fatal(rule)
			}
		})
	}
}

func migrationSnapshots(t *testing.T, p *publishPanel) string {
	t.Helper()
	states := []desiredstate.State{}
	for _, node := range []string{p.nodeA, p.nodeB} {
		state, err := p.service.Snapshot(p.ctx, node, p.users)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	data, err := json.Marshal(states)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
