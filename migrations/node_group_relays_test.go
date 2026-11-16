package migrations

import (
	"database/sql"
	"testing"
)

func TestNodeGroupRelaysMigrationKeepsRuleEndpoints(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		"PRAGMA foreign_keys=ON",
		"CREATE TABLE node_groups(id TEXT PRIMARY KEY)",
		"CREATE TABLE inbounds(id TEXT PRIMARY KEY)",
		"CREATE TABLE node_group_inbounds(group_id TEXT REFERENCES node_groups(id) ON DELETE CASCADE,inbound_id TEXT REFERENCES inbounds(id) ON DELETE CASCADE,PRIMARY KEY(group_id,inbound_id))",
		"CREATE TABLE tunnel_rules(id TEXT PRIMARY KEY,target_mode TEXT,target_inbound_id TEXT)",
		"INSERT INTO node_groups VALUES('g1'),('g2'),('g3')",
		"INSERT INTO inbounds VALUES('i1'),('i2')",
		"INSERT INTO node_group_inbounds VALUES('g1','i1'),('g2','i1'),('g3','i2')",
		"INSERT INTO tunnel_rules VALUES('linked','linked','i1'),('manual','manual',NULL),('unused','linked','i9')",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	applyWhole(t, db, "0151_node_group_relays.sql")
	rows, err := db.Query("SELECT group_id||'/'||rule_id FROM node_group_relays ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	rows.Close()
	if len(got) != 2 || got[0] != "g1/linked" || got[1] != "g2/linked" {
		t.Fatal(got)
	}
	var count int
	if _, err := db.Exec("DELETE FROM node_groups WHERE id='g1'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO node_group_relays VALUES('g3','manual')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM tunnel_rules WHERE id='linked'"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM node_group_relays").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := db.Exec("INSERT INTO node_group_relays VALUES('missing','manual')"); err == nil {
		t.Fatal("accepted a membership of a missing group")
	}
}
