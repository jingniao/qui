package importer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"proxycontrol/internal/panel/subscription/source"
)

const identityContent = "socks5://user:pass@example.com:1080#Original"

// These are the cache bytes before ImportKind was added to Origin.
const identityCacheJSON = `{"DialerProxy":"","ClientChain":"","Origin":{"Relayed":false,"Source":"","Ref":"","Node":{"ID":"","Name":""},"Labels":null},"Name":"Original","Server":"example.com","Port":1080,"Protocol":"socks5","Credential":{"Username":"user","Password":"pass","PublicKey":""},"Flow":"","Method":"","Security":"","TLS":null,"Transport":null,"QUIC":null,"WireGuard":null}`

func TestContentImportIDsAreRandomAndPersist(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	seen := map[string]bool{}
	for _, name := range []string{"Original", "Original", "Renamed|:节点"} {
		key, _, err := s.Save(ctx, "", Input{Name: "batch", Interval: 60, Content: "socks5://user:pass@example.com:1080#" + name + "\nsocks5://user:pass@other.example.com:1081#Other"})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.Database.Query("SELECT id FROM imported_proxies WHERE source_id=?", key)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for rows.Next() {
			var pid string
			if err := rows.Scan(&pid); err != nil {
				t.Fatal(err)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(pid)
			if err != nil || len(decoded) != 32 || seen[pid] {
				t.Errorf("node ID %q is not a fresh id.New identity", pid)
			}
			seen[pid] = true
			ids = append(ids, pid)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if len(ids) != 2 {
			t.Fatalf("new content batch stored %d nodes, want 2", len(ids))
		}
		before := identitySnapshot(t, s, key, false)
		if _, _, err = s.Save(ctx, key, Input{Name: "metadata edited", Interval: 120}); err != nil {
			t.Fatal(err)
		}
		if after := identitySnapshot(t, s, key, false); after != before {
			t.Fatal("metadata edit changed node identities or encrypted payloads")
		}
	}
}

func TestURLImportPreservesCachedJSONAndHash(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, identityContent+"\n"+identityContent)
	})
	key, _, err := s.Save(ctx, "", Input{Name: "airport", URL: "https://example.com/fixture", Interval: 60})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := s.Refresh(ctx, key)
		if err != nil || result.Counts.Accepted != 1 || result.Counts.Duplicates == nil || *result.Counts.Duplicates != 1 {
			t.Fatalf("refresh: %+v, %v", result, err)
		}
		var pid string
		var sealed []byte
		if err := s.Database.QueryRow("SELECT id,payload_encrypted FROM imported_proxies WHERE source_id=?", key).Scan(&pid, &sealed); err != nil {
			t.Fatal(err)
		}
		raw, err := s.Vault.Open(sealed)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != identityCacheJSON || pid != key+"-ebd268dec1e814e384ed7d3280d21d1eb89f892258d7b553e906621e59d0897d" {
			t.Fatalf("URL cache bytes or content ID changed: %s, %s", raw, pid)
		}
	}
}

func TestSaveRejectsContentReplacementAndKindChanges(t *testing.T) {
	for _, test := range []struct {
		name, kind, content, url string
	}{
		{"content replacement", "content", identityContent, ""},
		{"content to url", "content", "", "https://example.com/next"},
		{"url to content", "url", identityContent, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _, _ := storeFixture(t)
			ctx := context.Background()
			in := Input{Name: "original", Interval: 60, Content: identityContent}
			if test.kind == "url" {
				in.Content, in.URL = "", "https://example.com/fixture"
			}
			key, _, err := s.Save(ctx, "", in)
			if err != nil {
				t.Fatal(err)
			}
			before := identitySnapshot(t, s, key, true)
			wantErr := ErrSourceKindChange
			if test.kind == "content" && test.content != "" {
				wantErr = ErrContentReplace
			}
			if _, _, err := s.Save(ctx, key, Input{Name: "changed", Interval: 120, Content: test.content, URL: test.url}); !errors.Is(err, wantErr) || !errors.Is(err, ErrInput) {
				t.Fatalf("save error = %v, want %v", err, wantErr)
			}
			w := saveHTTP(s, key, Input{Name: "changed", Interval: 120, Content: test.content, URL: test.url})
			message := "不能切换导入来源类型，请新增导入来源"
			if wantErr == ErrContentReplace {
				message = "单独导入不支持整批替换，请新增导入节点"
			}
			if code, gotMessage, _ := errorResult(t, w); w.Code != http.StatusBadRequest || code != "import_invalid_input" || gotMessage != message || bytes.Contains(w.Body.Bytes(), []byte(`"result"`)) {
				t.Fatalf("refused save response: %d %s", w.Code, w.Body.String())
			}
			if after := identitySnapshot(t, s, key, true); after != before {
				t.Fatal("refused save changed source or cached nodes")
			}
		})
	}
}

