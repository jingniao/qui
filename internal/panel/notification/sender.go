package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Sender struct {
	Client      *http.Client
	TelegramAPI string
}

func (s Sender) Send(ctx context.Context, channel Channel, config ChannelConfig, n Notification) string {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	switch channel.Kind {
	case "telegram":
		return s.sendTelegram(ctx, client, config, n)
	case "webhook":
		return s.sendWebhook(ctx, client, config, n)
	default:
		return "channel_rejected"
	}
}

func (s Sender) sendTelegram(ctx context.Context, client *http.Client, config ChannelConfig, n Notification) string {
	if config.BotToken == "" || config.ChatID == "" {
		return "channel_rejected"
	}
	apiBase := s.TelegramAPI
	if apiBase == "" {
		apiBase = "https://api.telegram.org"
	}
	targetURL := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(apiBase, "/"), config.BotToken)

	text := fmt.Sprintf("[%s] %s\n%s", strings.ToUpper(n.Severity), n.Title, n.Body)
	if n.Severity == "" {
		text = fmt.Sprintf("%s\n%s", n.Title, n.Body)
	}

	reqBody, _ := json.Marshal(map[string]any{
		"chat_id": config.ChatID,
		"text":    text,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return "channel_rejected"
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return classifyNetworkError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "channel_http"
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "channel_network"
	}

	var tgResp struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(bodyBytes, &tgResp); err == nil {
		if !tgResp.OK {
			return "channel_rejected"
		}
	}
	return ""
}

func (s Sender) sendWebhook(ctx context.Context, client *http.Client, config ChannelConfig, n Notification) string {
	u, err := url.Parse(config.URL)
	if err != nil || u.Scheme != "https" {
		return "channel_rejected"
	}

	payload := map[string]any{
		"kind":     n.Kind,
		"severity": n.Severity,
		"title":    n.Title,
		"body":     n.Body,
		"node_id":  n.NodeID,
		"at":       n.At,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "channel_rejected"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewReader(data))
	if err != nil {
		return "channel_rejected"
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return classifyNetworkError(err)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "channel_http"
	}
	return ""
}

func classifyNetworkError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "channel_timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "channel_timeout"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return "channel_timeout"
		}
		if errors.Is(urlErr.Err, context.DeadlineExceeded) {
			return "channel_timeout"
		}
	}
	return "channel_network"
}
