package notification

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWebhookFormatsAndSignatures(t *testing.T) {
	// Signature fixtures independently computed with Node crypto, not sender helpers.
	for _, format := range []string{"", "generic", "wecom", "dingtalk", "feishu"} {
		for _, signed := range []bool{false, true} {
			if signed && format != "dingtalk" && format != "feishu" {
				continue
			}
			t.Run(format+fmtBool(signed), func(t *testing.T) {
				var payload map[string]any
				var query url.Values
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("invalid HTTP envelope")
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					query = r.URL.Query()
					io.WriteString(w, `{"errcode":0,"code":0}`)
				}))
				defer server.Close()
				cfg := ChannelConfig{URL: server.URL + "?access_token=keep%2Btoken&extra=one&extra=two", Format: format}
				if signed {
					cfg.Secret = "test-secret"
				}
				sender := Sender{Client: server.Client(), Now: func() time.Time { return time.Unix(1700000000, 123000000) }}
				n := Notification{Kind: "cpu", Severity: "warning", Title: "标题", Body: "正文", NodeID: "node1", At: "at"}
				if code := sender.Send(context.Background(), Channel{Kind: "webhook"}, cfg, n); code != "" {
					t.Fatal(code)
				}
				want := map[string]any{"msgtype": "text", "text": map[string]any{"content": "[WARNING] 标题\n正文"}}
				if format == "" || format == "generic" {
					want = map[string]any{"kind": "cpu", "severity": "warning", "title": "标题", "body": "正文", "node_id": "node1", "at": "at"}
				}
				if format == "feishu" {
					want = map[string]any{"msg_type": "text", "content": map[string]any{"text": "[WARNING] 标题\n正文"}}
				}
				if signed && format == "feishu" {
					want["timestamp"] = "1700000000"
					want["sign"] = "mbm4Y4oluIPQ00qlBIhX8vAZ0EKv3nw0LuTb91jPL84="
				}
				if !reflect.DeepEqual(payload, want) {
					t.Fatalf("payload=%#v want=%#v", payload, want)
				}
				if query.Get("access_token") != "keep+token" || !reflect.DeepEqual(query["extra"], []string{"one", "two"}) {
					t.Fatalf("lost original query: %v", query)
				}
				if signed && format == "dingtalk" {
					if query.Get("timestamp") != "1700000000123" || query.Get("sign") != "emvuTAxMTWOXmz4z3p3ifyzxqMNeLEBESaq9BQR0d6w=" {
						t.Fatalf("wrong signature parameters: %v", query)
					}
				} else if query.Has("timestamp") || query.Has("sign") {
					t.Fatal("unexpected URL signature")
				}
			})
		}
	}
}

func fmtBool(v bool) string {
	if v {
		return "/signed"
	}
	return "/unsigned"
}

