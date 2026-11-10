package notification

import (
	"context"
	"log/slog"
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
}

func (w *Worker) Run(ctx context.Context) {
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
	metrics, err := w.Sources.Metrics(ctx, now.Add(-15*time.Minute))
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
		Now:     now,
		Nodes:   nodes,
		Metrics: metrics,
		Traffic: traffic,
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
			w.Publish("notification", map[string]any{"id": saved.ID})
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
	if m, ok := data.(map[string]any); ok {
		if v, ok := m["alert"].(string); ok {
			alertKind = v
		}
		if v, ok := m["message"].(string); ok {
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
		w.Publish("notification", map[string]any{"id": saved.ID})
	}
	w.dispatchToChannels(ctx, saved)
}

func (w *Worker) dispatchToChannels(ctx context.Context, n Notification) {
	channels, err := w.Repository.Channels(ctx)
	if err != nil {
		return
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		cfg, err := w.Repository.ChannelConfig(ctx, ch.ID)
		if err != nil {
			continue
		}
		code := w.Sender.Send(ctx, ch, cfg, n)
		_ = w.Repository.RecordDelivery(ctx, ch.ID, code, time.Now())
	}
}

func (w *Worker) Test(ctx context.Context, channelID string) string {
	channels, err := w.Repository.Channels(ctx)
	if err != nil {
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
	_ = w.Repository.RecordDelivery(ctx, channelID, code, time.Now())
	return code
}
