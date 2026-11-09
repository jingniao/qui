package backup

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/panel/certificate"
)

// TestCertificateArchiveKeepsSealedMaterial checks that the panel's own
// certificate store travels in the encrypted backup: the sealed material is
// archived unchanged, the archive validation opens it, and the inventory counts
// the rows.
func TestCertificateArchiveKeepsSealedMaterial(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	repository := &certificate.Repository{Database: source.Database, Vault: source.ConfigurationVault}
	item := certificate.Certificate{ID: "certificate-01", Domains: []string{"edge.example.com"}, Status: certificate.StatusPending, CreatedAt: stamp, UpdatedAt: stamp}
	if err := repository.Create(ctx, item); err != nil {
		t.Fatal(err)
	}
	material := certificate.Material{CertificatePEM: "-----BEGIN CERTIFICATE-----\nleaf\n-----END CERTIFICATE-----\n", PrivateKeyPEM: "-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----\n"}
	if err := repository.MarkIssuing(ctx, item.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveIssued(ctx, item.ID, material, stamp, stamp, time.Now()); err != nil {
		t.Fatal(err)
	}
	inventory, err := ReadInventory(ctx, source.Database)
	if err != nil || inventory.Totals.Certificates != 1 {
		t.Fatalf("inventory: %+v %v", inventory.Totals, err)
	}
	result, err := source.Create(ctx, directory, "certificate_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		var sealed []byte
		if err := database.QueryRow("SELECT encrypted_material FROM certificates WHERE id='certificate-01'").Scan(&sealed); err != nil {
			return err
		}
		plaintext, err := source.ConfigurationVault.Open(sealed)
		if err != nil {
			return err
		}
		var restored certificate.Material
		if err := json.Unmarshal(plaintext, &restored); err != nil {
			return err
		}
		if restored != material {
			t.Fatalf("archived material: %+v", restored)
		}
		archived, err := ReadInventory(ctx, database)
		if err != nil || archived.Totals.Certificates != 1 {
			t.Fatalf("archive inventory: %+v %v", archived.Totals, err)
		}
		return validateCertificates(ctx, database, source.ConfigurationVault)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCertificateValidationRejectsMalformedRows checks the archive checker against
// values the live schema or this panel could never write.
func TestCertificateValidationRejectsMalformedRows(t *testing.T) {
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name         string
		statement    string
		ignoreChecks bool
	}{
		{"unknown status", "INSERT INTO certificates(id,domains_json,status,created_at,updated_at) VALUES('bad','[\"a.example.com\"]','expired',?,?)", true},
		{"unknown error code", "INSERT INTO certificates(id,domains_json,status,last_error_code,created_at,updated_at) VALUES('bad','[\"a.example.com\"]','failed','boom',?,?)", false},
		{"eleven names", "INSERT INTO certificates(id,domains_json,status,created_at,updated_at) VALUES('bad','[\"a.example.com\",\"b.example.com\",\"c.example.com\",\"d.example.com\",\"e.example.com\",\"f.example.com\",\"g.example.com\",\"h.example.com\",\"i.example.com\",\"j.example.com\",\"k.example.com\"]','pending',?,?)", false},
		{"upper case name", "INSERT INTO certificates(id,domains_json,status,created_at,updated_at) VALUES('bad','[\"Edge.Example.com\"]','pending',?,?)", false},
		{"not json", "INSERT INTO certificates(id,domains_json,status,created_at,updated_at) VALUES('bad','edge.example.com','pending',?,?)", false},
		{"local timestamp", "INSERT INTO certificates(id,domains_json,status,created_at,updated_at) VALUES('bad','[\"a.example.com\"]','pending','2026-01-01T08:00:00+08:00',?)", false},
		{"material that does not open", "INSERT INTO certificates(id,domains_json,status,encrypted_material,created_at,updated_at) VALUES('bad','[\"a.example.com\"]','valid',x'01',?,?)", false},
	} {
		source, _, _ := sourceFixture(t)
		if tc.ignoreChecks {
			if _, err := source.Database.Exec("PRAGMA ignore_check_constraints=ON"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := source.Database.Exec(tc.statement, stamp, stamp); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if validateCertificates(ctx, source.Database, source.ConfigurationVault) == nil {
			t.Fatalf("%s was accepted", tc.name)
		}
	}
}

// TestCertificateValidationRejectsDanglingInboundReference refuses an archive
// whose inbound points at a certificate the archive does not hold.
func TestCertificateValidationRejectsDanglingInboundReference(t *testing.T) {
	ctx := context.Background()
	source, _, _ := sourceFixture(t)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	// The reference is a foreign key, so the damaged state is only reachable in an
	// archive written with enforcement off.
	if _, err := source.Database.Exec("PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at,certificate_id)
SELECT 'inbound-1','edge',id,'vless','0.0.0.0',443,1,1,0,'direct','[]',x'01',?,'certificate-missing' FROM nodes LIMIT 1`, stamp); err != nil {
		t.Fatal(err)
	}
	if validateCertificates(ctx, source.Database, source.ConfigurationVault) == nil {
		t.Fatal("a dangling certificate reference was accepted")
	}
}

// TestCertificateValidationAcceptsPre0137 keeps older archives restorable.
func TestCertificateValidationAcceptsPre0137(t *testing.T) {
	ctx := context.Background()
	source, _, _ := sourceFixture(t)
	rewindCertificates(t, source.Database)
	if err := validateCertificates(ctx, source.Database, source.ConfigurationVault); err != nil {
		t.Fatalf("a pre-0137 archive was rejected: %v", err)
	}
	// A table without its migration, or a migration without the column, is an
	// archive this build did not write.
	if _, err := source.Database.Exec("CREATE TABLE certificates(id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if validateCertificates(ctx, source.Database, source.ConfigurationVault) == nil {
		t.Fatal("a partial 0137 schema was accepted")
	}
}
