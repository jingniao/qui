package node

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/platform/vault"
)

func groupImportRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	r, ctx := deletionRepository(t)
	v, err := vault.FromKey(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	r.Vault = v
	for _, source := range []struct{ id, url string }{{"content", ""}, {"other", ""}, {"url", "https://example.com/subscription"}} {
		sealed, err := v.Seal([]byte(source.url))
		if err != nil {
			t.Fatal(err)
		}
		insert(t, r, ctx, "INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,created_at,updated_at) VALUES(?,?,?,60,'now','now')", source.id, source.id, sealed)
	}
	for _, item := range []struct{ id, source, name string }{
		{"stable-one", "content", "same"}, {"stable-two", "content", "same"}, {"foreign", "other", "same"},
		{"url-one", "url", "region:node"}, {"url-two", "url", "region|node"}, {"url-three", "url", "id:stable-one"},
	} {
		insert(t, r, ctx, "INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES(?,?,?,'socks5',x'01','now')", item.id, item.source, item.name)
	}
	return r, ctx
}

func TestGroupContentImportsUseStableIDs(t *testing.T) {
	r, ctx := groupImportRepository(t)
	input := GroupInput{Name: "stable members", Purpose: "subscription", Enabled: true, SubscriptionVisible: true,
		Imports: []GroupImport{{SourceID: "content", ProxyKey: "id:stable-one"}, {SourceID: "content", ProxyKey: "id:stable-two"}}}
	g, err := r.SaveGroup(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Group(ctx, g.ID)
	if err != nil || !reflect.DeepEqual(got.Imports, input.Imports) {
		t.Fatalf("saved stable members = %+v, error = %v", got.Imports, err)
	}
	insert(t, r, ctx, "UPDATE imported_proxies SET name='renamed:node|new' WHERE id='stable-one'")
	if _, err := r.SaveGroup(ctx, g.ID, input); err != nil {
		t.Fatalf("rename broke stable membership: %v", err)
	}
	for _, key := range []string{"", "same|socks5", "id:", "id:missing", "id:foreign", "id:url-one", "stable-one", "id:stable-one|socks5"} {
		t.Run(key, func(t *testing.T) {
			invalid := input
			invalid.Imports = []GroupImport{{SourceID: "content", ProxyKey: key}}
			if _, err := r.SaveGroup(ctx, g.ID, invalid); err == nil {
				t.Fatalf("invalid content key %q accepted", key)
			}
			got, err := r.Group(ctx, g.ID)
			if err != nil || !reflect.DeepEqual(got.Imports, input.Imports) {
				t.Fatalf("failed write changed members: %+v, %v", got.Imports, err)
			}
		})
	}
}

func TestGroupURLImportsKeepLegacyKeys(t *testing.T) {
	r, ctx := groupImportRepository(t)
	for _, key := range []string{"", "region:node|socks5", "region|node|socks5", "id:stable-one|socks5"} {
		t.Run(key, func(t *testing.T) {
			input := GroupInput{Name: "url " + key, Purpose: "subscription", Imports: []GroupImport{{SourceID: "url", ProxyKey: key}}}
			g, err := r.SaveGroup(ctx, "", input)
			if err != nil {
				t.Fatal(err)
			}
			got, err := r.Group(ctx, g.ID)
			if err != nil || !reflect.DeepEqual(got.Imports, input.Imports) {
				t.Fatalf("URL members = %+v, error = %v", got.Imports, err)
			}
		})
	}
	for _, key := range []string{"id:url-one", "missing|socks5"} {
		if _, err := r.SaveGroup(ctx, "", GroupInput{Name: "invalid url", Purpose: "subscription", Imports: []GroupImport{{SourceID: "url", ProxyKey: key}}}); err == nil {
			t.Fatalf("invalid URL member %q accepted", key)
		}
	}
}

func TestGroupReadsLegacyContentMembersUnchanged(t *testing.T) {
	r, ctx := groupImportRepository(t)
	g, err := r.SaveGroup(ctx, "", GroupInput{Name: "legacy", Purpose: "subscription"})
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupImport{{SourceID: "content", ProxyKey: ""}, {SourceID: "content", ProxyKey: "missing|socks5"}, {SourceID: "content", ProxyKey: "same|socks5"}}
	for _, item := range want {
		insert(t, r, ctx, "INSERT INTO node_group_imports VALUES(?,?,?)", g.ID, item.SourceID, item.ProxyKey)
	}
	got, err := r.Group(ctx, g.ID)
	if err != nil || !reflect.DeepEqual(got.Imports, want) {
		t.Fatalf("legacy members = %+v, error = %v", got.Imports, err)
	}
}

func TestGroupImportLimit1000(t *testing.T) {
	r, ctx := groupImportRepository(t)
	input := GroupInput{Name: "large import group", Purpose: "subscription"}
	for i := 0; i < 1001; i++ {
		id := fmt.Sprintf("limit-%04d", i)
		insert(t, r, ctx, "INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES(?,'content',?,'socks5',x'01','now')", id, id)
		input.Imports = append(input.Imports, GroupImport{SourceID: "content", ProxyKey: "id:" + id})
	}
	valid := input
	valid.Imports = input.Imports[:1000]
	g, err := r.SaveGroup(ctx, "", valid)
	if err != nil || len(g.Imports) != 1000 {
		t.Fatalf("1000 imports not saved: count=%d, error=%v", len(g.Imports), err)
	}
	if _, err := r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("1001 imports accepted")
	}
	got, err := r.Group(ctx, g.ID)
	if err != nil || !reflect.DeepEqual(got.Imports, valid.Imports) {
		t.Fatalf("rejected oversized save changed members: count=%d, error=%v", len(got.Imports), err)
	}
}

