package notification

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"proxycontrol/internal/platform/vault"
)

type Repository struct {
	Database *sql.DB
	Vault    *vault.Vault
}

func (r *Repository) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT kind, enabled, threshold, duration_minutes, cooldown_minutes FROM notification_rules ORDER BY kind")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var rule Rule
		var enabled int
		if err := rows.Scan(&rule.Kind, &enabled, &rule.Threshold, &rule.DurationMinutes, &rule.CooldownMinutes); err != nil {
			return nil, err
		}
		rule.Enabled = enabled != 0
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *Repository) SaveRules(ctx context.Context, rules []Rule) error {
	for _, rule := range rules {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, rule := range rules {
		enabled := 0
		if rule.Enabled {
			enabled = 1
		}
		if _, err := tx.ExecContext(ctx, "UPDATE notification_rules SET enabled=?, threshold=?, duration_minutes=?, cooldown_minutes=?, updated_at=? WHERE kind=?",
			enabled, rule.Threshold, rule.DurationMinutes, rule.CooldownMinutes, now, rule.Kind); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) Insert(ctx context.Context, n Notification) (Notification, error) {
	if n.At == "" {
		n.At = time.Now().UTC().Format(time.RFC3339Nano)
	}
	var nodeID sql.NullString
	if n.NodeID != "" {
		nodeID = sql.NullString{String: n.NodeID, Valid: true}
	}
	res, err := r.Database.ExecContext(ctx, "INSERT INTO notifications(at, kind, severity, node_id, title, body) VALUES(?,?,?,?,?,?)",
		n.At, n.Kind, n.Severity, nodeID, n.Title, n.Body)
	if err != nil {
		return Notification{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Notification{}, err
	}
	n.ID = id
	return n, nil
}

func (r *Repository) List(ctx context.Context, viewer bool, unreadOnly bool, page, size int) ([]Notification, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	var where []string
	var args []any
	if viewer {
		where = append(where, "kind IN ('node_offline','cpu','memory','disk')")
	}
	if unreadOnly {
		where = append(where, "(read_at IS NULL OR read_at = '')")
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM notifications"+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := "SELECT id, at, kind, severity, coalesce(node_id,''), title, body, CASE WHEN read_at IS NOT NULL AND read_at != '' THEN 1 ELSE 0 END FROM notifications" +
		whereClause + " ORDER BY at DESC, id DESC LIMIT ? OFFSET ?"
	queryArgs := append(args, size, (page-1)*size)

	rows, err := r.Database.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []Notification
	for rows.Next() {
		var item Notification
		var readInt int
		if err := rows.Scan(&item.ID, &item.At, &item.Kind, &item.Severity, &item.NodeID, &item.Title, &item.Body, &readInt); err != nil {
			return nil, 0, err
		}
		item.Read = readInt != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) MarkAllRead(ctx context.Context) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.Database.ExecContext(ctx, "UPDATE notifications SET read_at=? WHERE read_at IS NULL OR read_at = ''", now)
	return err
}

func (r *Repository) Channels(ctx context.Context) ([]Channel, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT id, kind, name, enabled, last4, last_result_code, last_sent_at FROM notification_channels ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		var enabled int
		var lastSent sql.NullString
		if err := rows.Scan(&ch.ID, &ch.Kind, &ch.Name, &enabled, &ch.Last4, &ch.LastResultCode, &lastSent); err != nil {
			return nil, err
		}
		ch.Enabled = enabled != 0
		if lastSent.Valid {
			ch.LastSentAt = &lastSent.String
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func extractLast4(kind string, cfg *ChannelConfig) string {
	if cfg == nil {
		return ""
	}
	target := ""
	if kind == "telegram" {
		target = cfg.BotToken
	} else if kind == "webhook" {
		target = cfg.URL
	}
	if len(target) <= 4 {
		return target
	}
	return target[len(target)-4:]
}

func (r *Repository) SaveChannel(ctx context.Context, ch Channel, config *ChannelConfig) (Channel, error) {
	if ch.Kind != "telegram" && ch.Kind != "webhook" {
		return Channel{}, ErrInvalid
	}
	ch.Name = strings.TrimSpace(ch.Name)
	if ch.Name == "" {
		return Channel{}, ErrInvalid
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	if ch.ID == "" {
		if config == nil {
			return Channel{}, ErrInvalid
		}
		idBytes := make([]byte, 16)
		if _, err := rand.Read(idBytes); err != nil {
			return Channel{}, err
		}
		ch.ID = hex.EncodeToString(idBytes)

		raw, err := json.Marshal(config)
		if err != nil {
			return Channel{}, err
		}
		encrypted, err := r.Vault.Seal(raw)
		if err != nil {
			return Channel{}, err
		}
		ch.Last4 = extractLast4(ch.Kind, config)
		enabled := 0
		if ch.Enabled {
			enabled = 1
		}
		if _, err := r.Database.ExecContext(ctx, "INSERT INTO notification_channels(id, kind, name, enabled, encrypted_config, last4, last_result_code, last_sent_at, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
			ch.ID, ch.Kind, ch.Name, enabled, encrypted, ch.Last4, "", nil, now, now); err != nil {
			return Channel{}, err
		}
		return ch, nil
	}

	// Update existing channel
	enabled := 0
	if ch.Enabled {
		enabled = 1
	}

	if config != nil {
		raw, err := json.Marshal(config)
		if err != nil {
			return Channel{}, err
		}
		encrypted, err := r.Vault.Seal(raw)
		if err != nil {
			return Channel{}, err
		}
		ch.Last4 = extractLast4(ch.Kind, config)
		res, err := r.Database.ExecContext(ctx, "UPDATE notification_channels SET name=?, enabled=?, encrypted_config=?, last4=?, updated_at=? WHERE id=?",
			ch.Name, enabled, encrypted, ch.Last4, now, ch.ID)
		if err != nil {
			return Channel{}, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return Channel{}, errors.New("channel not found")
		}
		return ch, nil
	}

	// config is nil: preserve original encrypted_config and last4
	res, err := r.Database.ExecContext(ctx, "UPDATE notification_channels SET name=?, enabled=?, updated_at=? WHERE id=?",
		ch.Name, enabled, now, ch.ID)
	if err != nil {
		return Channel{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Channel{}, errors.New("channel not found")
	}

	var last4, lastResult string
	var lastSent sql.NullString
	if err := r.Database.QueryRowContext(ctx, "SELECT last4, last_result_code, last_sent_at FROM notification_channels WHERE id=?", ch.ID).Scan(&last4, &lastResult, &lastSent); err != nil {
		return Channel{}, err
	}
	ch.Last4 = last4
	ch.LastResultCode = lastResult
	if lastSent.Valid {
		ch.LastSentAt = &lastSent.String
	}
	return ch, nil
}

func (r *Repository) DeleteChannel(ctx context.Context, id string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM notification_channels WHERE id=?", id)
	return err
}

func (r *Repository) ChannelConfig(ctx context.Context, id string) (ChannelConfig, error) {
	var encrypted []byte
	if err := r.Database.QueryRowContext(ctx, "SELECT encrypted_config FROM notification_channels WHERE id=?", id).Scan(&encrypted); err != nil {
		return ChannelConfig{}, err
	}
	plaintext, err := r.Vault.Open(encrypted)
	if err != nil {
		return ChannelConfig{}, err
	}
	var cfg ChannelConfig
	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return ChannelConfig{}, err
	}
	return cfg, nil
}

func (r *Repository) RecordDelivery(ctx context.Context, id string, code string, at time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sentAt := at.UTC().Format(time.RFC3339Nano)
	_, err := r.Database.ExecContext(ctx, "UPDATE notification_channels SET last_result_code=?, last_sent_at=?, updated_at=? WHERE id=?",
		code, sentAt, now, id)
	return err
}

func (r *Repository) State(ctx context.Context, key string) (State, bool, error) {
	var activeInt int
	var sinceStr, lastNotifiedStr, val string
	err := r.Database.QueryRowContext(ctx, "SELECT active, since, last_notified_at, value FROM notification_state WHERE dedupe_key=?", key).Scan(&activeInt, &sinceStr, &lastNotifiedStr, &val)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	since, _ := time.Parse(time.RFC3339Nano, sinceStr)
	lastNotified, _ := time.Parse(time.RFC3339Nano, lastNotifiedStr)
	return State{
		Key:          key,
		Active:       activeInt != 0,
		Since:        since,
		LastNotified: lastNotified,
		Value:        val,
	}, true, nil
}

func (r *Repository) SaveState(ctx context.Context, s State) error {
	active := 0
	if s.Active {
		active = 1
	}
	sinceStr := s.Since.UTC().Format(time.RFC3339Nano)
	lastNotifiedStr := s.LastNotified.UTC().Format(time.RFC3339Nano)
	_, err := r.Database.ExecContext(ctx, "INSERT INTO notification_state(dedupe_key, active, since, last_notified_at, value) VALUES(?,?,?,?,?) ON CONFLICT(dedupe_key) DO UPDATE SET active=excluded.active, since=excluded.since, last_notified_at=excluded.last_notified_at, value=excluded.value",
		s.Key, active, sinceStr, lastNotifiedStr, s.Value)
	return err
}

func (r *Repository) DeleteState(ctx context.Context, key string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM notification_state WHERE dedupe_key=?", key)
	return err
}

func (r *Repository) Prune(ctx context.Context, before time.Time) (int64, error) {
	cutoff := before.UTC().Format(time.RFC3339Nano)
	res, err := r.Database.ExecContext(ctx, "DELETE FROM notifications WHERE at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
