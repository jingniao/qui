package recovery

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/pki"
	"testing"
	"time"
)

func TestRecoveryPreservesUnconfirmedCertificateRenewal(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	authority, err := pki.LoadExisting(f.source.Config.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM, err := identity.CSR(f.agentDirectory)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(csrPEM)
	if err != nil {
		t.Fatal(err)
	}
	_, leaf, err := authority.IssueNode(csr, f.node.ID)
	if err != nil {
		t.Fatal(err)
	}
	leaf.NotAfter = leaf.NotAfter.Add(24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, authority.Certificate, csr.PublicKey, authority.Key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	var previous string
	if err := f.source.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", f.node.ID).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	if _, err := f.nodes.StageRenewal(ctx, f.node.ID, previous, string(encoded), leaf); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	assertInt(t, database, "SELECT count(*) FROM node_certificate_renewals WHERE node_id=?", 1, f.node.ID)
	repository := &node.Repository{Database: database}
	if err := repository.ValidateIdentity(ctx, f.node.ID, pki.Fingerprint(leaf)); err != nil {
		t.Fatal("restored pending renewal cannot be activated", err)
	}
	assertInt(t, database, "SELECT count(*) FROM node_certificate_renewals WHERE node_id=?", 0, f.node.ID)
	if err := repository.ValidateIdentity(ctx, f.node.ID, previous); err == nil {
		t.Fatal("restoration reactivated replaced certificate")
	}
}