func TestWebhookVendorResponses(t *testing.T) {
	for _, tc := range []struct {
		format, body, want string
		status             int
	}{
		{"wecom", `{"errcode":0}`, "", 200}, {"wecom", `{"errcode":93000,"errmsg":"private"}`, "channel_rejected", 200},
		{"dingtalk", `{"errcode":0}`, "", 200}, {"dingtalk", `{"errcode":310000}`, "channel_rejected", 200},
		{"dingtalk", `{"errcode":"0"}`, "", 200}, {"dingtalk", `{"errcode":"310000"}`, "channel_rejected", 200},
		{"feishu", `{"code":0}`, "", 200}, {"feishu", `{"code":19021}`, "channel_rejected", 200},
		{"feishu", `{"StatusCode":0}`, "", 200}, {"feishu", `{"StatusCode":1}`, "channel_rejected", 200},
		{"wecom", `{}`, "channel_rejected", 200}, {"dingtalk", `not json`, "channel_rejected", 200},
		{"feishu", `{"code":null}`, "channel_rejected", 200}, {"feishu", `{"code":"0"}`, "channel_rejected", 200},
		{"feishu", strings.Repeat(" ", 65536) + `{"code":0}`, "channel_rejected", 200},
		{"wecom", `{"errcode":0}`, "channel_http", 500}, {"generic", `not json`, "", 200},
	} {
		t.Run(tc.format+tc.body[:min(len(tc.body), 30)], func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
			defer server.Close()
			cfg := ChannelConfig{URL: server.URL, Format: tc.format}
			if got := (Sender{Client: server.Client()}).Send(context.Background(), Channel{Kind: "webhook"}, cfg, Notification{}); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNotificationTextWithoutSeverity(t *testing.T) {
	if got := notificationText(Notification{Title: "标题", Body: "正文"}); got != "标题\n正文" {
		t.Fatal(got)
	}
}

func TestTelegramRequestShape(t *testing.T) {
	var receivedPath string
	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	sender := Sender{
		Client:      server.Client(),
		TelegramAPI: server.URL,
	}

	ch := Channel{ID: "ch1", Kind: "telegram", Name: "Telegram"}
	cfg := ChannelConfig{BotToken: "test_bot_token", ChatID: "-10012345"}
	n := Notification{
		At:       "2026-11-09T00:00:00Z",
		Kind:     "node_offline",
		Severity: "critical",
		Title:    "节点离线",
		Body:     "节点已超过3分钟没有心跳",
	}

	code := sender.Send(context.Background(), ch, cfg, n)
	if code != "" {
		t.Fatalf("expected success (empty code), got %q", code)
	}

	if receivedPath != "/bottest_bot_token/sendMessage" {
		t.Fatalf("unexpected path: %s", receivedPath)
	}
	if receivedBody["chat_id"] != "-10012345" {
		t.Fatalf("unexpected chat_id: %v", receivedBody["chat_id"])
	}
	text, _ := receivedBody["text"].(string)
	if text == "" {
		t.Fatal("empty text in telegram body")
	}
}

func TestWebhookRequiresHTTPS(t *testing.T) {
	sender := Sender{}
	ch := Channel{ID: "ch2", Kind: "webhook", Name: "Insecure Webhook"}
	cfg := ChannelConfig{URL: "http://example.com/insecure"}
	n := Notification{Title: "Test", Body: "Test"}

	code := sender.Send(context.Background(), ch, cfg, n)
	if code != "channel_rejected" {
		t.Fatalf("expected channel_rejected for http URL, got %q", code)
	}
}

func TestSenderErrorCodes(t *testing.T) {
	// 1. Timeout
	timeoutServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer timeoutServer.Close()

	client := timeoutServer.Client()
	client.Timeout = 20 * time.Millisecond
	sender := Sender{Client: client, TelegramAPI: timeoutServer.URL}
	ch := Channel{Kind: "telegram"}
	cfg := ChannelConfig{BotToken: "tok", ChatID: "123"}
	n := Notification{Title: "T", Body: "B"}

	if code := sender.Send(context.Background(), ch, cfg, n); code != "channel_timeout" {
		t.Fatalf("expected channel_timeout, got %q", code)
	}

	// 2. HTTP 500
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", 500)
	}))
	defer errServer.Close()
	sender = Sender{Client: errServer.Client(), TelegramAPI: errServer.URL}
	if code := sender.Send(context.Background(), ch, cfg, n); code != "channel_http" {
		t.Fatalf("expected channel_http for 500 response, got %q", code)
	}

	// 3. Network connection refused
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := closedServer.URL
	closedServer.Close()
	sender = Sender{Client: http.DefaultClient, TelegramAPI: closedURL}
	if code := sender.Send(context.Background(), ch, cfg, n); code != "channel_network" {
		t.Fatalf("expected channel_network for closed server, got %q", code)
	}

	// 4. Telegram rejected {"ok": false}
	rejectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":false,"description":"Bad Request: chat not found"}`))
	}))
	defer rejectServer.Close()
	sender = Sender{Client: rejectServer.Client(), TelegramAPI: rejectServer.URL}
	if code := sender.Send(context.Background(), ch, cfg, n); code != "channel_rejected" {
		t.Fatalf("expected channel_rejected for telegram ok=false, got %q", code)
	}
}

func TestDeliveryFailureKeepsNotification(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	// Create channel pointing to a failing server
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 503)
	}))
	defer errServer.Close()

	_, err := repo.SaveChannel(ctx, Channel{
		Kind:    "telegram",
		Name:    "Failing Telegram",
		Enabled: true,
	}, &ChannelConfig{
		BotToken: "tok",
		ChatID:   "123",
	})
	if err != nil {
		t.Fatal(err)
	}

	worker := &Worker{
		Repository: repo,
		Sender: Sender{
			Client:      errServer.Client(),
			TelegramAPI: errServer.URL,
		},
	}

	// Send an alert through worker
	worker.Alert(ctx, map[string]any{
		"alert":   "node_renewal",
		"message": "Node expiring soon",
	})

	// Notification must be stored even though delivery failed
	items, total, err := repo.List(ctx, false, false, 1, 10)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("notification not preserved: total=%d, err=%v", total, err)
	}

	// Channel last_result_code must be recorded
	channels, err := repo.Channels(ctx)
	if err != nil || len(channels) != 1 {
		t.Fatal(err)
	}
	if channels[0].LastResultCode != "channel_http" {
		t.Fatalf("expected last_result_code 'channel_http', got %q", channels[0].LastResultCode)
	}
}
