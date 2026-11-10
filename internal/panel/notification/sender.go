package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Sender struct {
	Client      *http.Client
	TelegramAPI string
	Now         func() time.Time
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

	reqBody, _ := json.Marshal(map[string]any{
		"chat_id": config.ChatID,
		"text":    notificationText(n),
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

func notificationText(n Notification) string {
	text := n.Title + "\n" + n.Body
	if n.Severity != "" {
		text = "[" + strings.ToUpper(n.Severity) + "] " + text
	}
	return text
}

func (s Sender) sendWebhook(ctx context.Context, client *http.Client, config ChannelConfig, n Notification) string {
	u, err := url.Parse(config.URL)
	if err != nil || config.Validate("webhook") != nil {
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
	switch config.WebhookFormat() {
	case "wecom", "dingtalk":
		payload = map[string]any{"msgtype": "text", "text": map[string]string{"content": notificationText(n)}}
	case "feishu":
		payload = map[string]any{"msg_type": "text", "content": map[string]string{"text": notificationText(n)}}
	}
	if config.Secret != "" {
		now := time.Now
		if s.Now != nil {
			now = s.Now
		}
		if config.Format == "dingtalk" {
			ts := strconv.FormatInt(now().UnixMilli(), 10)
			mac := hmac.New(sha256.New, []byte(config.Secret))
			_, _ = mac.Write([]byte(ts + "\n" + config.Secret))
			query := u.Query()
			query.Set("timestamp", ts)
			// Values.Encode applies QueryEscape once, including Base64 '+' and '='.
			query.Set("sign", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
			u.RawQuery = query.Encode()
		} else {
			ts := strconv.FormatInt(now().Unix(), 10)
			mac := hmac.New(sha256.New, []byte(ts+"\n"+config.Secret))
			payload["timestamp"] = ts
			payload["sign"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "channel_rejected"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(data))
	if err != nil {
		return "channel_rejected"
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return classifyNetworkError(err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "channel_http"
	}
	if readErr != nil {
		return "channel_network"
	}
	if config.WebhookFormat() != "generic" {
		var result struct {
			ErrCode    json.Number `json:"errcode"`
			Code       *int        `json:"code"`
			StatusCode *int        `json:"StatusCode"`
		}
		if json.Unmarshal(body, &result) != nil {
			return "channel_rejected"
		}
		if config.Format != "feishu" {
			// DingTalk documents Number, but its official response example uses "0".
			if result.ErrCode != "0" {
				return "channel_rejected"
			}
			return ""
		}
		code := result.Code
		if code == nil {
			code = result.StatusCode
		}
		if code == nil || *code != 0 {
			return "channel_rejected"
		}
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
