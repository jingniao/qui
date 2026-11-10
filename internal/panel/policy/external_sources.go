package policy

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/vault"
)

type SourceFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

type ExternalSource struct {
	ID              string  `json:"id"`
	URL             string  `json:"url"`
	Format          string  `json:"format"`
	Behavior        string  `json:"behavior"`
	IntervalSeconds int     `json:"intervalSeconds"`
	NodeUsable      bool    `json:"nodeUsable"`
	ClientUsable    bool    `json:"clientUsable"`
	LastStatus      string  `json:"lastStatus"`
	LastErrorCode   string  `json:"lastErrorCode"`
	LastFetchAt     *string `json:"lastFetchAt"`
	LastSuccessAt   *string `json:"lastSuccessAt"`
	EntryCount      int     `json:"entryCount"`
	SkippedCount    int     `json:"skippedCount"`
}

type SourceInput struct {
	ID              string `json:"id,omitempty"`
	URL             string `json:"url"`
	Format          string `json:"format"`
	Behavior        string `json:"behavior"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

func (r *Repository) SaveSources(ctx context.Context, tx *sql.Tx, serviceID string, inputs []SourceInput) error {
	if tx == nil {
		var err error
		tx, err = r.Database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := r.saveSourcesTx(ctx, tx, serviceID, inputs); err != nil {
			return err
		}
		return tx.Commit()
	}
	return r.saveSourcesTx(ctx, tx, serviceID, inputs)
}

type existingSource struct {
	lastStatus     string
	lastErrorCode  string
	lastFetchAt    sql.NullString
	lastSuccessAt  sql.NullString
	entryCount     int
	skippedCount   int
	encryptedCache []byte
	createdAt      string
}

func (r *Repository) saveSourcesTx(ctx context.Context, tx *sql.Tx, serviceID string, inputs []SourceInput) error {
	if len(inputs) > 10 {
		return errors.New("maximum 10 sources allowed")
	}

	for _, in := range inputs {
		if !importer.ValidURL(in.URL) {
			return errors.New("invalid source url")
		}
		format := strings.ToLower(strings.TrimSpace(in.Format))
		if format != "yaml" && format != "text" && format != "mrs" {
			return errors.New("invalid source format")
		}
		behavior := strings.ToLower(strings.TrimSpace(in.Behavior))
		if behavior != "classical" && behavior != "domain" && behavior != "ipcidr" {
			return errors.New("invalid source behavior")
		}
		if format == "mrs" && behavior != "domain" && behavior != "ipcidr" {
			return errors.New("mrs format requires domain or ipcidr behavior")
		}
		if in.IntervalSeconds != 0 && (in.IntervalSeconds < 3600 || in.IntervalSeconds > 604800) {
			return errors.New("interval must be between 3600 and 604800 seconds")
		}
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, last_status, last_error_code, last_fetch_at, last_success_at, entry_count, skipped_count, encrypted_cache, created_at
		 FROM service_set_sources WHERE service_id=?`, serviceID)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := make(map[string]existingSource)
	for rows.Next() {
		var id string
		var ex existingSource
		if err := rows.Scan(&id, &ex.lastStatus, &ex.lastErrorCode, &ex.lastFetchAt, &ex.lastSuccessAt, &ex.entryCount, &ex.skippedCount, &ex.encryptedCache, &ex.createdAt); err != nil {
			return err
		}
		existing[id] = ex
	}
	rows.Close()

	if _, err := tx.ExecContext(ctx, "DELETE FROM service_set_sources WHERE service_id=?", serviceID); err != nil {
		return err
	}

	now := timestamp()
	for pos, in := range inputs {
		format := strings.ToLower(strings.TrimSpace(in.Format))
		behavior := strings.ToLower(strings.TrimSpace(in.Behavior))
		interval := in.IntervalSeconds
		if interval == 0 {
			interval = 86400
		}

		sourceID := in.ID
		var (
			lastStatus     = "pending"
			lastErrorCode  = ""
			lastFetchAt    sql.NullString
			lastSuccessAt  sql.NullString
			entryCount     = 0
			skippedCount   = 0
			encryptedCache []byte
			createdAt      = now
		)

		if sourceID != "" {
			if ex, ok := existing[sourceID]; ok {
				lastStatus = ex.lastStatus
				lastErrorCode = ex.lastErrorCode
				lastFetchAt = ex.lastFetchAt
				lastSuccessAt = ex.lastSuccessAt
				entryCount = ex.entryCount
				skippedCount = ex.skippedCount
				encryptedCache = ex.encryptedCache
				createdAt = ex.createdAt
			}
		} else {
			var err error
			sourceID, err = id.New()
			if err != nil {
				return err
			}
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO service_set_sources(id, service_id, url, format, behavior, interval_seconds, position, last_status, last_error_code, last_fetch_at, last_success_at, entry_count, skipped_count, encrypted_cache, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			sourceID, serviceID, in.URL, format, behavior, interval, pos, lastStatus, lastErrorCode, lastFetchAt, lastSuccessAt, entryCount, skippedCount, encryptedCache, createdAt, now); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) SourceEntries(ctx context.Context, serviceID string) (ParsedEntries, error) {
	rows, err := r.Database.QueryContext(ctx,
		`SELECT encrypted_cache FROM service_set_sources
		 WHERE service_id=? AND format <> 'mrs' AND encrypted_cache IS NOT NULL
		 ORDER BY position ASC`, serviceID)
	if err != nil {
		return ParsedEntries{}, err
	}
	defer rows.Close()

	var (
		domains      []string
		suffixes     []string
		keywords     []string
		cidrs        []string
		seenDomains  = make(map[string]bool)
		seenSuffixes = make(map[string]bool)
		seenKeywords = make(map[string]bool)
		seenCIDRs    = make(map[string]bool)
	)

	for rows.Next() {
		var encrypted []byte
		if err := rows.Scan(&encrypted); err != nil {
			return ParsedEntries{}, err
		}
		if len(encrypted) == 0 {
			continue
		}
		if r.Vault == nil {
			return ParsedEntries{}, errors.New("vault unconfigured")
		}
		plain, err := r.Vault.Open(encrypted)
		if err != nil {
			return ParsedEntries{}, err
		}

		var entries ParsedEntries
		if err := json.Unmarshal(plain, &entries); err != nil {
			return ParsedEntries{}, err
		}

		for _, d := range entries.Domains {
			if !seenDomains[d] {
				seenDomains[d] = true
				domains = append(domains, d)
			}
		}
		for _, s := range entries.Suffixes {
			if !seenSuffixes[s] {
				seenSuffixes[s] = true
				suffixes = append(suffixes, s)
			}
		}
		for _, k := range entries.Keywords {
			if !seenKeywords[k] {
				seenKeywords[k] = true
				keywords = append(keywords, k)
			}
		}
		for _, c := range entries.CIDRs {
			if !seenCIDRs[c] {
				seenCIDRs[c] = true
				cidrs = append(cidrs, c)
			}
		}
	}

	return ParsedEntries{
		Domains:  domains,
		Suffixes: suffixes,
		Keywords: keywords,
		CIDRs:    cidrs,
	}, rows.Err()
}

type SourceRefresher struct {
	Repository *Repository
	Fetcher    SourceFetcher
	Vault      *vault.Vault
	Now        func() time.Time
	OnChange   func(serviceID string)
}

func (s *SourceRefresher) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *SourceRefresher) Refresh(ctx context.Context, sourceID string) (ExternalSource, error) {
	var (
		serviceID          string
		url                string
		format             string
		behavior           string
		oldEncryptedCache  []byte
	)

	err := s.Repository.Database.QueryRowContext(ctx,
		`SELECT service_id, url, format, behavior, encrypted_cache FROM service_set_sources WHERE id=?`, sourceID).
		Scan(&serviceID, &url, &format, &behavior, &oldEncryptedCache)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSource{}, ErrNotFound
	}
	if err != nil {
		return ExternalSource{}, err
	}

	if format == "mrs" {
		return s.readSource(ctx, sourceID)
	}

	nowStr := s.now().UTC().Format(time.RFC3339Nano)

	body, err := s.Fetcher.Fetch(ctx, url)
	if err != nil {
		code := "fetch_failed"
		var fe *importer.FetchError
		if errors.As(err, &fe) {
			code = fe.Code
		}
		if _, updateErr := s.Repository.Database.ExecContext(ctx,
			`UPDATE service_set_sources SET last_status='failed', last_error_code=?, last_fetch_at=?, updated_at=? WHERE id=?`,
			code, nowStr, nowStr, sourceID); updateErr != nil {
			return ExternalSource{}, updateErr
		}
		return s.readSource(ctx, sourceID)
	}

	parsed, skipped, err := ParseRuleList(body, format, behavior)
	if err != nil {
		code := "parse_failed"
		if errors.Is(err, ErrEmptyList) {
			code = "parse_empty"
		}
		if _, updateErr := s.Repository.Database.ExecContext(ctx,
			`UPDATE service_set_sources SET last_status='failed', last_error_code=?, last_fetch_at=?, updated_at=? WHERE id=?`,
			code, nowStr, nowStr, sourceID); updateErr != nil {
			return ExternalSource{}, updateErr
		}
		return s.readSource(ctx, sourceID)
	}

	if s.Vault == nil {
		return ExternalSource{}, errors.New("vault required to seal cache")
	}
	raw, err := json.Marshal(parsed)
	if err != nil {
		return ExternalSource{}, err
	}
	sealed, err := s.Vault.Seal(raw)
	if err != nil {
		return ExternalSource{}, err
	}

	changed := false
	if len(oldEncryptedCache) == 0 {
		changed = true
	} else {
		oldPlain, err := s.Vault.Open(oldEncryptedCache)
		if err != nil || !bytes.Equal(oldPlain, raw) {
			changed = true
		}
	}

	entryCount := len(parsed.Domains) + len(parsed.Suffixes) + len(parsed.Keywords) + len(parsed.CIDRs)
	if _, err := s.Repository.Database.ExecContext(ctx,
		`UPDATE service_set_sources
		 SET last_status='ok', last_error_code='', last_fetch_at=?, last_success_at=?, entry_count=?, skipped_count=?, encrypted_cache=?, updated_at=?
		 WHERE id=?`,
		nowStr, nowStr, entryCount, skipped, sealed, nowStr, sourceID); err != nil {
		return ExternalSource{}, err
	}

	if changed && s.OnChange != nil {
		s.OnChange(serviceID)
	}

	return s.readSource(ctx, sourceID)
}

