package migrations_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/internal/platform/db"
)

// TestCertificatesMigration checks 0137 on the schema the panel actually runs:
// the certificate store, its value rules, and the inbound reference phase D2
// uses for inbounds.certificate_id.
func TestCertificatesMigration(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO nodes(id,name,status,enabled,hostname,operating_system,agent_version,public_ip,last_sequence,created_at)
VALUES('node-1','edge','online',1,'edge','linux','1.4.6','203.0.113.7',1,?)`, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO certificates(id,domains_json,status,auto_dns,dns_node_id,created_at,updated_at)
VALUES('certificate-1','["edge.example.com","*.edge.example.com"]','pending',1,'node-1',?,?)`, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	var domains, status string
	var attempts int
	if err := database.QueryRow("SELECT domains_json,status,attempts FROM certificates WHERE id='certificate-1'").Scan(&domains, &status, &attempts); err != nil {
		t.Fatal(err)
	}
	if domains != `["edge.example.com","*.edge.example.com"]` || status != "pending" || attempts != 0 {
		t.Fatalf("stored row: %q %q %d", domains, status, attempts)
	}
	// The status, the auto_dns flag and the attempt counter are constrained.
	for _, statement := range []string{
		"INSERT INTO certificates(id,domains_json,status,created_at,updated_at) VALUES('bad-status','[\"a.example.com\"]','unknown',?,?)",
		"INSERT INTO certificates(id,domains_json,status,auto_dns,created_at,updated_at) VALUES('bad-dns','[\"a.example.com\"]','pending',2,?,?)",
		"INSERT INTO certificates(id,domains_json,status,attempts,created_at,updated_at) VALUES('bad-attempts','[\"a.example.com\"]','pending',-1,?,?)",
	} {
		if _, err := database.Exec(statement, stamp, stamp); err == nil {
			t.Fatalf("accepted %s", statement)
		}
	}
	// An inbound without a certificate keeps a NULL reference.
	if _, err := database.Exec(`INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at)
VALUES('inbound-1','legacy','node-1','vless','0.0.0.0',443,1,1,0,'direct','[]',x'01',?)`, stamp); err != nil {
		t.Fatal(err)
	}
	var reference any
	if err := database.QueryRow("SELECT certificate_id FROM inbounds WHERE id='inbound-1'").Scan(&reference); err != nil || reference != nil {
		t.Fatalf("default reference: %v %v", reference, err)
	}
	// The reference is a foreign key: it can neither point at a missing row nor
	// let the certificate disappear while an inbound still uses it.
	if _, err := database.Exec("UPDATE inbounds SET certificate_id='missing' WHERE id='inbound-1'"); err == nil {
		t.Fatal("an inbound referenced a certificate that does not exist")
	}
	if _, err := database.Exec("UPDATE inbounds SET certificate_id='certificate-1' WHERE id='inbound-1'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM certificates WHERE id='certificate-1'"); err == nil {
		t.Fatal("a referenced certificate was deleted")
	}
	if _, err := database.Exec("UPDATE inbounds SET certificate_id=NULL WHERE id='inbound-1'"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM certificates WHERE id='certificate-1'"); err != nil {
		t.Fatalf("an unreferenced certificate could not be deleted: %v", err)
	}
}
