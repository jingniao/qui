// Package canvasfixture builds published v1.3 canvas forwards for archive and
// startup integration tests through the real canvas materialization service.
package canvasfixture

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
)

type Fixture struct {
	SchemeID   string
	RuleIDs    []string
	InboundIDs []string
	Material   []byte
}

type publisher struct{}

func (publisher) PublishCanvas(_ context.Context, _, _ string, snapshots []proxy.NodeSnapshot) (proxy.CanvasPublishResult, error) {
	for _, snapshot := range snapshots {
		if err := snapshot.State.Validate(); err != nil {
			return proxy.CanvasPublishResult{}, err
		}
	}
	return proxy.CanvasPublishResult{}, nil
}

func PublishedForwards(t *testing.T, database *sql.DB, key *vault.Vault) Fixture {
	t.Helper()
	ctx := context.Background()
	nodes := &node.Repository{Database: database}
	ids := []string{}
	for _, name := range []string{"M1 entry", "M1 exit"} {
		n, _, _, err := nodes.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err = nodes.SetPublicIP(ctx, n.ID, "192.0.2.50"); err != nil {
			t.Fatal(err)
		}
		caps, _ := json.Marshal(&pb.Capabilities{CoreName: "xray", ForwardInboundSupported: true, MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray", ForwardInboundSupported: true}, {Name: "gost", ForwardInboundSupported: true, GostContractVersion: 3}}})
		if _, err = database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(caps), n.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	if _, err := database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,30002,30010)", ids[1]); err != nil {
		t.Fatal(err)
	}
	r := &proxy.Repository{Database: database, Vault: key}
	port, target := 30000, 8443
	gostPort := 30001
	layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "a", Ref: "server:" + ids[0], Role: "entry"}, {InstanceID: "b", Ref: "server:" + ids[1], Role: "exit"}}, Connections: []proxy.CanvasConnection{
		{ID: "plain", Source: "a", Target: "b", Type: "port_forward", ListenPort: &port, TargetHost: "example.com", TargetPort: &target, SupportTCP: true, SupportUDP: true},
		{ID: "gost", Source: "a", Target: "b", Type: "port_forward", TunnelProtocol: "gost", ListenPort: &gostPort, TargetHost: "example.org", TargetPort: &target, SupportTCP: true, SupportUDP: true},
	}}
	scheme, err := r.SaveCanvasScheme(ctx, "", proxy.CanvasSchemeInput{Name: "M1 old forwards", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	service := &proxy.Service{Repository: r, Nodes: nodes, Deploy: publisher{}}
	if _, err = service.PublishCanvasScheme(ctx, scheme.ID, "Owner", &user.Repository{Database: database, Vault: key}); err != nil {
		t.Fatal(err)
	}
	fixture := Fixture{SchemeID: scheme.ID}
	for _, c := range scheme.Layout.Connections {
		fixture.RuleIDs = append(fixture.RuleIDs, c.TunnelID)
	}
	if err = database.QueryRow("SELECT gost_material FROM canvas_tunnels WHERE id=?", fixture.RuleIDs[1]).Scan(&fixture.Material); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query("SELECT object_id FROM canvas_tunnel_objects ORDER BY object_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		fixture.InboundIDs = append(fixture.InboundIDs, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func AssertConverted(t *testing.T, database *sql.DB, key *vault.Vault, f Fixture, restored bool) {
	t.Helper()
	r := &proxy.Repository{Database: database, Vault: key}
	for _, id := range f.RuleIDs {
		rule, err := r.TunnelRule(context.Background(), id)
		if err != nil || rule.SubscriptionVisible || rule.Target.Mode != "manual" || rule.PortMode != "manual" || rule.Network != "both" {
			t.Fatal(rule, err)
		}
		want := "published"
		if restored {
			want = "modified"
		}
		if rule.Status != want {
			t.Fatal(rule.Status, want)
		}
	}
	var material []byte
	if err := database.QueryRow("SELECT gost_material FROM tunnels WHERE id=?", f.RuleIDs[1]).Scan(&material); err != nil || string(material) != string(f.Material) {
		t.Fatal("sealed material changed", err)
	}
	for _, id := range f.InboundIDs {
		var count int
		if err := database.QueryRow("SELECT count(*) FROM tunnel_objects WHERE object_id=?", id).Scan(&count); err != nil || count != 1 {
			t.Fatal(id, count, err)
		}
	}
	if err := proxy.ValidateTunnels(context.Background(), database, key); err != nil {
		t.Fatal(err)
	}
	scheme, err := r.CanvasScheme(context.Background(), f.SchemeID)
	if err != nil || len(scheme.Layout.Connections) != 0 || len(scheme.Layout.Instances) != 2 {
		t.Fatal(scheme, err)
	}
	if err = r.ReconcileCanvasForwards(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = database.QueryRow("SELECT count(*) FROM port_claims WHERE owner_id IN (?,?)", f.RuleIDs[0], f.RuleIDs[1]).Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
}