func (s *SourceRefresher) readSource(ctx context.Context, sourceID string) (ExternalSource, error) {
	var src ExternalSource
	var lastFetch, lastSuccess sql.NullString
	err := s.Repository.Database.QueryRowContext(ctx,
		`SELECT id, url, format, behavior, interval_seconds, last_status, last_error_code, last_fetch_at, last_success_at, entry_count, skipped_count
		 FROM service_set_sources WHERE id=?`, sourceID).
		Scan(&src.ID, &src.URL, &src.Format, &src.Behavior, &src.IntervalSeconds, &src.LastStatus, &src.LastErrorCode, &lastFetch, &lastSuccess, &src.EntryCount, &src.SkippedCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalSource{}, ErrNotFound
	}
	if err != nil {
		return ExternalSource{}, err
	}
	if lastFetch.Valid {
		src.LastFetchAt = &lastFetch.String
	}
	if lastSuccess.Valid {
		src.LastSuccessAt = &lastSuccess.String
	}
	src.ClientUsable = true
	src.NodeUsable = src.Format != "mrs"
	return src, nil
}

func (s *SourceRefresher) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	check := func() {
		rows, err := s.Repository.Database.QueryContext(ctx,
			`SELECT id FROM service_set_sources
			 WHERE format <> 'mrs'
			   AND (last_fetch_at IS NULL OR datetime(last_fetch_at, '+' || interval_seconds || ' seconds') <= datetime('now'))`)
		if err != nil {
			return
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			select {
			case <-ctx.Done():
				return
			default:
				_, _ = s.Refresh(ctx, id)
			}
		}
	}

	check()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}
