package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTelegramRequiresValidSuccessJSON(t *testing.T) {
	for _, body := range []string{"", "not json", `{`, `{}`, `null`, `{"ok":false}`, `{"ok":"true"}`, `{"ok":true} trailing`, `{"ok":true}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer server.Close()
			got := (Sender{Client: server.Client(), TelegramAPI: server.URL}).Send(context.Background(), Channel{Kind: "telegram"}, ChannelConfig{BotToken: "secret", ChatID: "private"}, Notification{})
			want := "channel_rejected"
			if body == `{"ok":true}` {
				want = ""
			}
			if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		})
	}
}

func TestTelegramRejectsTruncatedSuccessPrefix(t *testing.T) {
	body := `{"ok":true}` + strings.Repeat(" ", 64*1024-len(`{"ok":true}`)) + "invalid JSON suffix"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
	defer server.Close()
	code := (Sender{Client: server.Client(), TelegramAPI: server.URL}).Send(context.Background(), Channel{Kind: "telegram"}, ChannelConfig{BotToken: "secret", ChatID: "private"}, Notification{})
	if code != "channel_rejected" {
		t.Fatalf("truncated response accepted: %q", code)
	}
}

func TestWebhookRejectsHTTPSDowngrade(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var received atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1) }))
			defer target.Close()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"?secret=private", status)
			}))
			defer server.Close()
			code := (Sender{Client: server.Client()}).Send(context.Background(), Channel{Kind: "webhook"}, ChannelConfig{URL: server.URL}, Notification{Body: "private"})
			if code == "" || received.Load() != 0 {
				t.Fatalf("code=%q insecure requests=%d", code, received.Load())
			}
		})
	}
}

func TestWebhookHTTPSRedirectDoesNotMutateClient(t *testing.T) {
	for _, policy := range []string{"default", "custom"} {
		t.Run(policy, func(t *testing.T) {
			var received atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/end", http.StatusTemporaryRedirect)
					return
				}
				received.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil || r.Method != "POST" || !strings.Contains(string(body), "notification body") {
					t.Errorf("redirect lost request: %s %s %v", r.Method, body, err)
				}
			}))
			defer server.Close()
			client := server.Client()
			if policy == "custom" {
				client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("stop") }
			}
			code := (Sender{Client: client}).Send(context.Background(), Channel{Kind: "webhook"}, ChannelConfig{URL: server.URL + "/start"}, Notification{Body: "notification body"})
			if policy == "default" {
				if code != "" || received.Load() != 1 || client.CheckRedirect != nil {
					t.Fatalf("code=%q requests=%d client mutated=%v", code, received.Load(), client.CheckRedirect != nil)
				}
			} else {
				if code != "" || received.Load() != 1 || client.CheckRedirect == nil {
					t.Fatalf("code=%q requests=%d client policy missing=%v", code, received.Load(), client.CheckRedirect == nil)
				}
				if err := client.CheckRedirect(nil, nil); err == nil || err.Error() != "stop" {
					t.Fatalf("caller policy mutated: %v", err)
				}
			}
		})
	}
}

func TestDeliveryErrorsWarnWithoutSecrets(t *testing.T) {
	for _, path := range []string{"dispatch", "test"} {
		for _, stage := range []string{"read", "decrypt", "save"} {
			t.Run(path+"/"+stage, func(t *testing.T) {
				repo, db := newTestRepo(t)
				ctx := context.Background()
				ch, err := repo.SaveChannel(ctx, Channel{Kind: "telegram", Name: "private-name", Enabled: true}, &ChannelConfig{BotToken: "private-token", ChatID: "private-chat"})
				if err != nil {
					t.Fatal(err)
				}
				switch stage {
				case "read":
					_, err = db.Exec("DROP TABLE notification_channels")
				case "decrypt":
					_, err = db.Exec("UPDATE notification_channels SET encrypted_config=?", []byte("private-config"))
				case "save":
					_, err = db.Exec("CREATE TRIGGER reject_delivery BEFORE UPDATE ON notification_channels BEGIN SELECT RAISE(FAIL, 'private-database-error'); END")
				}
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"ok":true}`) }))
				defer server.Close()
				var logs bytes.Buffer
				worker := &Worker{Repository: repo, Sender: Sender{Client: server.Client(), TelegramAPI: server.URL}, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
				if path == "dispatch" {
					worker.dispatchToChannels(ctx, Notification{})
				} else {
					worker.Test(ctx, ch.ID)
				}
				var entry map[string]any
				if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
					t.Fatalf("missing single warning: %q: %v", logs.String(), err)
				}
				wantCode := map[string]string{"read": "channel_read_failed", "decrypt": "channel_config_failed", "save": "channel_delivery_save_failed"}[stage]
				wantID := ch.ID
				if path == "dispatch" && stage == "read" {
					wantID = ""
				}
				if entry["level"] != "WARN" || entry["code"] != wantCode || entry["channel_id"] != wantID {
					t.Fatalf("wrong warning: %v", entry)
				}
				if len(entry) != 5 || strings.Contains(logs.String(), "private") {
					t.Fatalf("unexpected log fields or secret: %s", logs.String())
				}
			})
		}
	}
}

func TestWebhookConfigFailureWarnsWithChannelID(t *testing.T) {
	repo, db := newTestRepo(t)
	ctx := context.Background()
	ch, err := repo.SaveChannel(ctx, Channel{Kind: "webhook", Name: "private", Enabled: true}, &ChannelConfig{URL: "https://example.invalid/private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE notification_channels SET encrypted_config=?", []byte("private")); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	w := Worker{Repository: repo, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	w.dispatchToChannels(ctx, Notification{})
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("missing warning: %q", logs.String())
	}
	if entry["channel_id"] != ch.ID || entry["code"] != "channel_config_failed" || entry["level"] != "WARN" || len(entry) != 5 || strings.Contains(logs.String(), "private") {
		t.Fatalf("wrong warning: %v", entry)
	}
}

func TestWorkerPublishesNotificationKind(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()
	if err := repo.SaveRules(ctx, []Rule{{Kind: "cpu", Enabled: true, Threshold: 90, DurationMinutes: 5}}); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	w := Worker{Repository: repo, Sources: durationSources{t, 15}, Publish: func(event string, data any) {
		payload := data.(map[string]any)
		kind, _ := payload["kind"].(string)
		if event != "notification" || payload["id"] == nil {
			t.Fatalf("unexpected event: %s %v", event, data)
		}
		kinds = append(kinds, kind)
	}}
	w.tick(ctx)
	w.Alert(ctx, map[string]any{"alert": "certificate_deploy_failed"})
	if strings.Join(kinds, ",") != "cpu,system_alert" {
		t.Fatalf("published kinds=%v", kinds)
	}
}
