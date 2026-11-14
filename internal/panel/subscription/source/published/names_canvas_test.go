package published_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	canvas "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/testutil/dbtest"
)

func TestEndpointNamesUsesCanvasEntryBusinessName(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	database, err := dbtest.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	reader, err := dbtest.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, query := range []string{
		`INSERT INTO nodes(id,name,status,created_at) VALUES('n','Edge','online','now')`,
		`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('i','Business entry','n','socks5','0.0.0.0',1080,1,1,0,'direct','[]',x'00','now')`,
		`INSERT INTO canvas_entry_bindings(id,instance_id,inbound_id,mode,state,updated_at) VALUES('binding','instance','i','create','detached','now')`,
	} {
		if _, err := database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	names, err := (published.EndpointNameStore{CanvasEntries: &canvas.Repository{Database: reader}}).EndpointNames(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"Business entry"}) {
		t.Fatalf("names = %v", names)
	}
}
