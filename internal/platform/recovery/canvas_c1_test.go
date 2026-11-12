package recovery

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/testutil/dbtest"
)

func TestCanvasC1RestoreLayout(t *testing.T) {
	ctx := context.Background()
	var scheme proxy.CanvasScheme
	f := listeningFixture(t, func(f *fixture) {
		r := &proxy.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}
		in, err := r.SaveGeneratedInbound(ctx, "", proxy.InboundInput{Name: "exit", NodeID: f.node.ID, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, DefaultOutbound: "direct"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := r.SaveOutbound(ctx, "", proxy.OutboundInput{Name: "external", NodeID: f.node.ID, Protocol: "socks5", Address: "198.51.100.8", Port: 1080, Enabled: true, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
		if err != nil {
			t.Fatal(err)
		}
		port := 30124
		layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{
			{InstanceID: "a", Ref: "server:" + f.node.ID, Role: "relay", RelayPortMode: "auto", RelayPort: &port},
			{InstanceID: "b", Ref: "inbound:" + in.ID, Role: "exit"},
			{InstanceID: "c", Ref: "outbound:" + out.ID, Role: "exit"},
		}, Connections: []proxy.CanvasConnection{{ID: "external", Source: "a", Target: "c", Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true}}}
		scheme, err = r.SaveCanvasScheme(ctx, "", proxy.CanvasSchemeInput{Name: "C1 restore", Execution: "server", Layout: layout})
		if err != nil {
			t.Fatal(err)
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
	if err := proxy.ValidateCanvasTunnels(ctx, restored); err != nil {
		t.Fatal(err)
	}
	got, err := (&proxy.Repository{Database: restored}).CanvasScheme(ctx, scheme.ID)
	if err != nil || !reflect.DeepEqual(got.Layout, scheme.Layout) {
		t.Fatalf("restored C1 layout %+v %v", got, err)
	}
}
