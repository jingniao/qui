package backup

import (
	"context"
	"testing"
)

func TestBackupRejectsMalformedPublicAddresses(t *testing.T) {
	t.Parallel()
	source, _, _ := sourceFixture(t)
	ctx := context.Background()

	// Initially valid
	if err := validateNodePublicAddresses(ctx, source.Database); err != nil {
		t.Fatalf("expected valid database to pass: %v", err)
	}

	for _, invalid := range []string{
		"not json",
		`[{"address":"203.0.113.1","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.2","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.3","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.4","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.5","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.6","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.7","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.8","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.9","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.10","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.11","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.12","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.13","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.14","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.15","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.16","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"},{"address":"203.0.113.17","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"}]`,
		`[{"address":"not-an-ip","source":"peer","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"}]`,
		`[{"address":"203.0.113.1","source":"invalid_source","family":"ipv4","lastSeen":"2026-11-09T00:00:00Z"}]`,
		`[{"address":"203.0.113.1","source":"peer","family":"invalid_fam","lastSeen":"2026-11-09T00:00:00Z"}]`,
		`[{"address":"203.0.113.1","source":"peer","family":"ipv4","lastSeen":"bad-date"}]`,
	} {
		if _, err := source.Database.Exec("UPDATE nodes SET public_addresses_json=?", invalid); err != nil {
			t.Fatal(err)
		}
		if err := validateNodePublicAddresses(ctx, source.Database); err == nil {
			t.Fatalf("expected malformed public_addresses_json %q to be rejected", invalid)
		}
	}
}
