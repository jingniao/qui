package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/panelstore"
)

func adminEntryOutput(t *testing.T, path, action string) string {
	t.Helper()
	var out bytes.Buffer
	if err := adminEntry(context.Background(), path, action, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestAdminEntryShowDefaultAndResetAfterSavedValue(t *testing.T) {
	c, path, _, database := ownerCredentialDatabase(t)
	if got := adminEntryOutput(t, path, "show"); got != "admin login path: /login\n" {
		t.Fatalf("unexpected show output %q", got)
	}
	if got := adminEntryOutput(t, path, "reset"); got != "admin login path already /login\n" {
		t.Fatalf("unexpected reset output %q", got)
	}
	repository := &system.Repository{Database: database}
	if _, err := repository.SaveAdminEntry(context.Background(), system.AdminEntryInput{Path: "/my-admin"}, "owner", "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if got := adminEntryOutput(t, path, "show"); got != "admin login path: /my-admin\n" {
		t.Fatalf("unexpected show output %q", got)
	}
	if got := adminEntryOutput(t, path, "reset"); got != "admin login path reset to /login\n" {
		t.Fatalf("unexpected reset output %q", got)
	}
	entry, err := repository.ReadAdminEntry(context.Background())
	if err != nil || entry.Path != system.DefaultAdminEntry {
		t.Fatal("reset did not restore the default", entry, err)
	}
	var operations int
	if err := database.QueryRow("SELECT count(*) FROM admin_operations WHERE operator='root (qui)' AND action='重置管理员登录地址'").Scan(&operations); err != nil || operations != 1 {
		t.Fatal("operation log entry missing", operations, err)
	}
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal("admin-entry retained ownership", err)
	}
	owner.Close()
}

func TestAdminEntryShowReportsDamagedRowAndResetClearsIt(t *testing.T) {
	_, path, _, database := ownerCredentialDatabase(t)
	if _, err := database.Exec("INSERT INTO system_settings(key,value_json) VALUES('admin_entry','{broken')"); err != nil {
		t.Fatal(err)
	}
	if got := adminEntryOutput(t, path, "show"); !strings.Contains(got, "invalid") || !strings.Contains(got, "--admin-entry reset") {
		t.Fatalf("damaged row not reported: %q", got)
	}
	if got := adminEntryOutput(t, path, "reset"); got != "admin login path reset to /login\n" {
		t.Fatalf("unexpected reset output %q", got)
	}
}

func TestAdminEntryRefusesWhilePanelIsRunning(t *testing.T) {
	c, path, _, _ := ownerCredentialDatabase(t)
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var out bytes.Buffer
	err = adminEntry(context.Background(), path, "reset", &out)
	if err == nil || !strings.Contains(err.Error(), "the panel is running; stop proxy-panel.service first") {
		t.Fatal("a running panel did not refuse the reset", err)
	}
}

func TestAdminEntryFlagRejectsBadArguments(t *testing.T) {
	_, path, _, _ := ownerCredentialDatabase(t)
	for _, args := range [][]string{
		{"--admin-entry", "show"},
		{"--admin-entry", "other", "--config", path},
		{"--admin-entry", "", "--config", path},
		{"--admin-entry", "show", "--config", path, "--check-config"},
		{"--admin-entry", "show", "--config", path, "--version"},
		{"--admin-entry", "show", "--config", path, "positional"},
	} {
		if err := runWithContext(context.Background(), args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
