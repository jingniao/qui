package migrations

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestNodeGroupEntriesMigration(t *testing.T) {
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, q := range []string{
		"PRAGMA foreign_keys=ON",
		"CREATE TABLE nodes(id TEXT PRIMARY KEY)",
		"CREATE TABLE inbounds(id TEXT PRIMARY KEY,node_id TEXT REFERENCES nodes(id),subscription_visible INTEGER,internal_relay INTEGER)",
		"CREATE TABLE users(id TEXT PRIMARY KEY)",
		"CREATE TABLE node_groups(id TEXT PRIMARY KEY,name TEXT UNIQUE,purpose TEXT,enabled INTEGER,subscription_visible INTEGER,remark TEXT DEFAULT '',created_at TEXT)",
		"CREATE TABLE node_group_members(group_id TEXT REFERENCES node_groups(id) ON DELETE CASCADE,node_id TEXT,PRIMARY KEY(group_id,node_id))",
		"CREATE TABLE user_node_group_grants(user_id TEXT,group_id TEXT REFERENCES node_groups(id) ON DELETE CASCADE,PRIMARY KEY(user_id,group_id))",
		"CREATE TABLE subscription_sources(id TEXT PRIMARY KEY,name TEXT,created_at TEXT)",
		"CREATE TABLE user_source_grants(user_id TEXT,source_id TEXT)",
		"CREATE TABLE node_meta(node_id TEXT PRIMARY KEY,remark TEXT)",
		"INSERT INTO nodes VALUES('a'),('b')",
		"INSERT INTO inbounds VALUES('entry','a',1,0),('hidden','a',0,0),('relay','a',1,1)",
		"INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('g','导入 · airport','subscription',1,1,'now')",
		"INSERT INTO node_group_members VALUES('g','a'),('g','b')",
		"INSERT INTO users VALUES('u')",
		"INSERT INTO subscription_sources VALUES('s','airport','now'),('t','airport','now')",
		"INSERT INTO user_source_grants VALUES('u','s'),('u','t')",
		"INSERT INTO node_meta VALUES('a','keep [ports:100-102]'),('b','invalid [ports:0-10]')",
	} {
		if _, err = d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := Files.ReadFile("0122_node_group_entries.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	check := func(q string, want int) {
		t.Helper()
		var n int
		if err := d.QueryRow(q).Scan(&n); err != nil || n != want {
			t.Fatalf("%s: %d want %d (%v)", q, n, want, err)
		}
	}
	check("SELECT count(*) FROM node_group_inbounds WHERE group_id='g' AND inbound_id='entry'", 1)
	check("SELECT count(*) FROM node_group_members", 1)
	check("SELECT count(*) FROM node_groups", 3)
	check("SELECT count(*) FROM node_group_imports WHERE proxy_key=''", 2)
	check("SELECT count(*) FROM user_node_group_grants", 2)
	check("SELECT count(*) FROM node_meta WHERE node_id='a' AND remark='keep' AND port_range_start=100 AND port_range_end=102", 1)
	check("SELECT count(*) FROM node_meta WHERE node_id='b' AND remark='invalid [ports:0-10]' AND port_range_start IS NULL", 1)
	for _, q := range []string{"UPDATE node_meta SET port_range_start=1 WHERE node_id='b'", "UPDATE node_meta SET port_range_start=103 WHERE node_id='a'"} {
		if _, err := d.Exec(q); err == nil {
			t.Fatal("invalid range accepted", q)
		}
	}
	if _, err = d.Exec("UPDATE inbounds SET node_id='b' WHERE id='entry'"); err != nil {
		t.Fatal(err)
	}
	check("SELECT count(*) FROM node_group_members WHERE node_id='b'", 1)
	if _, err = d.Exec("DELETE FROM inbounds WHERE id='entry'"); err != nil {
		t.Fatal(err)
	}
	check("SELECT count(*) FROM node_group_members", 0)
}
