package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/migrations"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// beforeMigration is the migration set up to (not including) the named file.
type beforeMigration struct {
	fs.FS
	stop string
}

func (b beforeMigration) Glob(pattern string) ([]string, error) {
	names, err := fs.Glob(b.FS, pattern)
	if err != nil {
		return nil, err
	}
	kept := []string{}
	for _, name := range names {
		if name < b.stop {
			kept = append(kept, name)
		}
	}
	return kept, nil
}

func (b beforeMigration) ReadFile(name string) ([]byte, error) { return fs.ReadFile(b.FS, name) }

// A5.1, A5.3, A5.4: migration 0113 on a database with users of 0, 1 and 3
// devices, device-scoped policies, a WireGuard address and subscription tokens.
func TestUserLevelAccessMigration(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	uri, err := db.URI(filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err := db.Migrate(ctx, database, beforeMigration{FS: migrations.Files, stop: "0113_user_level_access.sql"}); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	stamp := func(minutes int) string {
		return time.Date(2026, 1, 1, 0, minutes, 0, 0, time.UTC).Format("2006-01-02T15:04:05.000000000Z")
	}
	for _, id := range []string{"u0", "u1", "u3"} {
		exec(`INSERT INTO users(id,username,display_name,status,max_devices,max_connections,protocols_json,created_at) VALUES(?,?,?,'active',5,8,'["socks5","wireguard"]',?)`, id, "user_"+id, id, stamp(0))
	}
	// u1 has one device; u3 has three: an older active one, a newer one that
	// holds the active subscription token, and a disabled one.
	exec(`INSERT INTO devices(id,user_id,name,status,created_at) VALUES('d1','u1','only','active',?)`, stamp(1))
	exec(`INSERT INTO devices(id,user_id,name,status,created_at) VALUES('d3a','u3','older','active',?)`, stamp(2))
	exec(`INSERT INTO devices(id,user_id,name,status,created_at) VALUES('d3b','u3','token','active',?)`, stamp(3))
	exec(`INSERT INTO devices(id,user_id,name,status,created_at) VALUES('d3c','u3','disabled','disabled',?)`, stamp(4))
	for _, credential := range [][3]string{{"c1", "u1", "d1"}, {"c3a", "u3", "d3a"}, {"c3b", "u3", "d3b"}, {"c3c", "u3", "d3c"}} {
		exec(`INSERT INTO credentials(id,user_id,device_id,protocol,username,encrypted_secret,status,created_at) VALUES(?,?,?,'socks5','name',x'00','active',?)`, credential[0], credential[1], credential[2], stamp(5))
	}
	exec(`INSERT INTO credentials(id,user_id,device_id,protocol,username,encrypted_secret,status,created_at) VALUES('c3a-wg','u3','d3a','wireguard','',x'00','active',?)`, stamp(5))
	// The address row needs an inbound only for its foreign key; the
	// migration's own cascade below runs with foreign keys on.
	exec("PRAGMA foreign_keys=OFF")
	exec(`INSERT INTO wireguard_addresses(inbound_id,device_id,address) VALUES('wg-inbound','d3a','10.77.0.2/32'),('wg-inbound','d3b','10.77.0.3/32')`)
	exec("PRAGMA foreign_keys=ON")
	exec(`INSERT INTO subscription_tokens(id,user_id,device_id,token_hash,created_at) VALUES('t3b','u3','d3b','hash-primary',?)`, stamp(6))
	exec(`INSERT INTO subscription_tokens(id,user_id,device_id,token_hash,created_at,revoked_at) VALUES('t3a','u3','d3a','hash-old',?,?)`, stamp(6), stamp(7))
	policy := func(id, scope string) {
		exec(`INSERT INTO policies(id,name,priority,enabled,scope_json,match_json,action_json,logging_json,timezone,remark,created_at,updated_at) VALUES(?,?,10,1,?,'{}','{"type":"Block"}','{}','UTC','',?,?)`, id, id, scope, stamp(8), stamp(8))
	}
	policy("p-devices", `{"device_ids":["d3a","d1"]}`)
	policy("p-mixed", `{"user_ids":["u0"],"device_ids":["d3c","d3b"]}`)
	policy("p-missing", `{"device_ids":["gone"]}`)
	policy("p-users", `{"user_ids":["u1"]}`)

	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatalf("migration 0113: %v", err)
	}

	count := func(query string, args ...any) int {
		t.Helper()
		var value int
		if err := database.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return value
	}
	devices := func(userID string) string {
		rows, err := database.QueryContext(ctx, "SELECT id FROM devices WHERE user_id=? ORDER BY id", userID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			rows.Scan(&id)
			ids = append(ids, id)
		}
		return strings.Join(ids, ",")
	}
	if got := devices("u0"); got != "" {
		t.Fatalf("u0 devices %q", got)
	}
	if got := devices("u1"); got != "d1" {
		t.Fatalf("u1 devices %q", got)
	}
	if got := devices("u3"); got != "d3b" {
		t.Fatalf("u3 kept %q, want the device with the active token", got)
	}
	if count(`SELECT count(*) FROM credentials WHERE device_id IN ('d3a','d3c')`) != 0 {
		t.Fatal("removed devices kept credentials")
	}
	if count(`SELECT count(*) FROM credential_tombstones WHERE credential_id IN ('c3a','c3a-wg','c3c') AND user_id='u3'`) != 3 {
		t.Fatal("removed credentials were not tombstoned")
	}
	if count(`SELECT count(*) FROM credential_tombstones WHERE credential_id IN ('c1','c3b')`) != 0 {
		t.Fatal("kept credentials were tombstoned")
	}
	if count(`SELECT count(*) FROM wireguard_addresses WHERE device_id='d3a'`) != 0 || count(`SELECT count(*) FROM wireguard_addresses WHERE device_id='d3b'`) != 1 {
		t.Fatal("WireGuard addresses were not cascaded with the removed device only")
	}
	if count(`SELECT count(*) FROM subscription_tokens WHERE id='t3b' AND revoked_at IS NULL`) != 1 || count(`SELECT count(*) FROM subscription_tokens WHERE device_id='d3a'`) != 0 {
		t.Fatal("A5.4: the primary token must survive, the removed device's tokens must go")
	}
	if count("SELECT count(*) FROM users WHERE max_devices<>1") != 0 {
		t.Fatal("max_devices not set to 1")
	}
	scope := func(id string) (map[string][]string, bool, string) {
		var raw, remark string
		var enabled bool
		if err := database.QueryRowContext(ctx, "SELECT scope_json,enabled,remark FROM policies WHERE id=?", id).Scan(&raw, &enabled, &remark); err != nil {
			t.Fatal(err)
		}
		value := map[string][]string{}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		return value, enabled, remark
	}
	if value, enabled, _ := scope("p-devices"); strings.Join(value["user_ids"], ",") != "u1,u3" || value["device_ids"] != nil || !enabled {
		t.Fatalf("p-devices: %v %t", value, enabled)
	}
	if value, _, _ := scope("p-mixed"); strings.Join(value["user_ids"], ",") != "u0,u3" || value["device_ids"] != nil {
		t.Fatalf("p-mixed: %v", value)
	}
	if value, enabled, remark := scope("p-missing"); enabled || value["device_ids"] != nil || !strings.Contains(remark, "disabled") {
		t.Fatalf("p-missing must be disabled with a remark: %v %t %q", value, enabled, remark)
	}
	if value, _, _ := scope("p-users"); strings.Join(value["user_ids"], ",") != "u1" {
		t.Fatalf("p-users changed: %v", value)
	}

	// Startup then gives every user an identity with one credential per
	// allowed protocol; u3 keeps its primary device.
	key, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{Database: database, Vault: key}
	changed, err := repository.EnsureAllAccessIdentities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 3 {
		t.Fatalf("identities changed for %v", changed)
	}
	for _, userID := range []string{"u0", "u1", "u3"} {
		if count("SELECT count(*) FROM devices WHERE user_id=?", userID) != 1 || count("SELECT count(*) FROM credentials c JOIN devices d ON d.id=c.device_id WHERE d.user_id=? AND c.status='active'", userID) != 2 {
			t.Fatalf("%s has no complete identity", userID)
		}
	}
	if devices("u3") != "d3b" {
		t.Fatal("startup replaced u3's primary device")
	}

	// A5.3: a node still running the revision with a removed device's
	// credential reports its traffic; it is charged to the user, not rejected.
	state := desiredstate.State{SchemaVersion: 1, Revision: 7, NodeID: "node-a", Credentials: []desiredstate.Credential{{ID: "c3a", UserID: "u3", DeviceID: "d3a", Protocol: "socks5", Secret: "unused"}}}
	batch := traffic.Batch{ID: "late-batch", NodeID: "node-a", Counters: []traffic.Counter{{CoreEpoch: "epoch", Revision: 7, CredentialID: "c3a", UserID: "u3", DeviceID: "d3a", Protocol: "socks5", UploadBytes: 40, DownloadBytes: 60, ObservedAt: time.Now().UTC().Add(-time.Minute)}}}
	if err := repository.ApplyTrafficWithStates(ctx, batch, map[int64]desiredstate.State{7: state}); err != nil {
		t.Fatalf("late traffic of a removed device was rejected: %v", err)
	}
	if used := count("SELECT used_traffic FROM users WHERE id='u3'"); used != 100 {
		t.Fatalf("u3 used %d, want 100", used)
	}
}
