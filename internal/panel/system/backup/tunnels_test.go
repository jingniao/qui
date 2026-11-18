package backup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestTunnelArchive(t *testing.T) {
	t.Parallel()
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
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
		if inventory.Totals.TunnelObjects != 1 || inventory.Totals.TunnelRules != 1 || inventory.Totals.Tunnels != 1 || inventory.Totals.TunnelHops != 2 || inventory.Totals.PortClaims != 2 {
			t.Fatal(inventory.Totals)
		}
		return proxy.ValidateTunnels(ctx, database, source.ConfigurationVault)
	})
	if err != nil {
		t.Fatal(err)
	}
}
