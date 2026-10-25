package user

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

// TestRestoreKeepsTombstonesCreatedAfterTheBackup is the restore counterpart of
// the delete hooks: a credential deleted after the backup was taken is gone from
// the restored database, and only the tombstone the live database recorded keeps
// the counters the nodes still send for it acceptable. Losing it turned those
// batches into unknown identities the agent could never repair.
func TestRestoreKeepsTombstonesCreatedAfterTheBackup(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationKey, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	live := &Repository{Database: database, Vault: configurationKey}

	// A backup of the empty panel, taken before anything was created.
	backup := filepath.Join(directory, "backup.db")
	if _, err := database.ExecContext(ctx, "VACUUM INTO ?", backup); err != nil {
		t.Fatal(err)
	}

	// After the backup: one user with one device, which is then deleted again.
	account, err := live.Save(ctx, "", Input{Username: "restored_gone", DisplayName: "Gone", Status: "active", MaxDevices: 1, MaxConnections: 8, AllowedProtocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := live.CreateDevice(ctx, account.ID, DeviceInput{Name: "Laptop", Protocols: []string{"vless"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := device.Credentials[0]
	revision := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: "node-a", Credentials: []desiredstate.Credential{{ID: credential.ID, UserID: account.ID, DeviceID: device.ID, Protocol: "vless", Secret: "unused-test-secret"}}}
	if err := live.Delete(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, live, "SELECT count(*) FROM credential_tombstones WHERE credential_id=?", credential.ID); got != 1 {
		t.Fatalf("the delete recorded %d tombstones", got)
	}

	// The restore: the older backup becomes the candidate database and a
	// consistent snapshot of the live database is attached as restore_source,
	// exactly as the offline recovery coordinator prepares it.
	liveSnapshot := filepath.Join(directory, "live-snapshot.db")
	if _, err := database.ExecContext(ctx, "VACUUM INTO ?", liveSnapshot); err != nil {
		t.Fatal(err)
	}
	candidate, err := db.Open(ctx, backup)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { candidate.Close() })
	if err := db.AttachRecoverySource(ctx, candidate, liveSnapshot); err != nil {
		t.Fatal(err)
	}
	tx, err := candidate.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// The restore prepares the candidate transaction through the user module,
	// exactly as the offline recovery coordinator does.
	if err := (&Repository{}).PrepareRestoreTx(ctx, tx); err != nil {
		t.Fatalf("the older backup could not be prepared: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	restored := &Repository{Database: candidate}
	if got := countRows(t, restored, "SELECT count(*) FROM credential_tombstones WHERE credential_id=?", credential.ID); got != 1 {
		t.Fatalf("the restore kept %d tombstones of the deleted credential", got)
	}
	if got := countRows(t, restored, "SELECT count(*) FROM users WHERE id=?", account.ID); got != 0 {
		t.Fatalf("the restore resurrected the deleted user: %d", got)
	}
	batch := trafficBatch(account.ID, device.ID, credential.ID, time.Now().UTC().Add(-time.Minute))
	if err := restored.ApplyTrafficWithStates(ctx, batch, statesOf(revision)); err != nil {
		t.Fatalf("a counter of the credential deleted after the backup was rejected: %v", err)
	}
	userID, deviceID, upload, download := cursorOf(t, restored, credential.ID)
	if userID != account.ID || deviceID != device.ID || upload != 100 || download != 900 {
		t.Fatalf("restored identity cursor not advanced: %s %s %d %d", userID, deviceID, upload, download)
	}
}
