package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strings"
	"time"
)

var ErrAdminEntryInvalid = errors.New("管理员登录地址须为以 / 开头的单段路径，4–64 个字符，只含小写字母、数字、- 与 _，且不得与面板已用的路径相同")
var ErrAdminEntryConflict = errors.New("管理员登录地址已被修改，请刷新后再保存")

// DefaultAdminEntry is the admin login path when none is saved, which keeps
// the address of a panel upgraded from before v1.5.4 unchanged.
const DefaultAdminEntry = "/login"

// adminEntryKey is the single system_settings row this feature owns.
const adminEntryKey = "admin_entry"

var adminEntryPattern = regexp.MustCompile(`^/[a-z0-9_-]{3,63}$`)

// ReservedAdminEntrySegments are the first path segments the panel already
// serves: server routes and the first segment of every page route in
// web/src/app/router.ts (web/scripts/admin-entry-selftest.mjs checks the router
// against this list). The admin login path may not take one of them, or the
// page there would turn into the admin login page.
var ReservedAdminEntrySegments = []string{
	// Server routes.
	"api", "sub", "install", "agent-catalogue", "assets", "healthz",
	// Page routes.
	"portal", "portal-preview", "setup", "403", "404",
	"dashboard", "users", "devices", "subscriptions", "nodes", "node-groups",
	"topology", "proxy", "routing", "access", "system",
}

// AdminEntry is the path the admin login page is served at. Only that path gets
// the admin marker in index.html (see the static handler); every other unknown
// path shows "page not found", so the address is not disclosed. This hides the
// entry from casual scans only: the admin API, authentication, CSRF and login
// rate limits are unchanged.
type AdminEntry struct {
	Path      string `json:"path"`
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	UpdatedBy string `json:"updatedBy"`
}

// AdminEntryInput is the write shape.
type AdminEntryInput struct {
	Path             string `json:"path"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// ValidAdminEntry reports whether path can be the admin login path.
func ValidAdminEntry(path string) bool {
	if !adminEntryPattern.MatchString(path) {
		return false
	}
	segment := strings.TrimPrefix(path, "/")
	for _, reserved := range ReservedAdminEntrySegments {
		if segment == reserved {
			return false
		}
	}
	return true
}

func readAdminEntry(ctx context.Context, tx *sql.Tx) (AdminEntry, error) {
	entry := AdminEntry{Path: DefaultAdminEntry}
	var encoded string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", adminEntryKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return entry, nil
	}
	if err != nil {
		return entry, err
	}
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entry); err != nil || decoder.Decode(&struct{}{}) != io.EOF || entry.Revision < 1 || !ValidAdminEntry(entry.Path) {
		return AdminEntry{}, ErrAdminEntryInvalid
	}
	if _, err := time.Parse(time.RFC3339Nano, entry.UpdatedAt); err != nil {
		return AdminEntry{}, ErrAdminEntryInvalid
	}
	return entry, nil
}

// ReadAdminEntry returns the saved admin login path, or DefaultAdminEntry with
// revision 0 when none is saved.
func (r *Repository) ReadAdminEntry(ctx context.Context) (AdminEntry, error) {
	tx, err := r.reader().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminEntry{}, err
	}
	defer tx.Rollback()
	entry, err := readAdminEntry(ctx, tx)
	if err != nil {
		return entry, err
	}
	return entry, tx.Commit()
}

// SaveAdminEntry writes the path under one revision like SaveInstallEndpoint:
// an unchanged save keeps the revision and writes no audit row, a stale
// expectedRevision is a conflict.
func (r *Repository) SaveAdminEntry(ctx context.Context, input AdminEntryInput, actor, ip string) (AdminEntry, error) {
	path := strings.TrimSpace(input.Path)
	if !ValidAdminEntry(path) || input.ExpectedRevision < 0 || actor == "" {
		return AdminEntry{}, ErrAdminEntryInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return AdminEntry{}, err
	}
	defer tx.Rollback()
	current, err := readAdminEntry(ctx, tx)
	if err != nil {
		return current, err
	}
	if path == current.Path {
		return current, nil
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return current, ErrAdminEntryConflict
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	next := AdminEntry{Path: path, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: actor}
	data, err := json.Marshal(next)
	if err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", adminEntryKey, string(data)); err != nil {
		return next, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改管理员登录地址',?,'成功',?)", stamp, actor, adminEntryKey, ip); err != nil {
		return next, err
	}
	return next, tx.Commit()
}

// ResetAdminEntry removes the saved path so the admin login is back at
// DefaultAdminEntry. It is the root-only recovery behind the server's qui menu
// for an Owner who forgot the address, and reports whether anything changed.
// The settings row is also readable when stored badly, so a corrupt value can
// be cleared too.
func (r *Repository) ResetAdminEntry(ctx context.Context, actor string) (bool, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM system_settings WHERE key=?", adminEntryKey)
	if err != nil {
		return false, err
	}
	removed, err := result.RowsAffected()
	if err != nil || removed == 0 {
		return false, err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'重置管理员登录地址',?,'成功','')", stamp, actor, adminEntryKey); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
