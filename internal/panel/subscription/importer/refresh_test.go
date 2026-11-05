package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/platform/httpapi"
)

func TestRefreshFailureResultAndPersistence(t *testing.T) {
	for _, parseFailure := range []bool{false, true} {
		for _, writeFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("parse=%t/writeFailure=%t", parseFailure, writeFailure), func(t *testing.T) {
				s, userID, _ := storeFixture(t)
				ctx := context.Background()
				const content = "socks5://fixture:private-fixture@example.com:1080#cached"
				status, body := http.StatusOK, content
				s.Fetcher, _, _ = fetchFixture(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
					fmt.Fprint(w, body)
				})
				key, err := s.Save(ctx, "", Input{Name: "fixture", URL: "https://example.com/fixture?token=private-fixture", Interval: 60})
				if err != nil {
					t.Fatal(err)
				}
				grantImport(t, s, userID, key, "")
				if err := s.Refresh(ctx, key); err != nil {
					t.Fatal(err)
				}
				wantErr := ErrFetch
				status, body = http.StatusBadGateway, "private-fixture upstream failure"
				if parseFailure {
					wantErr = ErrParse
					status = http.StatusOK
				}
				if writeFailure {
					_, err = s.Database.Exec(`CREATE TRIGGER reject_refresh_status BEFORE UPDATE OF last_status ON subscription_sources
 WHEN NEW.last_status='failed' BEGIN SELECT RAISE(ABORT, 'fixture status write failed'); END`)
					if err != nil {
						t.Fatal(err)
					}
				}
				err = s.Refresh(ctx, key)
				if writeFailure {
					if err == nil || errors.Is(err, ErrFetch) || errors.Is(err, ErrParse) || !strings.Contains(err.Error(), "fixture status write failed") {
						t.Fatalf("persistence error hidden: %v", err)
					}
				} else if !errors.Is(err, wantErr) {
					t.Fatalf("refresh error = %v, want %v", err, wantErr)
				}
				items, err := s.List(ctx)
				if err != nil || len(items) != 1 {
					t.Fatalf("list: %v, %v", items, err)
				}
				wantStatus, wantCount := "failed", 0
				if writeFailure {
					wantStatus, wantCount = "ok", 1
				}
				if items[0].Status != wantStatus || items[0].LastFetch == "" || !writeFailure && items[0].Error == "" {
					t.Fatalf("unexpected source status: %+v", items[0])
				}
				proxies, warnings, err := s.Proxies(ctx, source.Viewer{UserID: userID, AllowedProtocols: []string{"socks5"}})
				if err != nil || len(proxies) != wantCount || len(warnings) == 0 {
					t.Fatalf("subscription proxies=%d warnings=%v err=%v", len(proxies), warnings, err)
				}
				var cached int
				if err := s.Database.QueryRow("SELECT count(*) FROM imported_proxies WHERE source_id=?", key).Scan(&cached); err != nil || cached != 1 {
					t.Fatalf("cached nodes changed: %d, %v", cached, err)
				}

				// Exercise the actual refresh handler without bypassing the store result.
				r := httptest.NewRequest(http.MethodPost, "/", nil)
				r.SetPathValue("id", key)
				w := httptest.NewRecorder()
				(&Handler{Store: s}).refresh(w, r)
				var response httpapi.ErrorBody
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				wantCode := "import_refresh_failed"
				wantMessage := "订阅来源抓取或解析失败，该来源的节点已从订阅中排除"
				if writeFailure {
					wantCode, wantMessage = "import_unavailable", "导入操作失败，请检查输入与来源状态"
				}
				if w.Code != http.StatusBadRequest || response.Error.Code != wantCode || response.Error.Message != wantMessage {
					t.Fatalf("refresh response: %d %s", w.Code, w.Body.String())
				}
				if strings.Contains(w.Body.String(), "private-fixture") || strings.Contains(w.Body.String(), "fixture status write failed") {
					t.Fatal("refresh response leaked source or persistence details")
				}
				// A later successful refresh still recovers the source and returns 204.
				status, body = http.StatusOK, content
				w = httptest.NewRecorder()
				(&Handler{Store: s}).refresh(w, r)
				if w.Code != http.StatusNoContent {
					t.Fatalf("recovery: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}
