package recovery

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/testutil/dbtest"
)

// TestInboundTLSSourceSurvivesBackupAndRestore pins the source column of an
// inbound: it is ordinary panel metadata in the inbounds row, so an archive
// carries it and a restore brings it back without touching the sealed material.
func TestInboundTLSSourceSurvivesBackupAndRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const inboundID = "tls-source-inbound"
	f := listeningFixture(t, func(f *fixture) {
		settings := selfSignedSettings(t)
		sealed, err := f.source.ConfigurationVault.Seal(settings)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at,tls_source) VALUES(?,?,?,'vless','0.0.0.0',24443,0,0,0,'direct','[]',?,'2026-11-09T00:00:00Z','self_signed')", inboundID, "tlssource", f.node.ID, sealed); err != nil {
			t.Fatal(err)
		}
	})
	// The running panel then edits its own row; the archive must win.
	if _, err := f.source.Database.Exec("UPDATE inbounds SET tls_source='' WHERE id=?", inboundID); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertText(t, restored, "SELECT tls_source FROM inbounds WHERE id=?", "self_signed", inboundID)
}

// selfSignedSettings is a TLS inbound settings block with a real self-signed pair,
// so the archive fixture is the shape the panel writes for that source.
func selfSignedSettings(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "203.0.113.9"}, IPAddresses: []net.IP{net.ParseIP("203.0.113.9")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour)}
	encoded, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cert := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded}))
	settings, err := json.Marshal(map[string]any{"security": "tls", "server_name": "203.0.113.9", "certificate_pem": cert, "private_key_pem": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})), "trusted_ca_pem": cert})
	if err != nil {
		t.Fatal(err)
	}
	return settings
}
