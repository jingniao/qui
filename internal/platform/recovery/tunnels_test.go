package recovery

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestTunnelRestore(t *testing.T) {
	ctx := context.Background()
	var ruleID string
	var tunnelID, fingerprint string
	f := listeningFixture(t, func(f *fixture) {
		service := &proxy.Service{Repository: &proxy.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}}
		exit, _, _, err := (&node.Repository{Database: f.source.Database}).CreateEnrollment(ctx, "tunnel exit", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		hopPort := 30000
		tunnel, err := service.SaveTunnel(ctx, "", proxy.TunnelInput{Name: "restored tunnel", Transport: "tls", Hops: []proxy.TunnelHop{{Role: "entry", NodeID: f.node.ID, PortMode: "auto"}, {Role: "exit", NodeID: exit.ID, PortMode: "manual", ListenPort: &hopPort}}})
		if err != nil {
			t.Fatal(err)
		}
		tunnelID, fingerprint = tunnel.ID, tunnel.Credentials.Fingerprint
		port := 30001
		generated, err := service.Repository.SaveGeneratedInbound(ctx, "", proxy.InboundInput{Name: "[隧道] restored", NodeID: f.node.ID, Protocol: "forward", Listen: "0.0.0.0", Port: port, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}})
		if err != nil {
			t.Fatal(err)
		}
		rule, err := service.SaveTunnelRule(ctx, "", proxy.TunnelRuleInput{Name: "restored rule", EntryNodeID: f.node.ID, ListenPort: &port, PortMode: "manual", Network: "both", TunnelID: &tunnelID, Target: proxy.TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		ruleID = rule.ID
		if _, err = f.source.Database.Exec("INSERT INTO tunnel_objects VALUES('inbound',?,'rule',?)", generated.ID, rule.ID); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"tunnels", "tunnel_rules"} {
			if _, err = f.source.Database.Exec("UPDATE " + table + " SET status='published',published_version=draft_version,published_at=updated_at"); err != nil {
				t.Fatal(err)
			}
		}
	})
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = proxy.ValidateTunnels(ctx, restored, f.source.ConfigurationVault); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = restored.QueryRow("SELECT count(*) FROM tunnel_rules").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal(count)
	}
	var owner string
	if err = restored.QueryRow("SELECT owner_id FROM tunnel_objects WHERE owner_kind='rule'").Scan(&owner); err != nil || owner != ruleID {
		t.Fatal(owner, err)
	}
	rule, err := (&proxy.Repository{Database: restored}).TunnelRule(ctx, ruleID)
	if err != nil || *rule.ListenPort != 30001 || rule.Status != "modified" {
		t.Fatal(rule, err)
	}
	tunnel, err := (&proxy.Repository{Database: restored, Vault: f.source.ConfigurationVault}).Tunnel(ctx, tunnelID)
	if err != nil || tunnel.Credentials == nil || tunnel.Credentials.Fingerprint != fingerprint || tunnel.Status != "modified" {
		t.Fatal(tunnel, err)
	}
}
