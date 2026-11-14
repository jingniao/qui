package published

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestEndpointNamesUsesSubscriptionNames(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO nodes(id,name,status,created_at) VALUES('n','Edge','online','now')`); err != nil {
		t.Fatal(err)
	}
	for _, in := range []struct {
		id, protocol, listen    string
		port, visible, internal int
	}{
		{"a", "vless", "0.0.0.0", 443, 1, 0},
		{"b", "vless", "::", 8443, 1, 0},
		{"c", "trojan", "0.0.0.0", 9443, 1, 0},
		{"hidden", "socks5", "0.0.0.0", 1080, 0, 0},
		{"internal", "http", "0.0.0.0", 8080, 1, 1},
		{"loopback", "shadowsocks", "127.0.0.1", 8488, 1, 0},
		{"forward", "forward", "0.0.0.0", 9000, 1, 0},
	} {
		if _, err := database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES(?,?,'n',?,?,?,1,?,?,'direct','[]',x'00','now')`, in.id, in.id, in.protocol, in.listen, in.port, in.visible, in.internal); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	names, err := (EndpointNameStore{}).EndpointNames(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Edge · trojan", "Edge · vless", "Edge · vless · 443", "Edge · vless · 8443"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestEndpointNamesIncludesRunningRevisionAndRelays(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	key, err := vault.FromKey(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO nodes(id,name,status,created_at) VALUES('n','Edge','online','now')`); err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "n", Logging: desiredstate.Logging{DomainOnly: true},
		Credentials: []desiredstate.Credential{{ID: "credential", Protocol: "socks5", Username: "alice", Secret: "secret"}, {ID: "http-credential", Protocol: "http", Username: "alice", Secret: "secret"}},
		Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
	for _, in := range []struct {
		id, protocol, credential string
		port, enabled, visible   int
	}{
		{"a", "socks5", "credential", 1080, 0, 1},
		{"b", "socks5", "credential", 1081, 0, 1},
		{"c", "http", "http-credential", 8080, 1, 1},
		{"hidden", "socks5", "credential", 1082, 0, 0},
	} {
		if _, err := database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES(?,?,'n',?,'0.0.0.0',?,?,?,0,'direct','[]',x'00','now')`, in.id, in.id, in.protocol, in.port, in.enabled, in.visible); err != nil {
			t.Fatal(err)
		}
		state.Inbounds = append(state.Inbounds, desiredstate.Inbound{ID: in.id, Protocol: in.protocol, Listen: "0.0.0.0", Port: in.port, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", CredentialIDs: []string{in.credential}})
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := desiredstate.Decode(data); err != nil {
		t.Fatal(err)
	}
	sealed, err := key.Seal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'owner','published','success','now',?)`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO node_runtime_state(node_id,revision) VALUES('n',1)`); err != nil {
		t.Fatal(err)
	}
	relay, err := json.Marshal(Relay{ID: "relay", InboundID: "a", Entry: proxy.Ref{ID: "n", Name: "Entry"}, Server: "203.0.113.1", Port: 9000, Network: "tcp", Enabled: true, Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_inbound_id,subscription_visible,enabled,status,created_at,updated_at,published_snapshot) VALUES('relay','relay','n',9000,'auto','tcp','linked','a',1,1,'published','now','now',?)`, string(relay)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE inbounds SET port=2080 WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	names, err := (EndpointNameStore{Vault: key}).EndpointNames(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Edge · http", "Edge · socks5", "Edge · socks5 · 1080", "Edge · socks5 · 1081", "Edge · socks5 · 经 Entry"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}
