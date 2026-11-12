package backup

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// An archive whose identity table still holds a retired fingerprint would bring
// a replaced certificate back on restore, so it is refused. A retirement on its
// own stays consistent.
func TestArchiveRejectsRetiredFingerprintStillInstalled(t *testing.T) {
	t.Parallel()
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	var nodeID string
	if err := source.Database.QueryRow("SELECT id FROM nodes LIMIT 1").Scan(&nodeID); err != nil {
		t.Fatal(err)
	}
	retired := strings.Repeat("c", 64)
	if _, err := source.Database.Exec("INSERT INTO node_identity_retirements(fingerprint,node_id,recovery_id,retired_at) VALUES(?,?,?,?)", retired, nodeID, "recovery-1", "2026-11-06T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	consistent, err := source.Create(ctx, directory, "identity_retirement_01", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Examine(ctx, directory, filepath.Join(directory, consistent.Manifest.ID+".pcb"), testPassword, func(Result, string) error { return nil }); err != nil {
		t.Fatalf("a consistent archive was rejected: %v", err)
	}
	if _, err := source.Database.Exec("INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,?,?,?)", nodeID, "certificate", retired, "2999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	// Create revalidates the archive it wrote, so the conflict is refused here.
	if _, err := source.Create(ctx, directory, "identity_retirement_02", "owner", testPassword); !errors.Is(err, ErrArchive) {
		t.Fatalf("an archive reviving a retired certificate was accepted: %v", err)
	}
}