func TestGroupImportSourceDecryptionFailure(t *testing.T) {
	r, ctx := groupImportRepository(t)
	insert(t, r, ctx, "UPDATE subscription_sources SET url_encrypted=x'01' WHERE id='content'")
	_, wantErr := r.Vault.Open([]byte{1})
	if wantErr == nil {
		t.Fatal("invalid encrypted fixture accepted")
	}
	for _, key := range []string{"", "same|socks5", "id:stable-one"} {
		_, err := r.SaveGroup(ctx, "", GroupInput{Name: "broken source", Purpose: "subscription", Imports: []GroupImport{{SourceID: "content", ProxyKey: key}}})
		if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
			t.Fatalf("source decryption error lost for %q: %v", key, err)
		}
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_groups WHERE name='broken source'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed create persisted group: count=%d, error=%v", count, err)
	}
}

func TestGroupEntriesRepositoryAndDerivedServers(t *testing.T) {
	r, id := latencyFixture(t)
	ctx := context.Background()
	for _, q := range []string{
		"INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('a','A',?,'socks5','0.0.0.0',10001,1,1,0,'direct','[]',x'01','now')",
		"INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('b','B',?,'socks5','0.0.0.0',10002,1,1,0,'direct','[]',x'01','now')",
	} {
		if _, err := r.Database.Exec(q, id); err != nil {
			t.Fatal(err)
		}
	}
	input := GroupInput{Name: "entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: []string{"a"}}
	g, err := r.SaveGroup(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.NodeIDs) != 1 || g.NodeIDs[0] != id || len(g.InboundIDs) != 1 || g.Imports == nil || g.GrantedUserIDs == nil {
		t.Fatal(g)
	}
	input.ServerIDs = []string{id}
	input.InboundIDs = []string{"a", "b"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Database.Exec("DELETE FROM inbounds WHERE id='a'"); err != nil {
		t.Fatal(err)
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.NodeIDs) != 1 || len(g.InboundIDs) != 1 {
		t.Fatal(g, err)
	}
	if _, err = r.Database.Exec("DELETE FROM inbounds WHERE id='b'"); err != nil {
		t.Fatal(err)
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.NodeIDs) != 1 || len(g.ServerIDs) != 1 || len(g.InboundIDs) != 0 {
		t.Fatal("inbound cascade removed explicit server", g, err)
	}
	if _, err = r.Database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('b','B',?,'socks5','0.0.0.0',10002,1,1,0,'direct','[]',x'01','now')", id); err != nil {
		t.Fatal(err)
	}
	input.ServerIDs = []string{}
	input.InboundIDs = []string{}
	input.NodeIDs = []string{id} // Explicit entries override legacy servers.
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.NodeIDs) != 0 {
		t.Fatal(g, err)
	}
	input.InboundIDs = nil
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.InboundIDs) != 1 {
		t.Fatal(g, err)
	}
	input.InboundIDs = []string{"unknown"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("unknown inbound accepted")
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.InboundIDs) != 1 {
		t.Fatal("failed save changed membership", g, err)
	}
	input.InboundIDs = []string{"b", "b"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestExplicitServersUnionLegacyAndDeletion(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "explicit")
	input := GroupInput{Name: "servers", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, ServerIDs: []string{n.ID}}
	g, err := r.SaveGroup(ctx, "", input)
	if err != nil || len(g.ServerIDs) != 1 || len(g.NodeIDs) != 1 || len(g.InboundIDs) != 0 {
		t.Fatal(g, err)
	}
	// Explicit-only membership is a policy scope, never an inbound grant.
	groups, err := r.GroupIDsForNode(ctx, n.ID)
	if err != nil || len(groups) != 1 || groups[0] != g.ID {
		t.Fatal(groups, err)
	}
	input.ServerIDs = nil
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.ServerIDs) != 1 {
		t.Fatal("omitted explicit servers lost", g, err)
	}
	input.NodeIDs = []string{} // Exact payload shape of bulk leave.
	g, err = r.SaveGroup(ctx, g.ID, input)
	if err != nil || len(g.NodeIDs) != 0 {
		t.Fatal("bulk leave retained explicit server", g, err)
	}
	for _, purpose := range []string{"relay", "egress"} {
		input.Purpose = purpose
		input.NodeIDs = []string{n.ID}
		g, err = r.SaveGroup(ctx, g.ID, input)
		if err != nil || len(g.ServerIDs) != 1 || len(g.InboundIDs) != 0 || g.SubscriptionVisible {
			t.Fatal(g, err)
		}
	}
	input.ServerIDs = []string{"missing"}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("unknown server accepted")
	}
	input.ServerIDs = []string{n.ID, n.ID}
	if _, err = r.SaveGroup(ctx, g.ID, input); err == nil {
		t.Fatal("duplicate server accepted")
	}
	if err = r.Delete(ctx, n.ID, nil, nil, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	g, err = r.Group(ctx, g.ID)
	if err != nil || len(g.ServerIDs) != 0 || len(g.NodeIDs) != 0 {
		t.Fatal("logical deletion retained server", g, err)
	}
}
