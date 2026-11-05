package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Every field path a diagnostic can name is accepted by the verifier. The Clash
// options are emitted by the parser, so a verifier that refused them would hide the
// reason from the list and refuse a backup of the panel's own database.
func TestDiagnosticFieldsCoverParserFields(t *testing.T) {
	for _, field := range []string{fieldNone, fieldEntry, fieldCredential, fieldTLSInsecure, fieldMethod,
		fieldSecurity, fieldType, fieldNetwork, fieldWSOpts, fieldGRPCOpts, fieldQuery} {
		if !validField(field) {
			t.Fatalf("the parser field %q is refused by the verifier", field)
		}
	}
	for _, field := range []string{"body", "query.with space", "ws_opts", "Entry"} {
		if validField(field) {
			t.Fatalf("an arbitrary field path %q is accepted", field)
		}
	}
}

const clashUUID = "11111111-1111-1111-1111-111111111111"

// A Clash entry that fails for a Clash-specific reason is stored and read back with
// its field, both for a paste and for a fetched document: the reason survives the
// shared verifier, the row comparison and a list read.
func TestClashReasonsRoundTrip(t *testing.T) {
	for _, test := range []struct {
		name  string
		entry string
		field string
		code  string
	}{
		{"unknown option", `  - {name: bad, type: socks5, server: b.example.com, port: 1080, allow-insecure: true}`, fieldEntry, "unsupported_option"},
		{"ws without ws-opts", `  - {name: bad, type: vless, server: b.example.com, port: 443, uuid: "` + clashUUID + `", network: ws}`, fieldWSOpts, "unsupported_transport"},
		{"grpc without grpc-opts", `  - {name: bad, type: vless, server: b.example.com, port: 443, uuid: "` + clashUUID + `", network: grpc}`, fieldGRPCOpts, "unsupported_transport"},
		{"invalid network", `  - {name: bad, type: socks5, server: b.example.com, port: 1080, network: quic}`, fieldNetwork, "unsupported_transport"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _, _ := storeFixture(t)
			ctx := context.Background()
			content := "proxies:\n  - {name: kept, type: socks5, server: a.example.com, port: 1080}\n" + test.entry + "\n"
			key, saved, err := s.Save(ctx, "", Input{Name: "clash paste", Interval: 60, Content: content})
			if err != nil || saved.Outcome != "partial" || saved.Counts.Accepted != 1 || saved.Counts.Total == nil || *saved.Counts.Total != 2 {
				t.Fatalf("paste result: %+v %v", saved, err)
			}
			assertClashReason(t, saved, test.field, test.code)
			// The same document fetched by a linked source reports the same reason.
			s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, content) })
			link, _, err := s.Save(ctx, "", Input{Name: "clash link", Interval: 60, URL: "https://example.com/clash"})
			if err != nil {
				t.Fatal(err)
			}
			fetched, err := s.Refresh(ctx, link)
			if err != nil || fetched.Outcome != "partial" {
				t.Fatalf("refresh result: %+v %v", fetched, err)
			}
			assertClashReason(t, fetched, test.field, test.code)
			// Both rows keep a reason the list can read and a backup can verify.
			items, err := s.List(ctx)
			if err != nil || len(items) != 2 {
				t.Fatalf("list: %+v %v", items, err)
			}
			for _, item := range items {
				if item.LatestResult == nil {
					t.Fatalf("source %s lost its reason", item.ID)
				}
				assertClashReason(t, *item.LatestResult, test.field, test.code)
			}
			for _, id := range []string{key, link} {
				var stored, lastFetch, lastSuccess, status string
				if err = s.Database.QueryRow("SELECT latest_result_json,last_fetch_at,last_success_at,last_status FROM subscription_sources WHERE id=?", id).Scan(&stored, &lastFetch, &lastSuccess, &status); err != nil {
					t.Fatal(err)
				}
				if err = ValidateStoredResult(id, lastFetch, lastSuccess, status, []byte(stored)); err != nil {
					t.Fatalf("the panel's own stored result was refused for %s: %v", id, err)
				}
			}
		})
	}
}

// assertClashReason checks the one rejected entry and the field it names.
func assertClashReason(t *testing.T, result Result, field, code string) {
	t.Helper()
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics: %+v", result.Diagnostics)
	}
	item := result.Diagnostics[0]
	if item.Code != code || item.Field != field || item.Stage != diagParse || item.EntryIndex == nil || *item.EntryIndex != 1 {
		t.Fatalf("diagnostic %+v, want %s/%s at entry 1", item, code, field)
	}
	if raw, err := json.Marshal(result); err != nil || ValidateResult(raw) != nil {
		t.Fatalf("result with %s is not panel-shaped: %+v %v", field, result, err)
	}
}
