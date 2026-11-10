package notification

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
