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

// The two ways the panel can store an import source. Both keep their payload in
// url_encrypted, so the kind is derived from what that column holds rather than
// from a column of its own (see sourceKind).
const (
	sourceKindURL     = "url"
	sourceKindContent = "content"
)

type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is read-only: "url" for a fetched HTTPS subscription, "content" for
	// content pasted into the panel once (the node list's 导入节点 action). The
	// node list uses it to tell 机场订阅 from 单独导入.
	Kind      string `json:"kind"`
	Interval  int    `json:"intervalSeconds"`
	LastFetch string `json:"lastFetchAt"`
	// LastSuccess is the latest attempt that committed endpoints; it stays empty
	// for a source that never succeeded.
	LastSuccess string `json:"lastSuccessAt"`
	Status      string `json:"lastStatus"`
	Error       string `json:"lastError"`
	// LatestResult is the bounded result of the latest attempt. It is nil for a
	// source written before migration 0133 and for a source whose URL just
	// changed, so an old failure is never explained with an invented cause.
	LatestResult *Result  `json:"latestResult,omitempty"`
	UserIDs      []string `json:"userIds"`
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

// sourceKind reports how a source was stored. A content import seals an empty
// URL (Refresh already returns early on it), so the decrypted plaintext is the
// only reliable signal: last_fetch_at/status are written for both kinds. A value
// that cannot be decrypted is reported as "url", the kind that owns a link.
func (s *Store) sourceKind(sealed []byte) string {
	raw, err := s.Vault.Open(sealed)
	if err != nil {
		return sourceKindURL
	}
	defer clear(raw)
	if len(raw) == 0 {
		return sourceKindContent
	}
	return sourceKindURL
}

func (s *Store) List(ctx context.Context) ([]Source, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id,name,interval_seconds,last_fetch_at,last_success_at,last_status,last_error,latest_result_json,url_encrypted FROM subscription_sources ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	items := []Source{}
	for rows.Next() {
		var p Source
		var stored string
		var sealed []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Interval, &p.LastFetch, &p.LastSuccess, &p.Status, &p.Error, &stored, &sealed); err != nil {
			rows.Close()
			return nil, err
		}
		p.Kind = s.sourceKind(sealed)
		p.LatestResult = readResult(stored)
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].UserIDs = []string{}
		r, e := s.reader().QueryContext(ctx, "SELECT DISTINCT a.user_id FROM user_node_group_grants a JOIN node_groups g ON g.id=a.group_id JOIN node_group_imports e ON e.group_id=g.id WHERE e.source_id=? AND g.enabled=1 AND g.subscription_visible=1 AND g.purpose IN ('subscription','mixed') ORDER BY a.user_id", items[i].ID)
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

