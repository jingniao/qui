package system

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/testutil/dbtest"
)

func TestAdminEntryValidation(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]bool{
		"/login":                      true,
		"/manage-7f3k":                true,
		"/a_b1":                       true,
		"/abc":                        true, // 4 characters
		"/ab":                         false,
		"/" + strings.Repeat("a", 63): true, // 64 characters
		"/" + strings.Repeat("a", 64): false,
		"manage":                      false,
		"/Manage":                     false,
		"/manage/x":                   false,
		"/manage.html":                false,
		"/":                           false,
		"/dashboard":                  false,
		"/portal":                     false,
		"/api":                        false,
		"/sub":                        false,
		"/agent-catalogue":            false,
		"/system":                     false,
	} {
		if got := ValidAdminEntry(path); got != want {
			t.Errorf("ValidAdminEntry(%q)=%v, want %v", path, got, want)
		}
	}
}

func TestAdminEntryRoundTripConflictAndReset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	// An upgraded panel has no row: the admin login stays at /login.
	if entry, err := r.ReadAdminEntry(ctx); err != nil || entry.Path != DefaultAdminEntry || entry.Revision != 0 {
		t.Fatalf("absent key: %+v %v", entry, err)
	}
	if _, err := r.SaveAdminEntry(ctx, AdminEntryInput{Path: "/users"}, "owner", "192.0.2.1"); !errors.Is(err, ErrAdminEntryInvalid) {
		t.Fatal("reserved path accepted:", err)
	}
	saved, err := r.SaveAdminEntry(ctx, AdminEntryInput{Path: " /manage-7f3k "}, "owner", "192.0.2.1")
	if err != nil || saved.Path != "/manage-7f3k" || saved.Revision != 1 || saved.UpdatedBy != "owner" {
		t.Fatalf("first save: %+v %v", saved, err)
	}
	if again, err := r.SaveAdminEntry(ctx, AdminEntryInput{Path: "/manage-7f3k", ExpectedRevision: 0}, "owner", ""); err != nil || again != saved {
		t.Fatalf("unchanged save: %+v %v", again, err)
	}
	if _, err := r.SaveAdminEntry(ctx, AdminEntryInput{Path: "/other-path", ExpectedRevision: 0}, "owner", ""); !errors.Is(err, ErrAdminEntryConflict) {
		t.Fatal("stale revision accepted:", err)
	}
	if current, err := r.ReadAdminEntry(ctx); err != nil || current != saved {
		t.Fatalf("read back: %+v %v", current, err)
	}
	if changed, err := r.ResetAdminEntry(ctx, "root (qui)"); err != nil || !changed {
		t.Fatal("reset:", changed, err)
	}
	if changed, err := r.ResetAdminEntry(ctx, "root (qui)"); err != nil || changed {
		t.Fatal("second reset changed something:", changed, err)
	}
	if entry, err := r.ReadAdminEntry(ctx); err != nil || entry.Path != DefaultAdminEntry {
		t.Fatalf("after reset: %+v %v", entry, err)
	}
	var saves, resets int
	database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改管理员登录地址' AND target='admin_entry'").Scan(&saves)
	database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='重置管理员登录地址' AND operator='root (qui)'").Scan(&resets)
	if saves != 1 || resets != 1 {
		t.Fatalf("audit rows: %d saves, %d resets", saves, resets)
	}
	// A damaged row is reported, and the reset still clears it.
	if _, err := database.Exec("INSERT INTO system_settings(key,value_json) VALUES('admin_entry','{\"path\":\"/api\",\"revision\":1}')"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAdminEntry(ctx); !errors.Is(err, ErrAdminEntryInvalid) {
		t.Fatal("damaged row accepted:", err)
	}
	if changed, err := r.ResetAdminEntry(ctx, "root (qui)"); err != nil || !changed {
		t.Fatal("reset of damaged row:", changed, err)
	}
}
