package notification

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/internal/platform/vault"
)

func newTestRepo(t *testing.T) (*Repository, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	key := make([]byte, 32)
	rand.Read(key)
	v, err := vault.FromKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return &Repository{Database: database, Vault: v}, database
}

func TestRulesRoundTripAndValidation(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	rules, err := repo.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 7 {
		t.Fatalf("expected 7 default rules, got %d", len(rules))
	}

	// Invalid threshold
	invalid := []Rule{
		{Kind: "cpu", Enabled: true, Threshold: 150, DurationMinutes: 5, CooldownMinutes: 30},
	}
	if err := repo.SaveRules(ctx, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for cpu threshold > 100, got %v", err)
	}

	// Valid update
	valid := []Rule{
		{Kind: "node_offline", Enabled: true, Threshold: 0, DurationMinutes: 5, CooldownMinutes: 60},
		{Kind: "cpu", Enabled: false, Threshold: 80, DurationMinutes: 10, CooldownMinutes: 45},
	}
	if err := repo.SaveRules(ctx, valid); err != nil {
		t.Fatalf("save valid rules failed: %v", err)
	}

	updated, err := repo.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var foundCPU bool
	for _, r := range updated {
		if r.Kind == "cpu" {
			foundCPU = true
			if r.Enabled || r.Threshold != 80 || r.DurationMinutes != 10 || r.CooldownMinutes != 45 {
				t.Fatalf("unexpected CPU rule: %+v", r)
			}
		}
	}
	if !foundCPU {
		t.Fatal("cpu rule not found after update")
	}
}

func TestChannelsEncryptionAndPreserveOnNilConfig(t *testing.T) {
	repo, rawDB := newTestRepo(t)
	ctx := context.Background()

	ch, err := repo.SaveChannel(ctx, Channel{
		Kind:    "telegram",
		Name:    "My Bot",
		Enabled: true,
	}, &ChannelConfig{
		BotToken: "123456:secret-token-xyz",
		ChatID:   "998877",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ID == "" || ch.Last4 != "-xyz" {
		t.Fatalf("unexpected channel: %+v", ch)
	}

	// Check raw database: no plaintext token
	var rawConfig []byte
	var last4 string
	if err := rawDB.QueryRowContext(ctx, "SELECT encrypted_config, last4 FROM notification_channels WHERE id=?", ch.ID).Scan(&rawConfig, &last4); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawConfig), "secret-token") {
		t.Fatal("plaintext token leaked in encrypted_config column")
	}

	// Decrypt config
	cfg, err := repo.ChannelConfig(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "123456:secret-token-xyz" || cfg.ChatID != "998877" {
		t.Fatalf("decrypted config mismatch: %+v", cfg)
	}

	// Update channel with nil config: preserves original encrypted config and last4
	ch.Name = "Renamed Bot"
	ch.Enabled = false
	updatedCh, err := repo.SaveChannel(ctx, ch, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updatedCh.Name != "Renamed Bot" || updatedCh.Enabled || updatedCh.Last4 != "-xyz" {
		t.Fatalf("updated channel mismatch: %+v", updatedCh)
	}

	cfgAfter, err := repo.ChannelConfig(ctx, ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfgAfter.BotToken != "123456:secret-token-xyz" {
		t.Fatalf("config lost after nil update: %+v", cfgAfter)
	}
}

func TestNotificationsListViewerAndUnread(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for _, n := range []Notification{
		{At: now.Format(time.RFC3339Nano), Kind: "node_offline", Severity: "critical", Title: "offline", Body: "down"},
		{At: now.Add(time.Second).Format(time.RFC3339Nano), Kind: "cpu", Severity: "warning", Title: "cpu", Body: "high"},
		{At: now.Add(2 * time.Second).Format(time.RFC3339Nano), Kind: "public_ip", Severity: "info", Title: "ip", Body: "changed"},
		{At: now.Add(3 * time.Second).Format(time.RFC3339Nano), Kind: "system_alert", Severity: "critical", Title: "alert", Body: "disk fault"},
	} {
		if _, err := repo.Insert(ctx, n); err != nil {
			t.Fatal(err)
		}
	}

	// Owner sees all 4
	items, total, err := repo.List(ctx, false, false, 1, 10)
	if err != nil || total != 4 || len(items) != 4 {
		t.Fatalf("owner list: total=%d, len=%d, err=%v", total, len(items), err)
	}

	// Viewer sees only 2 (node_offline, cpu)
	viewerItems, viewerTotal, err := repo.List(ctx, true, false, 1, 10)
	if err != nil || viewerTotal != 2 || len(viewerItems) != 2 {
		t.Fatalf("viewer list: total=%d, len=%d, err=%v", viewerTotal, len(viewerItems), err)
	}

	// Mark all read
	if err := repo.MarkAllRead(ctx); err != nil {
		t.Fatal(err)
	}

	// Unread query returns 0
	unreadItems, unreadTotal, err := repo.List(ctx, false, true, 1, 10)
	if err != nil || unreadTotal != 0 || len(unreadItems) != 0 {
		t.Fatalf("unread list after mark all read: total=%d, len=%d", unreadTotal, len(unreadItems))
	}
}

func TestNotificationsPrune(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	oldTime := time.Now().UTC().Add(-100 * 24 * time.Hour)
	recentTime := time.Now().UTC()

	if _, err := repo.Insert(ctx, Notification{At: oldTime.Format(time.RFC3339Nano), Kind: "cpu", Severity: "warning", Title: "old", Body: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Insert(ctx, Notification{At: recentTime.Format(time.RFC3339Nano), Kind: "cpu", Severity: "warning", Title: "recent", Body: "recent"}); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().UTC().Add(-90 * 24 * time.Hour)
	pruned, err := repo.Prune(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}

	items, total, err := repo.List(ctx, false, false, 1, 10)
	if err != nil || total != 1 || len(items) != 1 || items[0].Title != "recent" {
		t.Fatalf("after prune: total=%d, len=%d, items=%+v", total, len(items), items)
	}
}

func TestStateRoundTrip(t *testing.T) {
	repo, _ := newTestRepo(t)
	ctx := context.Background()

	_, ok, err := repo.State(ctx, "none")
	if err != nil || ok {
		t.Fatalf("expected not ok, got ok=%v, err=%v", ok, err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	s := State{Key: "k1", Active: true, Since: now, LastNotified: now, Value: "val"}
	if err := repo.SaveState(ctx, s); err != nil {
		t.Fatal(err)
	}

	got, ok, err := repo.State(ctx, "k1")
	if err != nil || !ok {
		t.Fatalf("expected ok, got ok=%v, err=%v", ok, err)
	}
	if got.Key != "k1" || !got.Active || got.Value != "val" {
		t.Fatalf("state mismatch: %+v", got)
	}

	if err := repo.DeleteState(ctx, "k1"); err != nil {
		t.Fatal(err)
	}

	_, ok, err = repo.State(ctx, "k1")
	if err != nil || ok {
		t.Fatalf("expected deleted, got ok=%v", ok)
	}
}