// Save stores one source. A pasted import is analysed by the same pipeline a fetch
// uses, so the attempt's bounded result is returned with the stored source: the
// caller answers 400 for invalid input, 422 when nothing was importable and 500
// when the write failed, and none of those answers is a bare message.
func (s *Store) Save(ctx context.Context, key string, in Input) (string, Result, error) {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	prior := committedSource{}
	if key != "" {
		state, _, err := s.sourceState(ctx, key)
		if err != nil {
			return "", Result{}, err
		}
		prior = state
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 || in.Interval < 60 || in.Interval > 2592000 {
		return "", Result{}, ErrInput
	}
	if in.URL != "" && in.Content != "" {
		return "", Result{}, ErrInput
	}
	if key == "" && in.URL == "" && in.Content == "" {
		return "", Result{}, ErrInput
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if in.URL != "" && !ValidURL(in.URL) {
		return "", failureResult(stamp, key, prior, stageInput, "invalid_source_url"), ErrInput
	}
	var entries []proxy.Proxy
	attempt := Result{SourceID: key, Stage: stageParse}
	if in.Content != "" {
		attempt, entries = Analyze([]byte(in.Content))
		if attempt.Outcome == "failed" {
			// The paste was refused before any write, so its result must not read
			// like a fetch that excluded a source. It keeps the analysed entries'
			// reasons and reports the state the source keeps.
			return "", rejectedPaste(stamp, key, prior, attempt), ErrParse
		}
	}
	var err error
	if key == "" {
		if key, err = id.New(); err != nil {
			return "", Result{}, err
		}
	}
	// fail reports a storage failure with the state the source keeps.
	fail := func(reason error) (string, Result, error) {
		result, cause := commitFailure(key, stamp, prior, reason)
		return "", result, cause
	}
	sealed, err := s.Vault.Seal([]byte(in.URL))
	if err != nil {
		return fail(err)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_sources(id,name,url_encrypted,interval_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET name=excluded.name, interval_seconds=excluded.interval_seconds,updated_at=excluded.updated_at`, key, in.Name, sealed, in.Interval, stamp, stamp)
	if err != nil {
		return fail(err)
	}
	if in.URL != "" {
		if _, err = tx.ExecContext(ctx, "UPDATE subscription_sources SET url_encrypted=?,last_fetch_at='',last_success_at='',last_status='pending',last_error='',latest_result_json='' WHERE id=?", sealed, key); err != nil {
			return fail(err)
		}
	}
	// userIds is accepted for wire compatibility; group grants alone authorize access.
	if entries != nil {
		if err = s.replace(ctx, tx, key, entries, stamp); err != nil {
			return fail(err)
		}
		// A pasted import commits at once, so its attempt time is also its success
		// time and its counts describe what the panel just stored.
		attempt.SourceID, attempt.AttemptedAt, attempt.LastSuccessAt = key, stamp, &stamp
		attempt.Counts.Cached, attempt.Counts.Usable = pointer(len(entries)), pointer(len(entries))
		attempt.Message = resultMessage(attempt.Code, attempt.Status)
		stored, marshalErr := json.Marshal(attempt)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE subscription_sources SET url_encrypted=?,last_status='ok',last_fetch_at=?,last_success_at=?,last_error='',latest_result_json=? WHERE id=?", sealed, stamp, stamp, stored, key); err != nil {
			return fail(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return key, attempt, nil
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

// Refresh fetches and parses one source and records the attempt. It returns the
// attempt's result with any error, so a caller answers with a status code and the
// bounded reasons instead of a bare message. Recording a failure is not a
// successful refresh: the typed fetch or parse error is returned with it.
func (s *Store) Refresh(ctx context.Context, key string) (Result, error) {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	prior, sealed, err := s.sourceState(ctx, key)
	if err != nil {
		return Result{SourceID: key}, err
	}
	result := Result{SourceID: key, LastSuccessAt: prior.lastSuccess}
	// The attempt time is fixed before the payload is opened: an attempt that fails
	// on the panel's own storage still reports when it happened.
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := s.Vault.Open(sealed)
	if err != nil {
		// The row exists but its own sealed payload cannot be opened. That is a
		// storage failure, not a fetch: nothing was written, so the result reports
		// the state the source keeps and never echoes the vault error.
		failure := failureResult(stamp, key, prior, stageStore, "import_source_unreadable")
		failure.Counts = unchangedCounts(prior)
		return failure, ErrStore
	}
	defer clear(raw)
	if len(raw) == 0 {
		// A pasted import has no link to fetch; the panel refuses rather than
		// reporting an attempt that never happened.
		return result, ErrContentSource
	}
	result.AttemptedAt = stamp
	body, err := s.Fetcher.Fetch(ctx, string(raw))
	defer clear(body)
	// The caller cancelled or the request ended: keep whatever the last committed
	// attempt recorded instead of writing a failure for an unfinished task.
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		failure := failureResult(stamp, key, prior, stageConnect, "source_connect_failed")
		var typed *FetchError
		if errors.As(err, &typed) {
			failure.Stage, failure.Code, failure.Status = typed.Stage, typed.Code, statusPointer(typed.Status)
			failure.Message = resultMessage(failure.Code, failure.Status)
		}
		return s.recordFailure(ctx, key, failure, prior, err)
	}
	parsed, entries := Analyze(body)
	parsed.SourceID, parsed.AttemptedAt, parsed.LastSuccessAt = key, stamp, prior.lastSuccess
	if parsed.Outcome == "failed" {
		return s.recordFailure(ctx, key, parsed, prior, ErrParse)
	}
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return commitFailure(key, stamp, prior, err)
	}
	defer tx.Rollback()
	if err = s.replace(ctx, tx, key, entries, stamp); err != nil {
		return commitFailure(key, stamp, prior, err)
	}
	// A partial attempt still committed endpoints, so the source keeps last_status
	// ok and the outcome carries the warning; only a failure disables it.
	parsed.LastSuccessAt = &stamp
	parsed.Counts.Cached, parsed.Counts.Usable = pointer(len(entries)), pointer(len(entries))
	stored, err := json.Marshal(parsed)
	if err != nil {
		return commitFailure(key, stamp, prior, err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE subscription_sources SET last_fetch_at=?,last_success_at=?,last_status='ok',last_error='',latest_result_json=? WHERE id=?", stamp, stamp, stored, key); err != nil {
		return commitFailure(key, stamp, prior, err)
	}
	if err = tx.Commit(); err != nil {
		return commitFailure(key, stamp, prior, err)
	}
	return parsed, nil
}

// ErrContentSource reports a pasted import, which has no link to fetch.
var ErrContentSource = errors.New("import source has no subscription link")

// ErrInput reports a request the panel refused before any attempt: a missing or
// oversized name, an out-of-range interval, no content at all, or both a link and
// pasted content. Nothing was stored and no attempt happened.
var ErrInput = errors.New("import source input is invalid")

// ErrStore reports a failure of the panel's own storage: the source row exists but
// its sealed payload cannot be opened. That is corruption or a wrong key, not a
// failed fetch, so a caller must not present it as one.
var ErrStore = errors.New("import source storage is unreadable")

// committedSource is what a source has stored right now. It is captured once per
// attempt, so a storage failure reports the state the transaction rolled back to
// instead of the state the attempt hoped for.
type committedSource struct {
	known       bool
	status      string
	cached      int
	lastSuccess *string
}

// sourceState reads the committed state of one source and its sealed payload. Reads
// use the read pool, so an attempt never waits for the write connection to read.
func (s *Store) sourceState(ctx context.Context, key string) (committedSource, []byte, error) {
	var sealed []byte
	var status, lastSuccess string
	var cached int
	err := s.reader().QueryRowContext(ctx, "SELECT url_encrypted,last_status,last_success_at,(SELECT count(*) FROM imported_proxies WHERE source_id=subscription_sources.id) FROM subscription_sources WHERE id=?", key).Scan(&sealed, &status, &lastSuccess, &cached)
	if err != nil {
		return committedSource{}, nil, err
	}
	return committedSource{known: true, status: status, cached: cached, lastSuccess: optionalStamp(lastSuccess)}, sealed, nil
}

// commitFailure reports a storage failure. Nothing was written, so the result
// describes the state the source keeps: the previous success time and the previous
// committed cache, which stays usable when the source was usable before. The
// analysed endpoints are never presented as retained.
func commitFailure(key, attemptedAt string, prior committedSource, reason error) (Result, error) {
	result := Result{
		SourceID: key, Outcome: "failed", Stage: stageCommit, Code: "import_commit_failed",
		AttemptedAt: attemptedAt, LastSuccessAt: prior.lastSuccess,
		Counts: unchangedCounts(prior), Diagnostics: []Diagnostic{},
	}
	result.Message = resultMessage(result.Code, nil)
	return result, reason
}

// unchangedCounts reports what a source still holds after an attempt that changed
// nothing: the previous cache, usable only while the source is still ok. A source
// the panel never read keeps both counts unknown instead of claiming zero.
func unchangedCounts(prior committedSource) Counts {
	if !prior.known {
		return Counts{}
	}
	counts := Counts{Cached: pointer(prior.cached)}
	if prior.status == "ok" {
		counts.Usable = pointer(prior.cached)
	} else {
		counts.Usable = pointer(0)
	}
	return counts
}

// rejectedPaste reports a pasted document refused before any write. The analysis
// keeps its per-entry reasons and counts, but the source state did not change: no
// source was created, or the existing source keeps the status, success time and
// cache it had.
func rejectedPaste(stamp, key string, prior committedSource, analyzed Result) Result {
	analyzed.Outcome, analyzed.SourceID, analyzed.AttemptedAt = "failed", key, stamp
	analyzed.LastSuccessAt, analyzed.Code = prior.lastSuccess, "import_paste_rejected"
	if prior.known {
		analyzed.Code = "import_paste_unchanged"
	}
	counts := unchangedCounts(prior)
	analyzed.Counts.Cached, analyzed.Counts.Usable = counts.Cached, counts.Usable
	analyzed.Message = resultMessage(analyzed.Code, nil)
	return analyzed
}

// failureResult starts a failed attempt. Its counts stay unknown until the store
// knows how many endpoints the source keeps.
func failureResult(stamp, key string, prior committedSource, stage, code string) Result {
	return Result{
		SourceID: key, Outcome: "failed", Stage: stage, Code: code, Message: resultMessage(code, nil),
		AttemptedAt: stamp, LastSuccessAt: prior.lastSuccess, Diagnostics: []Diagnostic{},
	}
}

// recordFailure stores a failed attempt. The previous cache is kept so the Owner
// can still see what the source used to provide, but a failed source contributes
// nothing to subscriptions, so its usable count is zero. A write that fails changes
// nothing, and the result then reports the state the source still has.
func (s *Store) recordFailure(ctx context.Context, key string, result Result, prior committedSource, reason error) (Result, error) {
	result.Counts.Cached, result.Counts.Usable = pointer(prior.cached), pointer(0)
	stored, err := json.Marshal(result)
	if err != nil {
		return commitFailure(key, result.AttemptedAt, prior, err)
	}
	if _, err = s.Database.ExecContext(ctx, "UPDATE subscription_sources SET last_fetch_at=?,last_status='failed',last_error=?,latest_result_json=? WHERE id=?", result.AttemptedAt, result.Message, stored, key); err != nil {
		return commitFailure(key, result.AttemptedAt, prior, err)
	}
	return result, reason
}

// readResult parses a stored result. An unreadable or out-of-bounds payload is
// reported as "no recorded cause" rather than as a guess.
func readResult(stored string) *Result {
	if stored == "" || ValidateResult([]byte(stored)) != nil {
		return nil
	}
	var result Result
	if json.Unmarshal([]byte(stored), &result) != nil {
		return nil
	}
	return &result
}

func optionalStamp(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// pointer returns a count the JSON reports, where nil means "not determined".
func pointer(value int) *int { return &value }
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
					// A scheduled refresh records its outcome and reports the same
					// result the manual action returns; nothing else depends on it.
					_, _ = s.Refresh(ctx, p.ID)
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
	rows, err := s.reader().QueryContext(ctx, `SELECT COALESCE(p.id,''),s.id,p.payload_encrypted,s.last_status,group_concat(DISTINCT g.id) FROM subscription_sources s
 JOIN node_group_imports e ON e.source_id=s.id JOIN node_groups g ON g.id=e.group_id
 JOIN user_node_group_grants a ON a.group_id=g.id
 LEFT JOIN imported_proxies p ON p.source_id=s.id AND (e.proxy_key='' OR e.proxy_key=p.name||'|'||p.protocol)
 WHERE a.user_id=? AND g.enabled=1 AND g.subscription_visible=1 AND g.purpose IN ('subscription','mixed') GROUP BY s.id,p.id ORDER BY s.created_at,s.id,p.id`, viewer.UserID)
	if err != nil {
		return nil, nil, errors.New("imported proxy cache unavailable")
	}
	defer rows.Close()
	entries := []proxy.Proxy{}
	warnings := []proxy.Warning{}
	failed := map[string]bool{}
	notified := map[string]bool{}
	for rows.Next() {
		var key, src, status, groups string
		var encrypted []byte
		if err = rows.Scan(&key, &src, &encrypted, &status, &groups); err != nil {
			return nil, nil, errors.New("imported proxy cache unavailable")
		}
		if status != "ok" {
			if !failed[src] {
				warnings = append(warnings, proxy.SourceWarning("import:"+src, "Import source unavailable"))
				failed[src] = true
			}
			continue
		}
		if key == "" {
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
		labels := []string{}
		for _, id := range strings.Split(groups, ",") {
			labels = append(labels, "nodeGroup:"+id)
		}
		p.Origin = proxy.Origin{Source: "import:" + src, Ref: key, Labels: labels}
		entries = append(entries, p)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, errors.New("imported proxy cache unavailable")
	}
	return entries, warnings, nil
}

// Names supplies metadata for the client-policy editor without opening payloads.
func (s *Store) Names(ctx context.Context) (map[string]string, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id,name FROM subscription_sources ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, err
		}
		names[key] = name
	}
	return names, rows.Err()
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
		items = append(items, map[string]any{"id": key, "sourceId": src, "proxyKey": name + "|" + protocol, "name": name, "protocol": protocol, "server": p.Server, "port": p.Port})
	}
	return items, rows.Err()
}