func TestReplaceRejectsContentSource(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	key, _, err := s.Save(ctx, "", Input{Name: "content", Interval: 60, Content: identityContent})
	if err != nil {
		t.Fatal(err)
	}
	before := identitySnapshot(t, s, key, true)
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = s.replace(ctx, tx, key, nil, "later"); !errors.Is(err, ErrContentReplace) {
		t.Fatalf("replace error = %v, want ErrContentReplace", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if after := identitySnapshot(t, s, key, true); after != before {
		t.Fatal("replace deleted content nodes before refusing")
	}
}

func TestProxiesSetsRuntimeImportKind(t *testing.T) {
	for _, kind := range []string{"content", "url"} {
		t.Run(kind, func(t *testing.T) {
			s, userID, _ := storeFixture(t)
			ctx := context.Background()
			in := Input{Name: "source", Interval: 60, Content: identityContent}
			if kind == "url" {
				in.Content, in.URL = "", "https://example.com/fixture"
				s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, identityContent) })
			}
			key, _, err := s.Save(ctx, "", in)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "url" {
				if _, err = s.Refresh(ctx, key); err != nil {
					t.Fatal(err)
				}
			}
			grantImport(t, s, userID, key, "")
			entries, _, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}})
			if err != nil || len(entries) != 1 {
				t.Fatalf("proxies: %+v, %v", entries, err)
			}
			if entries[0].Origin.ImportKind != kind {
				t.Fatalf("runtime import kind missing: %+v", entries[0].Origin)
			}
			raw, err := json.Marshal(entries[0])
			if err != nil || bytes.Contains(raw, []byte("ImportKind")) {
				t.Fatalf("runtime kind leaked into JSON: %s, %v", raw, err)
			}
		})
	}
}

func TestSaveRejectsUnreadableSourceKind(t *testing.T) {
	s, _, _ := storeFixture(t)
	ctx := context.Background()
	key, _, err := s.Save(ctx, "", Input{Name: "content", Interval: 60, Content: identityContent})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec("UPDATE subscription_sources SET url_encrypted=x'00' WHERE id=?", key); err != nil {
		t.Fatal(err)
	}
	before := identitySnapshot(t, s, key, true)
	_, vaultErr := s.Vault.Open([]byte{0})
	if _, _, err = s.Save(ctx, key, Input{Name: "changed", Interval: 60, URL: "https://example.com/fixture"}); err == nil || err.Error() != vaultErr.Error() {
		t.Fatalf("source kind error = %v, want original vault error %v", err, vaultErr)
	}
	if after := identitySnapshot(t, s, key, true); after != before {
		t.Fatal("unreadable source was changed")
	}
}

func TestSourceKindStrictPreservesVaultErrors(t *testing.T) {
	s, _, _ := storeFixture(t)
	for _, test := range []struct{ raw, kind string }{{"", "content"}, {"https://example.com/fixture", "url"}} {
		sealed, err := s.Vault.Seal([]byte(test.raw))
		if err != nil {
			t.Fatal(err)
		}
		if kind, err := s.sourceKindStrict(sealed); err != nil || kind != test.kind {
			t.Fatalf("strict kind = %q, %v; want %q", kind, err, test.kind)
		}
	}
	_, original := s.Vault.Open([]byte{0})
	if kind, err := s.sourceKindStrict([]byte{0}); err == nil || err.Error() != original.Error() || kind != "" {
		t.Fatalf("strict kind hid a vault error: %q, %v", kind, err)
	}
	if kind := s.sourceKind([]byte{0}); kind != "url" {
		t.Fatalf("legacy sourceKind behavior changed: %q", kind)
	}
}

func identitySnapshot(t *testing.T, s *Store, key string, includeSource bool) string {
	t.Helper()
	var nodes string
	if err := s.Database.QueryRow(`SELECT COALESCE(group_concat(row), '') FROM
		(SELECT json_array(id,source_id,name,protocol,hex(payload_encrypted),fetched_at,revision) AS row FROM imported_proxies WHERE source_id=? ORDER BY id)`, key).Scan(&nodes); err != nil {
		t.Fatal(err)
	}
	if !includeSource {
		return nodes
	}
	var source string
	if err := s.Database.QueryRow(`SELECT json_array(id,name,hex(url_encrypted),interval_seconds,last_fetch_at,last_success_at,last_status,last_error,hex(latest_result_json),created_at,updated_at) FROM subscription_sources WHERE id=?`, key).Scan(&source); err != nil {
		t.Fatal(err)
	}
	return source + nodes
}
