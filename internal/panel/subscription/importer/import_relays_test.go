package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
)

func importRelayFixture(t *testing.T, s *Store, userID, pid, protocol, network string) string {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO nodes(id,name,public_ip,connect_host,status,created_at) VALUES('entry','Entry','192.0.2.1','entry.example','online','now')`,
		`INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('relay-group','relay group','subscription',1,1,'now')`,
	} {
		if _, err := s.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := json.Marshal(map[string]any{"ID": "rule", "importId": pid, "Entry": proxy.Ref{ID: "entry", Name: "Entry"}, "Server": "snapshot.example", "Port": 24000, "Network": network, "DestinationAddress": "original.example", "DestinationPort": 443, "Enabled": true, "Visible": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec(`INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_import_id,subscription_visible,enabled,status,published_at,published_snapshot,created_at,updated_at) VALUES('rule','Relay current','entry',24000,'manual',?,'import',?,1,1,'published','now',?,'now','now')`, network, pid, string(snapshot)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec(`INSERT INTO node_group_relays(group_id,rule_id) VALUES('relay-group','rule')`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec(`INSERT INTO user_node_group_grants(user_id,group_id) VALUES(?,'relay-group')`, userID); err != nil {
		t.Fatal(err)
	}
	return "relay-group"
}

func TestImportRelaySubscriptionUsesSnapshotAndCurrentPublicParameters(t *testing.T) {
	for _, protocol := range []string{"vless", "hysteria2"} {
		t.Run(protocol, func(t *testing.T) {
			s, a, b := storeFixture(t)
			ctx := context.Background()
			key, _, err := s.Save(ctx, "", Input{Name: "import", Content: identityContent, Interval: 60})
			if err != nil {
				t.Fatal(err)
			}
			var pid string
			if err = s.Database.QueryRow(`SELECT id FROM imported_proxies WHERE source_id=?`, key).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			p := proxy.Proxy{Name: "Original", Server: "original.example", Port: 443, Protocol: protocol, Security: "tls", Credential: proxy.Credential{Password: "secret"}, TLS: &proxy.TLS{}, QUIC: &proxy.QUIC{HopPorts: "20000-20010", HopInterval: 30}}
			raw, _ := json.Marshal(p)
			sealed, err := s.Vault.Seal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Database.Exec(`UPDATE imported_proxies SET payload_encrypted=?,protocol=? WHERE id=?`, sealed, protocol, pid); err != nil {
				t.Fatal(err)
			}
			group := importRelayFixture(t, s, a, pid, protocol, "both")
			viewer := source.Viewer{UserID: a, NodeGroupIDs: []string{group}, AllowedProtocols: []string{protocol}}
			endpoints, _, err := s.Proxies(ctx, viewer)
			if err != nil || len(endpoints) != 1 {
				t.Fatalf("endpoints=%+v err=%v", endpoints, err)
			}
			got := endpoints[0]
			if got.Server != "entry.example" || got.Port != 24000 || got.Name != "Relay current" || got.TLS.ServerName != "original.example" || got.Credential.Password != "secret" || !got.Origin.Relayed || got.Origin.Ref != "rule" {
				t.Fatalf("endpoint=%+v TLS=%+v", got, got.TLS)
			}
			if got.QUIC.HopPorts != "" || got.QUIC.HopInterval != 0 {
				t.Fatal("hop retained")
			}
			viewer.UserID = b
			if got, _, err := s.Proxies(ctx, viewer); err != nil || len(got) != 0 {
				t.Fatalf("unauthorized=%+v err=%v", got, err)
			}
			linked, err := (&published.RelayStore{Database: s.Database}).Relays(ctx)
			if err != nil || len(linked) != 0 {
				t.Fatalf("published import leak=%+v err=%v", linked, err)
			}
		})
	}
}

func TestImportRelayRefreshRebindsIdentityAndInvalidatesOnlyAddressChanges(t *testing.T) {
	s, a, _ := storeFixture(t)
	ctx := context.Background()
	content := identityContent
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, content) })
	src, _, err := s.Save(ctx, "", Input{Name: "URL", URL: "https://example.com/sub", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Refresh(ctx, src); err != nil {
		t.Fatal(err)
	}
	var pid string
	if err = s.Database.QueryRow(`SELECT id FROM imported_proxies WHERE source_id=?`, src).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	importRelayFixture(t, s, a, pid, "socks5", "both")
	if _, err = s.Database.Exec(`INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,target_mode,target_import_id,subscription_visible,enabled,status,created_at,updated_at) VALUES('draft','Draft','entry',24001,'manual','both','import',?,1,1,'draft','now','now')`, pid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		content string
		status  string
		version int
	}{{"socks5://user:newpass@example.com:1080#Original", "published", 1}, {"socks5://user:newpass@changed.example:1081#Original", "needs_publish", 2}, {"socks5://user:newpass@other.example:1081#Different", "needs_publish", 3}} {
		content = change.content
		if _, err = s.Refresh(ctx, src); err != nil {
			t.Fatal(err)
		}
		var status string
		var version int
		var target *string
		var snapshot string
		if err = s.Database.QueryRow(`SELECT status,draft_version,target_import_id,published_snapshot FROM tunnel_rules WHERE id='rule'`).Scan(&status, &version, &target, &snapshot); err != nil {
			t.Fatal(err)
		}
		if status != change.status || version != change.version {
			t.Fatalf("status=%s version=%d", status, version)
		}
		var snap struct {
			ImportID           string `json:"importId"`
			DestinationAddress string
		}
		if err = json.Unmarshal([]byte(snapshot), &snap); err != nil {
			t.Fatal(err)
		}
		if snap.DestinationAddress != "original.example" {
			t.Fatal("runtime snapshot changed")
		}
		if target != nil && snap.ImportID != *target {
			t.Fatal("snapshot identity not rebound")
		}
		if err = s.Database.QueryRow(`SELECT status FROM tunnel_rules WHERE id='draft'`).Scan(&status); err != nil || status != "draft" {
			t.Fatalf("draft status=%s err=%v", status, err)
		}
	}
}

func TestImportRuleDeletionPreviewAndConflict(t *testing.T) {
	s, a, _ := storeFixture(t)
	ctx := context.Background()
	src, _, err := s.Save(ctx, "", Input{Name: "content", Content: identityContent, Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	var pid string
	if err = s.Database.QueryRow(`SELECT id FROM imported_proxies WHERE source_id=?`, src).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	importRelayFixture(t, s, a, pid, "socks5", "both")
	preview, err := s.DeletionPreview(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(preview)
	var body struct{ Rules []struct{ ID, Name string } }
	if err = json.Unmarshal(raw, &body); err != nil || len(body.Rules) != 1 || body.Rules[0].ID != "rule" {
		t.Fatalf("preview=%s err=%v", raw, err)
	}
	err = s.DeleteProxy(ctx, pid, DeleteInput{ExpectedRevision: 1, RemoveReferences: true})
	var conflict *DeletionConflict
	if !errors.As(err, &conflict) || conflict.Code != "import_in_use_by_rule" {
		t.Fatalf("conflict=%v", err)
	}
}
