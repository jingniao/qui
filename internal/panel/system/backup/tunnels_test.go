package backup

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
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
	for _, q := range []string{"DROP TRIGGER tunnel_rule_node_address_changed", "DROP TRIGGER tunnel_rule_inbound_port_changed", "DROP TABLE tunnel_objects", "DROP TABLE port_claims", "DROP TABLE tunnel_rules", "DROP TABLE tunnel_hops", "DROP TABLE tunnels", "DELETE FROM schema_migrations WHERE version='0129_tunnels.sql'"} {
		if _, err := database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTunnelArchiveVersions(t *testing.T) {
	for _, old := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "pre0129"}[old], func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			ctx := context.Background()
			if old {
				rewindTunnelFixture(t, source.Database)
			} else {
				var nodeID string
				if err := source.Database.QueryRow("SELECT id FROM nodes LIMIT 1").Scan(&nodeID); err != nil {
					t.Fatal(err)
				}
				service := &proxy.Service{Repository: &proxy.Repository{Database: source.Database, Vault: source.ConfigurationVault}}
				exit, _, _, err := (&node.Repository{Database: source.Database}).CreateEnrollment(ctx, "archive exit", time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				hopPort := 30000
				tunnel, err := service.SaveTunnel(ctx, "", proxy.TunnelInput{Name: "archive tunnel", Transport: "tls", Hops: []proxy.TunnelHop{{Role: "entry", NodeID: nodeID, PortMode: "auto"}, {Role: "exit", NodeID: exit.ID, PortMode: "manual", ListenPort: &hopPort}}})
				if err != nil || tunnel.Credentials == nil {
					t.Fatal(tunnel, err)
				}
				port := 30001
				generated, err := service.Repository.SaveGeneratedInbound(ctx, "", proxy.InboundInput{Name: "[隧道] archive", NodeID: nodeID, Protocol: "forward", Listen: "0.0.0.0", Port: port, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}})
				if err != nil {
					t.Fatal(err)
				}
				rule, err := service.SaveTunnelRule(ctx, "", proxy.TunnelRuleInput{Name: "archive rule", EntryNodeID: nodeID, ListenPort: &port, PortMode: "manual", Network: "tcp", Target: proxy.TunnelTarget{Mode: "manual", Host: "example.com", Port: 443}, Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = source.Database.Exec("INSERT INTO tunnel_objects VALUES('inbound',?,'rule',?)", generated.ID, rule.ID); err != nil {
					t.Fatal(err)
				}
			}
			result, err := source.Create(ctx, directory, "tunnels_archive_01", "owner", testPassword)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
				database, e := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
				if e != nil {
					return e
				}
				defer database.Close()
				inventory, e := ReadInventory(ctx, database)
				if e != nil {
					return e
				}
				want := int64(1)
				if old {
					want = 0
				}
				if inventory.Totals.TunnelObjects != want || inventory.Totals.TunnelRules != want || inventory.Totals.Tunnels != want || inventory.Totals.TunnelHops != want*2 || inventory.Totals.PortClaims != want*2 {
					t.Fatal(inventory.Totals)
				}
				return proxy.ValidateTunnels(ctx, database, source.ConfigurationVault)
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
