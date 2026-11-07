package recovery

import (
	"context"
	"errors"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/pki"
	"testing"
	"time"
)

// Restoring a backup taken before a certificate recovery must not revive the
// certificate the recovery replaced: the retirement wins over the archived
// identity and the replacement keeps its identity.
func TestRestoringPreRecoveryBackupKeepsReplacement(t *testing.T) {
	t.Parallel()
	f := recoveryFixture(t)
	ctx := context.Background()
	database := f.source.Database
	var previous string
	if err := database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", f.node.ID).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	// The Owner recovered the node after this backup was taken: the Agent
	// generated a new key, the panel issued the certificate and the replaced
	// fingerprint was retired.
	authority, err := pki.LoadExisting(f.source.Config.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := identity.CSR(filepath.Join(t.TempDir(), "recovered", "identity"))
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(csrPEM)
	if err != nil {
		t.Fatal(err)
	}
	encoded, leaf, err := authority.IssueNode(csr, f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	replacement := pki.Fingerprint(leaf)
	if _, err := database.Exec("INSERT INTO node_identity_retirements(fingerprint,node_id,recovery_id,retired_at) VALUES(?,?,?,?)", previous, f.node.ID, "recovery-1", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE node_identities SET certificate_pem=?,fingerprint=?,expires_at=?,revoked_at=NULL WHERE node_id=?", string(encoded), replacement, leaf.NotAfter.UTC().Format(time.RFC3339Nano), f.node.ID); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	candidateDB, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer candidateDB.Close()
	assertText(t, candidateDB, "SELECT fingerprint FROM node_identities WHERE node_id=?", replacement, f.node.ID)
	assertInt(t, candidateDB, "SELECT count(*) FROM node_identity_retirements WHERE node_id=? AND fingerprint=?", 1, f.node.ID, previous)
	repository := &node.Repository{Database: candidateDB}
	if err := repository.ValidateIdentity(ctx, f.node.ID, replacement); err != nil {
		t.Fatalf("the replacement certificate lost its identity: %v", err)
	}
	if err := repository.ValidateIdentity(ctx, f.node.ID, previous); !errors.Is(err, node.ErrIdentity) {
		t.Fatalf("the restored backup revived the replaced certificate: %v", err)
	}
}
