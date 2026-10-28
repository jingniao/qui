package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
)

type Store struct {
	Database *sql.DB
	Reader   *sql.DB
	Vault    *vault.Vault
	Fetcher  Fetcher
	refresh  sync.Mutex
}

// Subscription reads must not wait for the single writer connection. The
// panel supplies the same read-only pool used by its published source.
func (s *Store) reader() *sql.DB {
	if s.Reader != nil {
		return s.Reader
	}
	return s.Database
}

type Source struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Interval  int      `json:"intervalSeconds"`
	LastFetch string   `json:"lastFetchAt"`
	Status    string   `json:"lastStatus"`
	Error     string   `json:"lastError"`
	UserIDs   []string `json:"userIds"`
}
type Input struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Content  string   `json:"content"`
	Interval int      `json:"intervalSeconds"`
	UserIDs  []string `json:"userIds"`
}

func (s *Store) ID() string     { return "imported" }
func (s *Store) Optional() bool { return true }
func (s *Store) List(ctx context.Context) ([]Source, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id,name,interval_seconds,last_fetch_at,last_status,last_error FROM subscription_sources ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	items := []Source{}
	for rows.Next() {
		var p Source
		if err := rows.Scan(&p.ID, &p.Name, &p.Interval, &p.LastFetch, &p.Status, &p.Error); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].UserIDs = []string{}
		r, e := s.reader().QueryContext(ctx, "SELECT user_id FROM user_source_grants WHERE source_id=? ORDER BY user_id", items[i].ID)
		if e != nil {
			return nil, e
		}
		for r.Next() {
			var u string
			if e = r.Scan(&u); e != nil {
				r.Close()
				return nil, e
			}
			items[i].UserIDs = append(items[i].UserIDs, u)
		}
		e = r.Err()
		r.Close()
		if e != nil {
			return nil, e
		}
	}
	return items, nil
}
func (s *Store) Save(ctx context.Context, key string, in Input) (string, error) {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	if key != "" {
		var exists int
		if err := s.Database.QueryRowContext(ctx, "SELECT count(*) FROM subscription_sources WHERE id=?", key).Scan(&exists); err != nil {
			return "", err
		}
		if exists != 1 {
			return "", sql.ErrNoRows
		}
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || in.Interval < 60 || in.Interval > 2592000 || len(in.UserIDs) > 100 {
		return "", ErrParse
	}
	if in.URL != "" && !ValidURL(in.URL) {
		return "", ErrFetch
	}
	if in.URL != "" && in.Content != "" {
		return "", ErrParse
	}
	var entries []proxy.Proxy
	var err error
	if in.Content != "" {
		entries, _, err = Parse([]byte(in.Content))
		if err != nil {
			return "", err
		}
	}
	if key == "" && in.URL == "" && in.Content == "" {
		return "", ErrParse
	}
	if key == "" {
		key, err = id.New()
		if err != nil {
			return "", err
		}
	}
	sealed, err := s.Vault.Seal([]byte(in.URL))
	if err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name, interval_seconds=excluded.interval_seconds,updated_at=excluded.updated_at`, key, in.Name, sealed, in.Interval, stamp, stamp)
	if err != nil {
		return "", err
	}
	if in.URL != "" {
		if _, err = tx.ExecContext(ctx, "UPDATE subscription_sources SET url_encrypted=?,last_fetch_at='',last_status='pending',last_error='' WHERE id=?", sealed, key); err != nil {
			return "", err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM user_source_grants WHERE source_id=?", key); err != nil {
		return "", err
	}
	for _, u := range in.UserIDs {
		if _, err = tx.ExecContext(ctx, "INSERT INTO user_source_grants(user_id,source_id) VALUES(?,?)", u, key); err != nil {
			return "", err
		}
	}
	if entries != nil {
		if err = s.replace(ctx, tx, key, entries, stamp); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE subscription_sources SET url_encrypted=?,last_status='ok',last_fetch_at=?,last_error='' WHERE id=?", sealed, stamp, key); err != nil {
			return "", err
		}
	}
	return key, tx.Commit()
}
func (s *Store) replace(ctx context.Context, tx *sql.Tx, key string, entries []proxy.Proxy, stamp string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM imported_proxies WHERE source_id=?", key); err != nil {
		return err
	}
	// Content identities cannot silently retarget a published chain after reorder.
	seen := map[string]bool{}
	for _, p := range entries {
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		encrypted, err := s.Vault.Seal(raw)
		pid := key + "-" + id.Hash(string(raw))
		clear(raw)
		if err != nil {
			return err
		}
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if _, err = tx.ExecContext(ctx, "INSERT INTO imported_proxies(id,source_id,name,protocol,payload_encrypted,fetched_at) VALUES(?,?,?,?,?,?)", pid, key, p.Name, p.Protocol, encrypted, stamp); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Delete(ctx context.Context, key string) error {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	result, err := s.Database.ExecContext(ctx, "DELETE FROM subscription_sources WHERE id=?", key)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) Refresh(ctx context.Context, key string) error {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	var sealed []byte
	if err := s.Database.QueryRowContext(ctx, "SELECT url_encrypted FROM subscription_sources WHERE id=?", key).Scan(&sealed); err != nil {
		return err
	}
	raw, err := s.Vault.Open(sealed)
	if err != nil {
		return ErrFetch
	}
	defer clear(raw)
	if len(raw) == 0 {
		return nil
	}
	body, err := s.Fetcher.Fetch(ctx, string(raw))
	defer clear(body)
	var entries []proxy.Proxy
	if err == nil {
		entries, _, err = Parse(body)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		_, writeErr := s.Database.ExecContext(ctx, "UPDATE subscription_sources SET last_fetch_at=?,last_status='failed',last_error='刷新失败；已停用该来源的缓存节点' WHERE id=?", stamp, key)
		return writeErr
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.replace(ctx, tx, key, entries, stamp); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE subscription_sources SET last_fetch_at=?,last_status='ok',last_error='' WHERE id=?", stamp, key); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		items, err := s.List(ctx)
		if err == nil {
			for _, p := range items {
				if ctx.Err() != nil {
					return
				}
				last, _ := time.Parse(time.RFC3339Nano, p.LastFetch)
				if time.Since(last) >= time.Duration(p.Interval)*time.Second {
					_ = s.Refresh(ctx, p.ID)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Store) Proxies(ctx context.Context, viewer source.Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT COALESCE(p.id,''),s.id,p.payload_encrypted,s.last_status FROM subscription_sources s JOIN user_source_grants g ON g.source_id=s.id LEFT JOIN imported_proxies p ON p.source_id=s.id WHERE g.user_id=? ORDER BY s.created_at,s.id,p.id`, viewer.UserID)
	if err != nil {
		return nil, nil, errors.New("imported proxy cache unavailable")
	}
	defer rows.Close()
	entries := []proxy.Proxy{}
	warnings := []proxy.Warning{}
	failed := map[string]bool{}
	notified := map[string]bool{}
	for rows.Next() {
		var key, src, status string
		var encrypted []byte
		if err = rows.Scan(&key, &src, &encrypted, &status); err != nil {
			return nil, nil, errors.New("imported proxy cache unavailable")
		}
		if status != "ok" {
			if !failed[src] {
				warnings = append(warnings, proxy.SourceWarning("import:"+src, "Import source unavailable"))
				failed[src] = true
			}
			continue
		}
		if !notified[src] {
			warnings = append(warnings, proxy.SourceWarning("import:"+src, "Only supported valid entries are imported; unsupported options and entries are omitted."))
			notified[src] = true
		}
		raw, e := s.Vault.Open(encrypted)
		var p proxy.Proxy
		if e == nil {
			e = json.Unmarshal(raw, &p)
		}
		clear(raw)
		if e != nil || !valid(p) {
			warnings = append(warnings, proxy.SourceWarning("import:"+src, "Imported proxy unavailable"))
			continue
		}
		if !viewer.AllowsProtocol(p.Protocol) {
			continue
		}
		p.Origin = proxy.Origin{Source: "import:" + src, Ref: key}
		entries = append(entries, p)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, errors.New("imported proxy cache unavailable")
	}
	return entries, warnings, nil
}

// Items exposes metadata only to the canvas, never credentials or source URLs.
func (s *Store) Items(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id,source_id,name,protocol,payload_encrypted FROM imported_proxies ORDER BY source_id,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var key, src, name, protocol string
		var sealed []byte
		if err = rows.Scan(&key, &src, &name, &protocol, &sealed); err != nil {
			return nil, err
		}
		raw, e := s.Vault.Open(sealed)
		var p proxy.Proxy
		if e == nil {
			e = json.Unmarshal(raw, &p)
		}
		clear(raw)
		if e != nil {
			return nil, errors.New("import metadata unavailable")
		}
		items = append(items, map[string]any{"id": key, "sourceId": src, "name": name, "protocol": protocol, "server": p.Server, "port": p.Port})
	}
	return items, rows.Err()
}
