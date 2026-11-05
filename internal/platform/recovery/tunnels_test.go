package recovery

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
	"proxycontrol/pkg/contracts/desiredstate"
)

func rewindTunnelFixture(t *testing.T, database *sql.DB) {
	t.Helper()
	var applied int
	if err := database.QueryRow("SELECT count(*) FROM schema_migrations WHERE version='0129_tunnels.sql'").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		return
	}
	for _, q := range []string{"DROP TABLE canvas_forward_reconcile", "DROP TABLE tunnel_migration_report", "DROP TRIGGER tunnel_rule_node_address_changed", "DROP TRIGGER tunnel_rule_inbound_port_changed", "DROP TABLE tunnel_objects", "DROP TABLE port_claims", "DROP TABLE tunnel_rules", "DROP TABLE tunnel_hops", "DROP TABLE tunnels", "DELETE FROM schema_migrations WHERE version IN ('0129_tunnels.sql','0130_relay_subscription_snapshot.sql','0131_canvas_forward_migration.sql')"} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}
func TestTunnelRestoreVersions(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "pre0129"}[old], func(t *testing.T) {
			ctx := context.Background()
			var ruleID string
			var tunnelID, fingerprint string
			f := listeningFixture(t, func(f *fixture) {
				if old {
					rewindTunnelFixture(t, f.source.Database)
					return
				}
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
			if err := db.Migrate(ctx, f.source.Database, migrations.Files); err != nil {
				t.Fatal(err)
			}
			candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
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
			if old && count != 0 || !old && count != 1 {
				t.Fatal(count)
			}
			if !old {
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
		})
	}
}
