package system

import (
	"context"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
)

// The install endpoint is one system_settings row: an absent row reads as unset
// (host "", port 0, revision 0) and the first save becomes revision 1.
func TestInstallEndpointRoundTripValidationAndAudit(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	r := &Repository{Database: database}
	unset, err := r.ReadInstallEndpoint(ctx)
	if err != nil || unset.Host != "" || unset.HTTPPort != 0 || unset.Revision != 0 || unset.UpdatedAt != "" {
		t.Fatalf("absent key must read as unset: %+v %v", unset, err)
	}
	saved, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{Host: "  panel.example.com  ", HTTPPort: 22550}, "owner", "192.0.2.10")
	if err != nil || !changed || saved.Revision != 1 || saved.Host != "panel.example.com" || saved.HTTPPort != 22550 || saved.UpdatedBy != "owner" {
		t.Fatalf("first save: %+v %v %v", saved, changed, err)
	}
	current, err := r.ReadInstallEndpoint(ctx)
	if err != nil || current != saved {
		t.Fatalf("read back: %+v %v", current, err)
	}
	// An unchanged save keeps the revision and must not duplicate the audit row.
	if again, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{Host: "panel.example.com", HTTPPort: 22550, ExpectedRevision: 1}, "owner", "192.0.2.10"); err != nil || changed || again != saved {
		t.Fatalf("unchanged save bumped the revision: %+v %v %v", again, changed, err)
	}
	var audits int
	if err := database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改节点安装连接地址' AND target='install_endpoint' AND result='成功'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audit rows: %d", audits)
	}
	// A stale revision is refused and keeps the stored value.
	if _, _, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{Host: "other.example.com", HTTPPort: 443}, "owner", "192.0.2.10"); !errors.Is(err, ErrInstallEndpointConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if current, err := r.ReadInstallEndpoint(ctx); err != nil || current != saved {
		t.Fatalf("stale save changed the endpoint: %+v %v", current, err)
	}
	// Both fields are independently optional: the port can be saved alone and
	// the dialogs then keep the browser hostname.
	portOnly, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{Host: "", HTTPPort: 22550, ExpectedRevision: 1}, "owner", "192.0.2.10")
	if err != nil || !changed || portOnly.Revision != 2 || portOnly.Host != "" || portOnly.HTTPPort != 22550 {
		t.Fatalf("port-only save: %+v %v %v", portOnly, changed, err)
	}
	// Clearing everything is a real change and keeps a revision history.
	cleared, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{ExpectedRevision: 2}, "owner", "192.0.2.10")
	if err != nil || !changed || cleared.Revision != 3 || cleared.Host != "" || cleared.HTTPPort != 0 {
		t.Fatalf("clear: %+v %v %v", cleared, changed, err)
	}
}

// The panel address is edited once and reused: invalid hosts and ports are
// refused instead of stored, including a bare IPv6 literal, which
// deploy/linux/node-install.sh rejects for --panel (the pattern allows only
// letters, digits, dots and hyphens).
func TestInstallEndpointRejectsInvalidHostAndPort(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	r := &Repository{Database: database}
	for _, input := range []InstallEndpointInput{
		{Host: "https://panel.example.com", HTTPPort: 22550},
		{Host: "panel.example.com:22550", HTTPPort: 22550},
		{Host: "2001:db8::1", HTTPPort: 22550},
		{Host: "[2001:db8::1]", HTTPPort: 22550},
		{Host: "panel example.com", HTTPPort: 22550},
		{Host: ".panel.example.com", HTTPPort: 22550},
		{Host: "panel.example.com.", HTTPPort: 22550},
		{Host: "panel..example.com", HTTPPort: 22550},
		{Host: "-panel.example.com", HTTPPort: 22550},
		{Host: "panel-.example.com", HTTPPort: 22550},
		{Host: "panel.example.com", HTTPPort: -1},
		{Host: "panel.example.com", HTTPPort: 65536},
	} {
		if _, _, err := r.SaveInstallEndpoint(ctx, input, "owner", "192.0.2.10"); !errors.Is(err, ErrInstallEndpointInvalid) {
			t.Fatalf("invalid endpoint accepted: %+v %v", input, err)
		}
	}
	revision := int64(0)
	for _, input := range []InstallEndpointInput{
		{Host: "panel.example.com"},
		{Host: "203.0.113.10", HTTPPort: 22550},
		{Host: "PANEL.Example.COM", HTTPPort: 65535},
		{Host: "edge-1.example.com", HTTPPort: 1},
	} {
		input.ExpectedRevision = revision
		saved, changed, err := r.SaveInstallEndpoint(ctx, input, "owner", "192.0.2.10")
		if err != nil || !changed || saved.Host != input.Host || saved.HTTPPort != input.HTTPPort {
			t.Fatalf("valid endpoint refused: %+v %+v %v %v", input, saved, changed, err)
		}
		revision = saved.Revision
	}
	// A corrupt stored value surfaces as invalid instead of being repaired.
	if _, err := database.Exec("UPDATE system_settings SET value_json='{\"host\":\"a b\",\"httpPort\":22550,\"revision\":1,\"updatedAt\":\"2026-11-08T00:00:00Z\",\"updatedBy\":\"owner\"}' WHERE key='install_endpoint'"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadInstallEndpoint(ctx); !errors.Is(err, ErrInstallEndpointInvalid) {
		t.Fatalf("corrupt stored endpoint accepted: %v", err)
	}
}
