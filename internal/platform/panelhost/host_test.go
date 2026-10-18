package panelhost

import (
	"bytes"
	"context"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/restoretasks"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestManagedHTTPRestoreRebuildsServicePreservesUsageAndRevokesSession(t *testing.T) {
	for _, failCandidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "candidate-failure"}[failCandidate], func(t *testing.T) {
			root := t.TempDir()
			c := config.DefaultPanel()
			c.Database = filepath.Join(root, "panel.db")
			c.NodeIdentityDirectory = filepath.Join(root, "identity")
			c.ShutdownTimeout = time.Second
			port, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			c.HTTPAddress = port.Addr().String()
			port.Close()
			c.GRPCAddress = "127.0.0.1:0"
			path := filepath.Join(root, "panel.yaml")
			data, _ := yaml.Marshal(c)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			c.Database, c.NodeIdentityDirectory = owner.Database, owner.IdentityDirectory
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				done <- Run(ctx, owner, c, path, fstest.MapFS{"index.html": {Data: []byte("UI")}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			}()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(60 * time.Second):
					t.Error("managed host failed to stop")
				}
			}()
			client := &http.Client{Timeout: 3 * time.Second}
			base := "http://" + c.HTTPAddress
			deadline := time.Now().Add(60 * time.Second)
			for {
				response, err := client.Get(base + "/healthz")
				if err == nil {
					response.Body.Close()
					if response.StatusCode == 200 {
						break
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("managed host failed to start")
				}
				time.Sleep(20 * time.Millisecond)
			}
			var cookie *http.Cookie
			csrf := ""
			call := func(method, path string, payload any) (int, []byte) {
				var input io.Reader
				if payload != nil {
					encoded, _ := json.Marshal(payload)
					input = bytes.NewReader(encoded)
				}
				request, err := http.NewRequest(method, base+"/api/v1"+path, input)
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Origin", base)
				if cookie != nil {
					request.AddCookie(cookie)
				}
				if csrf != "" {
					request.Header.Set("X-CSRF-Token", csrf)
				}
				response, err := client.Do(request)
				if err != nil {
					return 0, nil
				}
				defer response.Body.Close()
				body, _ := io.ReadAll(response.Body)
				if len(response.Cookies()) > 0 {
					cookie = response.Cookies()[0]
				}
				return response.StatusCode, body
			}
			tokenBytes, err := os.ReadFile(filepath.Join(root, "setup-token"))
			if err != nil {
				t.Fatal("setup token file missing", err)
			}
			status, _ := call("POST", "/auth/setup", map[string]any{"token": strings.TrimSpace(string(tokenBytes)), "username": "owner", "password": "owner-test-password", "platformName": "Online test", "timezone": "UTC"})
			if status != 201 {
				t.Fatal("setup failed", status)
			}
			status, body := call("POST", "/auth/login", map[string]any{"username": "owner", "password": "owner-test-password"})
			var session auth.Session
			json.Unmarshal(body, &session)
			csrf = session.CSRFToken
			if status != 200 {
				t.Fatal(status, string(body))
			}
			password := "online-restore-test-password"
			status, body = call("POST", "/users", map[string]any{"username": "online-user", "displayName": "Snapshot name", "status": "active", "maxDevices": 2, "maxConnections": 8, "allowedProtocols": []string{"http"}})
			var user struct{ ID string }
			json.Unmarshal(body, &user)
			if status != 201 {
				t.Fatal(status, string(body))
			}
			if failCandidate {
				injector, err := db.Open(context.Background(), c.Database)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := injector.Exec("CREATE TRIGGER reject_restore_audit BEFORE INSERT ON admin_operations WHEN NEW.action='准备恢复数据库' BEGIN SELECT RAISE(ABORT,'candidate audit failure'); END"); err != nil {
					t.Fatal(err)
				}
				injector.Close()
			}
			status, body = call("POST", "/backups", map[string]any{"password": password})
			var backup system.Backup
			json.Unmarshal(body, &backup)
			if status != 201 {
				t.Fatal(status, string(body))
			}
			observer, err := db.Open(context.Background(), c.Database)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := observer.Exec("UPDATE users SET display_name='Current name',used_traffic=900 WHERE id=?", user.ID); err != nil {
				t.Fatal(err)
			}
			if failCandidate {
				if _, err := observer.Exec("DROP TRIGGER reject_restore_audit"); err != nil {
					t.Fatal(err)
				}
			}
			observer.Close()
			status, body = call("POST", "/backups/"+backup.ID+"/restore-preview", map[string]any{"password": password})
			var preview system.RestorePreview
			json.Unmarshal(body, &preview)
			if status != 200 || !preview.ExecutionAvailable {
				t.Fatal("managed preview disabled", status, string(body))
			}
			status, _ = call("POST", "/backups/"+backup.ID+"/restore", map[string]any{"password": password, "archiveSha256": backup.SHA256, "confirmBackupId": "wrong"})
			if status != 400 {
				t.Fatal("incorrect confirmation accepted", status)
			}
			status, body = call("POST", "/backups/"+backup.ID+"/restore", map[string]any{"password": password, "archiveSha256": backup.SHA256, "confirmBackupId": backup.ID})
			var task restoretasks.Record
			json.Unmarshal(body, &task)
			if status != 202 || task.ID == "" {
				t.Fatal("restore not accepted", status, string(body))
			}
			if failCandidate {
				deadline = time.Now().Add(15 * time.Second)
				for {
					status, body = call("GET", "/restore-records", nil)
					var result struct{ Items []restoretasks.Record }
					json.Unmarshal(body, &result)
					if status == 200 && len(result.Items) > 0 && result.Items[0].Status == "failed" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("failed restore did not restart current service", status, string(body))
					}
					time.Sleep(30 * time.Millisecond)
				}
				status, body = call("GET", "/users/"+user.ID, nil)
				var current struct {
					DisplayName string
					UsedTraffic int64
				}
				json.Unmarshal(body, &current)
				if status != 200 || current.DisplayName != "Current name" || current.UsedTraffic != 900 {
					t.Fatal("failed restore changed original state", status, string(body))
				}
				return
			}
			deadline = time.Now().Add(15 * time.Second)
			for {
				status, _ = call("GET", "/users", nil)
				if status == 401 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("old session not revoked after rebuild", status)
				}
				time.Sleep(30 * time.Millisecond)
			}
			cookie = nil
			csrf = ""
			status, body = call("POST", "/auth/login", map[string]any{"username": "owner", "password": "owner-test-password"})
			json.Unmarshal(body, &session)
			csrf = session.CSRFToken
			if status != 200 {
				t.Fatal("restored login failed", status, string(body))
			}
			status, body = call("GET", "/users/"+user.ID, nil)
			var restored struct {
				DisplayName string
				UsedTraffic int64
			}
			json.Unmarshal(body, &restored)
			if status != 200 || restored.DisplayName != "Snapshot name" || restored.UsedTraffic != 900 {
				t.Fatal("restore changed draft/usage incorrectly", status, string(body))
			}
			status, body = call("GET", "/restore-records", nil)
			var records struct{ Items []restoretasks.Record }
			json.Unmarshal(body, &records)
			if status != 200 || len(records.Items) != 1 || records.Items[0].ID != task.ID || records.Items[0].LastStage != "serving" {
				t.Fatal("accepted task did not reach serving", status, string(body))
			}
			status, body = call("POST", "/backups/"+backup.ID+"/restore", map[string]any{"password": password, "archiveSha256": backup.SHA256, "confirmBackupId": backup.ID})
			var second restoretasks.Record
			json.Unmarshal(body, &second)
			if status != 202 || second.ID == task.ID {
				t.Fatal("rebuilt service could not accept subsequent restore", status, string(body))
			}
			deadline = time.Now().Add(15 * time.Second)
			for {
				status, _ = call("GET", "/users", nil)
				if status == 401 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("second managed restore did not rebuild service", status)
				}
				time.Sleep(30 * time.Millisecond)
			}
			cookie = nil
			csrf = ""
			status, body = call("POST", "/auth/login", map[string]any{"username": "owner", "password": "owner-test-password"})
			json.Unmarshal(body, &session)
			csrf = session.CSRFToken
			if status != 200 {
				t.Fatal("second restored service unavailable", status)
			}
			status, body = call("GET", "/restore-records", nil)
			json.Unmarshal(body, &records)
			if status != 200 || len(records.Items) != 2 || records.Items[0].ID != second.ID || records.Items[1].Status != "stopped" {
				t.Fatal("restore lifetime history inconsistent", status, string(body))
			}
		})
	}
}

func TestConcurrentRestoreReservationHasSingleOwner(t *testing.T) {
	r := &receiver{}
	first, err := r.hooks().Reserve()
	if err != nil {
		t.Fatal(err)
	}
	if release, err := r.hooks().Reserve(); err == nil || release != nil {
		t.Fatal("parallel restore was admitted")
	}
	first()
	second, err := r.hooks().Reserve()
	if err != nil {
		t.Fatal("rejected preparation leaked reservation", err)
	}
	second()
}
