package backup

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/proxy"
)

func TestCanvasForwardFieldsBackupRestoreRoundTrip(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	var nodeID string
	if err := source.Database.QueryRow("SELECT id FROM nodes LIMIT 1").Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	listen, target := 30000, 443
	layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "a", Ref: "server:" + nodeID, Role: "entry"}, {InstanceID: "b", Ref: "server:" + nodeID, Role: "exit"}}, Connections: []proxy.CanvasConnection{{ID: "forward", Source: "a", Target: "b", Type: "port_forward", ListenPort: &listen, TargetHost: "2001:db8::1", TargetPort: &target, SupportTCP: true, SupportUDP: true}}}
	repository := &proxy.Repository{Database: source.Database, Vault: source.ConfigurationVault}
	scheme, err := repository.SaveCanvasScheme(ctx, "", proxy.CanvasSchemeInput{Name: "new forwarding fields", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	created, err := source.Create(ctx, directory, "canvas_forward_001", "owner", testPassword)
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
	restored, err := (&proxy.Repository{Database: database}).CanvasScheme(ctx, scheme.ID)
	if err != nil || !reflect.DeepEqual(restored, scheme) {
		t.Fatalf("backup/verified restore changed canvas: %+v %v", restored, err)
	}
}
