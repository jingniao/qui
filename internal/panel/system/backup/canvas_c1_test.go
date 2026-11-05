package backup

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/proxy"
)

func TestCanvasC1ExitAndRelayFieldsArchive(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	var nodeID string
	if err := source.Database.QueryRow("SELECT id FROM nodes LIMIT 1").Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	repository := &proxy.Repository{Database: source.Database, Vault: source.ConfigurationVault}
	in, err := repository.SaveGeneratedInbound(ctx, "", proxy.InboundInput{Name: "US SOCKS5", NodeID: nodeID, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	inboundID := in.ID
	out, err := repository.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "external SOCKS5", NodeID: nodeID, Protocol: "socks5", Address: "198.51.100.8", Port: 1080, Enabled: true, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
	if err != nil {
		t.Fatal(err)
	}
	port := 30123
	layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{
		{InstanceID: "entry", Ref: "inbound:" + inboundID, Role: "entry"},
		{InstanceID: "relay", Ref: "server:" + nodeID, Role: "relay", RelayPortMode: "manual", RelayPort: &port},
		{InstanceID: "exit-inbound", Ref: "inbound:" + inboundID, Role: "exit"},
		{InstanceID: "exit-outbound", Ref: "outbound:" + out.ID, Role: "exit"},
	}, Connections: []proxy.CanvasConnection{{ID: "external", Source: "relay", Target: "exit-outbound", Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true}}}
	scheme, err := repository.SaveCanvasScheme(ctx, "", proxy.CanvasSchemeInput{Name: "C1 fixture", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "canvas_c1_fixture", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareRestore(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, restoreTargetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := DiscardPreparedRestore(ctx, prepared); err != nil {
			t.Error(err)
		}
	}()
	if _, err := VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256); err != nil {
		t.Fatal(err)
	}
	database, err := readonlyDatabase(filepath.Join(prepared.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := proxy.ValidateCanvasTunnels(ctx, database); err != nil {
		t.Fatal(err)
	}
	restored, err := (&proxy.Repository{Database: database}).CanvasScheme(ctx, scheme.ID)
	if err != nil || !reflect.DeepEqual(restored, scheme) {
		t.Fatalf("C1 layout changed: %+v %v", restored, err)
	}
}
