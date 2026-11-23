package notification

import (
	"context"
	"errors"
	"log/slog"
	"proxycontrol/internal/panel/node"
	"time"
)

type Sources interface {
	Nodes(ctx context.Context) ([]NodeSnapshot, error)
	Metrics(ctx context.Context, since time.Time) (map[string]MetricWindow, error)
	Traffic(ctx context.Context, now time.Time) (map[string]TrafficSnapshot, error)
}

type Worker struct {
	Repository *Repository
	Sources    Sources
	Sender     Sender
	Publish    func(kind string, data any)
	Logger     *slog.Logger
	Interval   time.Duration
	startedAt  time.Time
}

func (w *Worker) Run(ctx context.Context) {
	w.startedAt = time.Now()
	interval := w.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pruneTicker := time.NewTicker(24 * time.Hour)
	defer pruneTicker.Stop()

	// Initial tick and prune
	w.tick(ctx)
	if w.Repository != nil {
		_, _ = w.Repository.Prune(ctx, time.Now().Add(-90*24*time.Hour))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-pruneTicker.C:
			if w.Repository != nil {
				_, _ = w.Repository.Prune(ctx, time.Now().Add(-90*24*time.Hour))
			}
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	if w.Sources == nil || w.Repository == nil {
		return
	}
	rules, err := w.Repository.Rules(ctx)
	if err != nil {
		if w.Logger != nil {
			w.Logger.Error("failed to load notification rules", "error", err)
		}
		return
	}
	states, err := w.Repository.States(ctx)
	if err != nil {
		if w.Logger != nil {
			w.Logger.Error("failed to load notification states", "error", err)
		}
		return
	}

	nodes, err := w.Sources.Nodes(ctx)
	if err != nil {
		if w.Logger != nil {
			w.Logger.Error("failed to load nodes snapshot", "error", err)
		}
		return
	}
	now := time.Now()
	windowMinutes := 15
	// A panel restart interrupts observation for every node. Let Agents reconnect
	// before opening new offline episodes; persisted active episodes still resolve.
	// The minimum startup grace period equals the heartbeat freshness window.
	offlineAlertNotBefore := w.startedAt.Add(node.HeartbeatFreshness)
	for _, rule := range rules {
		if rule.Kind == "node_offline" {
			offlineAlertNotBefore = w.startedAt.Add(max(node.HeartbeatFreshness, time.Duration(rule.DurationMinutes)*time.Minute))
		}
		if rule.Enabled && (rule.Kind == "cpu" || rule.Kind == "memory") && rule.DurationMinutes > windowMinutes {
			windowMinutes = rule.DurationMinutes
		}
	}
	metrics, err := w.Sources.Metrics(ctx, now.Truncate(time.Minute).Add(-time.Duration(windowMinutes)*time.Minute))
	if err != nil {
		if w.Logger != nil {
			w.Logger.Error("failed to load metrics", "error", err)
		}
		return
	}
	traffic, err := w.Sources.Traffic(ctx, now)
	if err != nil {
		if w.Logger != nil {
			w.Logger.Error("failed to load traffic", "error", err)
		}
		return
	}

	snap := Snapshot{
		OfflineAlertNotBefore: offlineAlertNotBefore,
		Now:                   now,
		Nodes:                 nodes,
		Metrics:               metrics,
		Traffic:               traffic,
	}

	events, nextStates := Evaluate(rules, snap, states)
	if err := w.Repository.SaveStates(ctx, nextStates); err != nil {
		if w.Logger != nil {
			w.Logger.Error("failed to save notification states", "error", err)
		}
	}

	for _, ev := range events {
		notif := Notification{
			At:       now.UTC().Format(time.RFC3339Nano),
			Kind:     ev.Kind,
			Severity: ev.Severity,
			NodeID:   ev.NodeID,
			Title:    ev.Title,
			Body:     ev.Body,
		}
		saved, err := w.Repository.Insert(ctx, notif)
		if err != nil {
			if w.Logger != nil {
				w.Logger.Error("failed to insert notification", "error", err)
			}
			continue
		}
		if w.Publish != nil {
			w.Publish("notification", map[string]any{"id": saved.ID, "kind": saved.Kind})
		}
		w.dispatchToChannels(ctx, saved)
	}
}

func (w *Worker) Alert(ctx context.Context, data any) {
	if w.Repository == nil {
		return
	}
	rules, err := w.Repository.Rules(ctx)
	if err != nil {
		return
	}
	systemAlertEnabled := false
	for _, r := range rules {
		if r.Kind == "system_alert" {
			systemAlertEnabled = r.Enabled
			break
		}
	}
	if !systemAlertEnabled {
		return
	}

	var alertKind, message string
	switch m := data.(type) {
	case map[string]any:
		if v, ok := m["alert"].(string); ok {
			alertKind = v
		}
		if v, ok := m["message"].(string); ok {
			message = v
		} else if v, ok := m["reason"].(string); ok {
			message = v
		}
	case map[string]string:
		alertKind = m["alert"]
		if v, ok := m["message"]; ok {
			message = v
		} else if v, ok := m["reason"]; ok {
			message = v
		}
	}

	title := "系统告警"
	switch alertKind {
	case "certificate_renewal":
		title = "证书续期失败"
	case "certificate_deploy_failed":
		title = "证书自动发布失败"
	case "node_renewal":
		title = "服务器续费提醒"
	case "auto_quota_failed":
		title = "自动额度发布失败"
	}

	notif := Notification{
		At:       time.Now().UTC().Format(time.RFC3339Nano),
		Kind:     "system_alert",
		Severity: "warning",
		Title:    title,
		Body:     message,
	}

	saved, err := w.Repository.Insert(ctx, notif)
	if err != nil {
		return
	}
	if w.Publish != nil {
		w.Publish("notification", map[string]any{"id": saved.ID, "kind": saved.Kind})
	}
	w.dispatchToChannels(ctx, saved)
}

func (w *Worker) dispatchToChannels(ctx context.Context, n Notification) {
	channels, err := w.Repository.Channels(ctx)
	if err != nil {
		w.warnChannelRead("", err)
		return
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		cfg, err := w.Repository.ChannelConfig(ctx, ch.ID)
		if err != nil {
			w.warnChannel(ch.ID, "channel_config_failed")
			continue
		}
		code := w.Sender.Send(ctx, ch, cfg, n)
		if err := w.Repository.RecordDelivery(ctx, ch.ID, code, time.Now()); err != nil {
			w.warnChannel(ch.ID, "channel_delivery_save_failed")
		}
	}
}

func (w *Worker) Test(ctx context.Context, channelID string) string {
	channels, err := w.Repository.Channels(ctx)
	if err != nil {
		w.warnChannelRead(channelID, err)
		return "channel_not_found"
	}
	var target Channel
	found := false
	for _, ch := range channels {
		if ch.ID == channelID {
			target = ch
			found = true
			break
		}
	}
	if !found {
		return "channel_not_found"
	}

	cfg, err := w.Repository.ChannelConfig(ctx, channelID)
	if err != nil {
		w.warnChannel(channelID, "channel_config_failed")
		return "channel_not_found"
	}

	testNotif := Notification{
		At:       time.Now().UTC().Format(time.RFC3339Nano),
		Kind:     "system_alert",
		Severity: "info",
		Title:    "测试通知",
		Body:     "这是一条测试通知，用于验证通知渠道是否配置正确。",
	}

	code := w.Sender.Send(ctx, target, cfg, testNotif)
	if err := w.Repository.RecordDelivery(ctx, channelID, code, time.Now()); err != nil {
		w.warnChannel(channelID, "channel_delivery_save_failed")
	}
	return code
}

func (w *Worker) warnChannelRead(channelID string, err error) {
	var configErr *channelConfigError
	if errors.As(err, &configErr) {
		w.warnChannel(configErr.channelID, "channel_config_failed")
		return
	}
	w.warnChannel(channelID, "channel_read_failed")
}

func (w *Worker) warnChannel(channelID, code string) {
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// Channel configuration and underlying errors may contain credentials.
	logger.Warn("notification delivery failed", "channel_id", channelID, "code", code)
}
