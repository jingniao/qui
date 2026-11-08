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
	if err != nil || unset.Host != "" || unset.HTTPPort != 0 || unset.GRPCHost != "" || unset.GRPCPort != 0 || unset.Revision != 0 || unset.UpdatedAt != "" {
		t.Fatalf("absent key must read as unset: %+v %v", unset, err)
	}
	saved, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{Host: "  panel.example.com  ", HTTPPort: 22550, GRPCHost: "  grpc.example.com ", GRPCPort: 18880}, "owner", "192.0.2.10")
	if err != nil || !changed || saved.Revision != 1 || saved.Host != "panel.example.com" || saved.HTTPPort != 22550 || saved.GRPCHost != "grpc.example.com" || saved.GRPCPort != 18880 || saved.UpdatedBy != "owner" {
		t.Fatalf("first save: %+v %v %v", saved, changed, err)
	}
	current, err := r.ReadInstallEndpoint(ctx)
	if err != nil || current != saved {
		t.Fatalf("read back: %+v %v", current, err)
	}
	// An unchanged save keeps the revision and must not duplicate the audit row.
	if again, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{Host: "panel.example.com", HTTPPort: 22550, GRPCHost: "grpc.example.com", GRPCPort: 18880, ExpectedRevision: 1}, "owner", "192.0.2.10"); err != nil || changed || again != saved {
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
	if err != nil || !changed || portOnly.Revision != 2 || portOnly.Host != "" || portOnly.HTTPPort != 22550 || portOnly.GRPCHost != "" || portOnly.GRPCPort != 0 {
		t.Fatalf("port-only save: %+v %v %v", portOnly, changed, err)
	}
	// The gRPC address and port are independently optional: saving only them
	// keeps the Web values cleared and lets the dialogs fall back to the panel
	// address and the port the panel actually listens on.
	grpcOnly, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{GRPCHost: "grpc.example.com", GRPCPort: 443, ExpectedRevision: 2}, "owner", "192.0.2.10")
	if err != nil || !changed || grpcOnly.Revision != 3 || grpcOnly.Host != "" || grpcOnly.HTTPPort != 0 || grpcOnly.GRPCHost != "grpc.example.com" || grpcOnly.GRPCPort != 443 {
		t.Fatalf("gRPC-only save: %+v %v %v", grpcOnly, changed, err)
	}
	// Clearing everything is a real change and keeps a revision history.
	cleared, changed, err := r.SaveInstallEndpoint(ctx, InstallEndpointInput{ExpectedRevision: 3}, "owner", "192.0.2.10")
	if err != nil || !changed || cleared.Revision != 4 || cleared.Host != "" || cleared.HTTPPort != 0 || cleared.GRPCHost != "" || cleared.GRPCPort != 0 {
		t.Fatalf("clear: %+v %v %v", cleared, changed, err)
	}
}

// A value written by v1.4.2 has no grpcPort field: it must keep reading as
// "gRPC port unset" (0), so upgrading the panel does not invalidate a saved
// endpoint or silently change the generated command.
func TestInstallEndpointReadsLegacyRowWithoutGRPCPort(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { database.Close() }()
	r := &Repository{Database: database}
	if _, err := database.Exec("INSERT INTO system_settings(key,value_json) VALUES('install_endpoint','{\"host\":\"panel.example.com\",\"httpPort\":22550,\"revision\":2,\"updatedAt\":\"2026-11-08T00:00:00Z\",\"updatedBy\":\"owner\"}')"); err != nil {
		t.Fatal(err)
	}
	endpoint, err := r.ReadInstallEndpoint(ctx)
	if err != nil || endpoint.Host != "panel.example.com" || endpoint.HTTPPort != 22550 || endpoint.GRPCHost != "" || endpoint.GRPCPort != 0 || endpoint.Revision != 2 {
		t.Fatalf("legacy row: %+v %v", endpoint, err)
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
		{Host: "panel.example.com", GRPCPort: -1},
		{Host: "panel.example.com", GRPCPort: 65536},
		// node-install.sh refuses --http-port == --grpc-port, so the same rule is
		// enforced before a value that can never install a node is stored.
		{Host: "panel.example.com", HTTPPort: 22550, GRPCPort: 22550},
		// The gRPC address follows the same host rule as the panel address: it
		// becomes --grpc-host verbatim in the generated command.
		{Host: "panel.example.com", GRPCHost: "https://grpc.example.com"},
		{Host: "panel.example.com", GRPCHost: "grpc.example.com:443"},
		{Host: "panel.example.com", GRPCHost: "2001:db8::1"},
		{Host: "panel.example.com", GRPCHost: ".grpc.example.com"},
		{Host: "panel.example.com", GRPCHost: "grpc..example.com"},
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
		{Host: "panel.example.com", HTTPPort: 22550, GRPCPort: 18880},
		{Host: "panel.example.com", GRPCPort: 443},
		{Host: "panel.example.com", HTTPPort: 22550, GRPCHost: "grpc.example.com", GRPCPort: 18880},
		{Host: "panel.example.com", GRPCHost: "203.0.113.10", GRPCPort: 443},
	} {
		input.ExpectedRevision = revision
		saved, changed, err := r.SaveInstallEndpoint(ctx, input, "owner", "192.0.2.10")
		if err != nil || !changed || saved.Host != input.Host || saved.HTTPPort != input.HTTPPort || saved.GRPCHost != input.GRPCHost || saved.GRPCPort != input.GRPCPort {
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
