package events_test

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/events"
	"proxycontrol/internal/testutil/dbtest"
)

func TestSSERoleFilteringAndReauthentication(t *testing.T) {
	ctx := context.Background()
	db, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := &auth.Repository{Database: db}
	service := &auth.Service{Repository: repo, Settings: &system.Repository{Database: db}}
	for _, role := range []string{"Owner", "Viewer"} {
		if err := repo.CreateAdmin(ctx, auth.Admin{ID: role, Username: role, DisplayName: role, Role: role, Status: "active", PasswordHash: "fixture", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(role))
		if err := repo.CreateSession(ctx, hex.EncodeToString(hash[:]), role, "fixture", "local", time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	bus := &events.Bus{}
	server := httptest.NewServer(bus.Handler(&auth.Handler{Service: service}))
	defer server.Close()
	for _, role := range []string{"Owner", "Viewer"} {
		t.Run(role, func(t *testing.T) {
			requestCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(requestCtx, "GET", server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: "proxy_session", Value: role})
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("status=%d", resp.StatusCode)
			}
			scanner := bufio.NewScanner(resp.Body)
			readFrame := func() string {
				t.Helper()
				var lines []string
				for scanner.Scan() {
					if scanner.Text() == "" {
						return strings.Join(lines, "\n")
					}
					lines = append(lines, scanner.Text())
				}
				t.Fatalf("stream ended: %v", scanner.Err())
				return ""
			}
			if frame := readFrame(); frame != ": connected" {
				t.Fatal(frame)
			}
			check := func(viewer bool) {
				t.Helper()
				for _, tc := range []struct {
					kind    string
					data    any
					allowed bool
				}{
					{"system.alert", map[string]any{"category": "user_access", "userId": "private"}, false},
					{"system.alert", map[string]string{"alert": "certificate_deploy_failed"}, false},
					{"deployment_status", map[string]int{"revision": 1}, true},
					{"dashboard_status", map[string]bool{"changed": true}, true},
					{"notification", map[string]any{"id": 1, "kind": "node_offline"}, true},
					{"notification", map[string]any{"id": 2, "kind": "cpu"}, true},
					{"notification", map[string]any{"id": 3, "kind": "memory"}, true},
					{"notification", map[string]any{"id": 4, "kind": "disk"}, true},
					{"notification", map[string]any{"id": 5, "kind": "system_alert"}, false},
					{"notification", map[string]any{"id": 6, "kind": "traffic"}, false},
					{"notification", map[string]any{"id": 7}, false},
					{"backup_status", map[string]bool{"changed": true}, false},
				} {
					bus.Publish(tc.kind, tc.data)
					bus.Publish("node_status", map[string]string{"nodeId": "sentinel"})
					var frames []string
					for {
						frame := readFrame()
						if strings.Contains(frame, "event: node_status\n") {
							break
						}
						frames = append(frames, frame)
					}
					want := 0
					if !viewer || tc.allowed {
						want = 1
					}
					if len(frames) != want {
						t.Errorf("viewer=%v event=%s data=%v frames=%v", viewer, tc.kind, tc.data, frames)
					}
				}
			}
			check(role == "Viewer")
			if role == "Owner" {
				if _, err := db.Exec("UPDATE admins SET role='Viewer' WHERE id='Owner'"); err != nil {
					t.Fatal(err)
				}
				if frame := readFrame(); frame != ": keepalive" {
					t.Fatal(frame)
				}
				check(true)
			}
		})
	}
}
