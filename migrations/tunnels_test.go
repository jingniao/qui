package migrations

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestTunnelsSchemaConstraints(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	for _, q := range []string{
		"PRAGMA foreign_keys=ON",
		"CREATE TABLE nodes(id TEXT PRIMARY KEY,public_ip TEXT)",
		"CREATE TABLE inbounds(id TEXT PRIMARY KEY,node_id TEXT,port INTEGER)",
		"INSERT INTO nodes VALUES('a','1.1.1.1'),('b','2.2.2.2'),('c','3.3.3.3')",
		"INSERT INTO inbounds VALUES('i','b',443)",
	} {
		if _, err = database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	data, err := Files.ReadFile("0129_tunnels.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec(string(data)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO tunnels(id,name,transport,status,gost_material,created_at,updated_at) VALUES('t','Tunnel','tls','draft',x'01','now','now')",
		"INSERT INTO tunnel_hops VALUES('t','entry',0,'a',NULL,'auto','')",
		"INSERT INTO tunnel_hops VALUES('t','exit',1,'b',30000,'auto','')",
		"INSERT INTO port_claims VALUES('b',30000,'tunnel_hop','t')",
	} {
		if _, err = database.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	for _, q := range []string{
		"INSERT INTO tunnels(id,name,transport,status,gost_material,created_at,updated_at) VALUES('u','Tunnel','tls','draft',x'01','now','now')",
		"INSERT INTO tunnels(id,name,transport,status,gost_material,created_at,updated_at) VALUES('u','Other','udp','draft',x'01','now','now')",
		"INSERT INTO tunnel_hops VALUES('t','mid',2,'a',30001,'manual','')",
		"INSERT INTO tunnel_hops VALUES('t','entry',2,'c',30001,'auto','')",
		"INSERT INTO tunnel_hops VALUES('t','mid',2,'c',NULL,'auto','')",
		"INSERT INTO port_claims VALUES('b',30000,'rule','r')",
		"INSERT INTO port_claims VALUES('b',65536,'rule','r')",
		"INSERT INTO port_claims VALUES('missing',30000,'rule','r')",
	} {
		if _, err = database.Exec(q); err == nil {
			t.Fatalf("accepted %s", q)
		}
	}
	q := `INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,subscription_visible,enabled,status,created_at,updated_at,published_at) VALUES('r','rule','a',30001,'auto','both','t','linked','i',1,1,'published','now','old','then')`
	if _, err = database.Exec(q); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("UPDATE inbounds SET port=444 WHERE id='i'"); err != nil {
		t.Fatal(err)
	}
	var status, stamp string
	var version int
	if err = database.QueryRow("SELECT status,updated_at,draft_version FROM tunnel_rules WHERE id='r'").Scan(&status, &stamp, &version); err != nil || status != "needs_publish" || stamp == "old" || version != 2 {
		t.Fatal(status, stamp, version, err)
	}
	if _, err = database.Exec("DELETE FROM tunnels WHERE id='t'"); err == nil {
		t.Fatal("deleted referenced tunnel")
	}
}
