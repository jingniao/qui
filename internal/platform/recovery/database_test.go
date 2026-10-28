package recovery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/portal"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/system"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/panelstore"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
	"time"
)

const recoveryPassword = "test-recovery-passphrase"

type fixture struct {
	owner          *panelstore.Ownership
	source         archive.Source
	prepared       archive.PreparedRestore
	users          *user.Repository
	nodes          *node.Repository
	deployments    *deployment.Repository
	access         *access.Service
	user           user.User
	device         user.Device
	node           node.Node
	state          desiredstate.State
	latest         traffic.Batch
	token          string
	agentDirectory string
}

func recoveryFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	c := config.DefaultPanel()
	// Recovery prepares real listeners before commit. Fixture ports must be
	// allocated by the OS so unrelated applications/reservations do not collide.
	c.HTTPAddress = "127.0.0.1:0"
	c.GRPCAddress = "127.0.0.1:0"
	c.Database = filepath.Join(root, "panel.db")
	c.NodeIdentityDirectory = filepath.Join(root, "identity")
	owner, err := panelstore.Try(c.Database, c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	c.Database, c.NodeIdentityDirectory = owner.Database, owner.IdentityDirectory
	database, err := db.Open(ctx, c.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(c.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	authority, err := pki.LoadOrCreate(c.NodeIdentityDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.DevelopmentServer(); err != nil {
		t.Fatal(err)
	}
	systems := &system.Repository{Database: database}
	admins := &auth.Repository{Database: database}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if err := admins.Initialize(ctx, auth.Admin{ID: "owner", Username: "owner", PasswordHash: "original-password-hash", CreatedAt: stamp}, func(ctx context.Context, tx *sql.Tx) error {
		return systems.SaveSetup(ctx, tx, "Original platform", "UTC")
	}); err != nil {
		t.Fatal(err)
	}
	if err := admins.CreateSession(ctx, "old-session", "owner", "local", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, token, _, err := nodes.CreateEnrollment(ctx, "Entry", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	agentDirectory := filepath.Join(t.TempDir(), "agent", "identity")
	csrPEM, err := identity.CSR(agentDirectory)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(csrPEM)
	if err != nil {
		t.Fatal(err)
	}
	encoded, certificate, err := authority.IssueNode(csr, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := nodes.ConsumeEnrollment(ctx, node.Hash(token), n.ID, "host", "Windows", "dev", string(encoded), pki.Fingerprint(certificate), certificate.NotAfter); err != nil {
		t.Fatal(err)
	}
	if err := identity.SaveEnrollment(agentDirectory, n.ID, encoded, authority.PEM); err != nil {
		t.Fatal(err)
	}
	group, err := nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{n.ID}})
	if err != nil {
		t.Fatal(err)
	}
	users := &user.Repository{Database: database, Vault: key}
	u, err := users.Save(ctx, "", user.Input{Username: "test-user", DisplayName: "Original name", Status: "active", TrafficLimit: 10000, MaxDevices: 4, MaxConnections: 8, AllowedProtocols: []string{"http"}, NodeGroupIDs: []string{group.ID}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "Laptop", Protocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	_, plaintext, err := (&subscription.Repository{Database: database}).Rotate(ctx, u.ID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	credential := device.Credentials[0]
	state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: u.ID, Enabled: true}}, Credentials: []desiredstate.Credential{{ID: credential.ID, UserID: u.ID, DeviceID: device.ID, Protocol: "http", Username: credential.Username, Secret: credential.Secret}}, Inbounds: []desiredstate.Inbound{{ID: "entry", Protocol: "http", Listen: "127.0.0.1", Port: 1080, Enabled: true, CredentialIDs: []string{credential.ID}, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	deployments := &deployment.Repository{Database: database, Vault: key}
	revision, err := deployments.Create(ctx, state, "owner", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if err := deployments.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := deployments.Result(ctx, n.ID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	state, err = deployments.State(ctx, revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := &access.Service{Repository: &access.Repository{Database: database}, Snapshots: deployments, Users: users}
	f := &fixture{owner: owner, source: archive.Source{Database: database, Config: c, ConfigurationVault: key}, users: users, nodes: nodes, deployments: deployments, access: service, user: u, device: device, node: n, state: state}
	f.token = plaintext
	f.agentDirectory = agentDirectory
	f.record(t, "before-backup", 100, 200)
	// A snapshot may contain its own file operation plus stale scheduling work.
	if _, err := database.Exec("INSERT INTO backup_file_ops(backup_id,operation,phase,metadata_json,created_at) VALUES('old-preparation','create','prepared','{}',?)", stamp); err != nil {
		t.Fatal(err)
	}
	created, err := f.source.Create(ctx, filepath.Join(root, "backups"), "snapshot_restore_001", "owner", recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM backup_file_ops"); err != nil {
		t.Fatal(err)
	}
	target := archive.RestoreTarget{Panel: c, ConfigurationPath: filepath.Join(root, "panel.yaml")}
	f.prepared, err = archive.PrepareRestore(ctx, filepath.Join(root, "stages"), filepath.Join(root, "backups", created.Manifest.ID+".pcb"), recoveryPassword, target)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) record(t *testing.T, id string, upload, download int64) {
	t.Helper()
	batch := traffic.Batch{ID: id, NodeID: f.node.ID, Counters: []traffic.Counter{{CoreEpoch: "epoch", Revision: f.state.Revision, CredentialID: f.device.Credentials[0].ID, UserID: f.user.ID, DeviceID: f.device.ID, Protocol: "http", UploadBytes: upload, DownloadBytes: download, ObservedAt: time.Now().UTC()}}}
	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.access.RecordTraffic(context.Background(), f.node.ID, &pb.TrafficBatch{BatchJson: data}, true); err != nil {
		t.Fatal(err)
	}
	f.latest = batch
}

func TestDatabaseRecoveryPreservesNewUsageReceiptsRevocationsAndDraftSnapshot(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	database := f.source.Database
	f.record(t, "after-backup", 300, 500)
	if _, err := database.Exec("UPDATE users SET display_name='New draft',status='disabled' WHERE id=?", f.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE nodes SET last_sequence=45,heartbeat_json='{}',status='online' WHERE id=?", f.node.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.nodes.Revoke(ctx, f.node.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.RotateCredential(ctx, f.device.ID, "http", true); err != nil {
		t.Fatal(err)
	}
	if err := (&auth.Repository{Database: database}).ChangePassword(ctx, "owner", "latest-password-hash"); err != nil {
		t.Fatal(err)
	}
	if err := (&auth.Repository{Database: database}).CreateSession(ctx, "new-session", "owner", "local", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO backup_runs(id,kind,period_key,schedule_revision,status,attempts,claim_token,lease_until,last_attempt_at) VALUES('current-run','daily','2026-10-05',1,'running',1,'private-claim',?,?)", time.Now().Add(time.Hour).Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var changesBefore int64
	if err := database.QueryRow("SELECT total_changes()").Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCandidate(ctx, candidate, f.prepared); err != nil {
		t.Fatal(err)
	}
	var changesAfter int64
	database.QueryRow("SELECT total_changes()").Scan(&changesAfter)
	if changesBefore != changesAfter {
		t.Fatal("recovery edited current database")
	}
	if err := archive.ValidateMaterialSet(ctx, candidate.Directory); err != nil {
		t.Fatal(err)
	}
	candidateDB, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer candidateDB.Close()
	assertInt(t, candidateDB, "SELECT used_traffic FROM users WHERE id=?", 800, f.user.ID)
	assertInt(t, candidateDB, "SELECT used_traffic FROM devices WHERE id=?", 800, f.device.ID)
	assertInt(t, candidateDB, "SELECT last_sequence FROM nodes WHERE id=?", 45, f.node.ID)
	assertInt(t, candidateDB, "SELECT count(*) FROM sessions", 0)
	assertInt(t, candidateDB, "SELECT count(*) FROM backup_file_ops", 0)
	assertInt(t, candidateDB, "SELECT count(*) FROM backup_runs WHERE status='running'", 0)
	assertText(t, candidateDB, "SELECT error_code FROM backup_runs WHERE id='current-run'", "restore_interrupted")
	assertText(t, candidateDB, "SELECT claim_token FROM backup_runs WHERE id='current-run'", "")
	assertText(t, candidateDB, "SELECT display_name FROM users WHERE id=?", "Original name", f.user.ID)
	assertText(t, candidateDB, "SELECT status FROM users WHERE id=?", "disabled", f.user.ID)
	assertText(t, candidateDB, "SELECT status FROM credentials WHERE id=?", "revoked", f.device.Credentials[0].ID)
	assertText(t, candidateDB, "SELECT password_hash FROM admins WHERE id='owner'", "latest-password-hash")
	assertInt(t, candidateDB, "SELECT count(*) FROM node_identities WHERE revoked_at IS NOT NULL", 1)
	assertText(t, candidateDB, "SELECT status FROM nodes WHERE id=?", "offline", f.node.ID)
	schedule, err := (&system.Repository{Database: candidateDB}).BackupSchedule(ctx)
	if err != nil || schedule.Policy.Enabled {
		t.Fatal("restored scheduler can run without review", err)
	}
	recoveredUsers := &user.Repository{Database: candidateDB, Vault: f.source.ConfigurationVault}
	recoveredDeployments := &deployment.Repository{Database: candidateDB, Vault: f.source.ConfigurationVault}
	service := &access.Service{Repository: &access.Repository{Database: candidateDB}, Snapshots: recoveredDeployments, Users: recoveredUsers}
	batchData, _ := json.Marshal(f.latest)
	if _, err := service.RecordTraffic(ctx, f.node.ID, &pb.TrafficBatch{BatchJson: batchData}, false); err != nil {
		t.Fatal("known traffic replay cannot be acknowledged", err)
	}
	assertInt(t, candidateDB, "SELECT used_traffic FROM users WHERE id=?", 800, f.user.ID)
	next := f.latest
	next.ID = "offline-next"
	next.Counters = append([]traffic.Counter(nil), next.Counters...)
	next.Counters[0].DownloadBytes += 100
	next.Counters[0].ObservedAt = time.Now().UTC()
	batchData, _ = json.Marshal(next)
	if _, err := service.RecordTraffic(ctx, f.node.ID, &pb.TrafficBatch{BatchJson: batchData}, true); err != nil {
		t.Fatal("unreported offline increment rejected", err)
	}
	assertInt(t, candidateDB, "SELECT used_traffic FROM users WHERE id=?", 900, f.user.ID)
}

// The real candidate coordinator must revoke backup sessions while preserving
// current authentication decisions and the encrypted copy of a rotated link.
func TestDatabaseRecoveryPreservesPortalLoginAndRecoverableSubscription(t *testing.T) {
	ctx := context.Background()
	var accounts []user.User
	var sharedToken subscription.Token
	var sharedPlaintext string
	f := listeningFixture(t, func(f *fixture) {
		logins := &portal.Repository{Database: f.source.Database}
		for i, enabled := range []bool{false, false, true, true} {
			account, err := f.users.Save(ctx, "", user.Input{Username: fmt.Sprintf("restore-login-%d", i), DisplayName: "Restore login", Status: "active", AllowedProtocols: []string{"http"}})
			if err != nil {
				t.Fatal(err)
			}
			accounts = append(accounts, account)
			if err := logins.SetLogin(ctx, account.ID, enabled, "backup-password-hash"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.source.Database.ExecContext(ctx, "UPDATE users SET last_login_at=? WHERE id=?", "2026-10-20T00:00:00.000000000Z", account.ID); err != nil {
				t.Fatal(err)
			}
		}
		if err := logins.CreateSession(ctx, "backup-portal-session", accounts[3].ID, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := logins.SessionUser(ctx, "backup-portal-session"); err != nil {
			t.Fatal("backup session must resolve before restore", err)
		}
		var err error
		identity, err := f.users.AccessIdentity(ctx, accounts[3].ID)
		if err != nil {
			t.Fatal(err)
		}
		sharedToken, sharedPlaintext, err = (&subscription.Repository{Database: f.source.Database, Vault: f.source.ConfigurationVault}).Rotate(ctx, accounts[3].ID, identity.ID)
		if err != nil {
			t.Fatal(err)
		}
		// The same hash exists on both sides, but only the current side retains
		// its encrypted copy, as with a pre-1.1 row upgraded before recovery.
		if _, err := f.source.Database.ExecContext(ctx, "UPDATE subscription_tokens SET encrypted_token=NULL WHERE id=?", sharedToken.ID); err != nil {
			t.Fatal(err)
		}
	})
	live := f.source.Database
	logins := &portal.Repository{Database: live}
	for i, account := range accounts {
		if err := logins.SetLogin(ctx, account.ID, i%2 == 1, "current-password-hash"); err != nil {
			t.Fatal(err)
		}
		stamp := "2026-10-21T00:00:00.000000000Z"
		if i%2 == 1 {
			stamp = "2026-10-19T00:00:00.000000000Z"
		}
		if _, err := live.ExecContext(ctx, "UPDATE users SET last_login_at=? WHERE id=?", stamp, account.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := logins.CreateSession(ctx, "current-portal-session", accounts[3].ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	newUser, err := f.users.Save(ctx, "", user.Input{Username: "after-backup-login", DisplayName: "New", Status: "active", AllowedProtocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := logins.SetLogin(ctx, newUser.ID, true, "new-user-password-hash"); err != nil {
		t.Fatal(err)
	}
	if err := logins.TouchLogin(ctx, newUser.ID); err != nil {
		t.Fatal(err)
	}
	newAccount, err := logins.Account(ctx, newUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := f.source.ConfigurationVault.Seal([]byte(sharedPlaintext))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.ExecContext(ctx, "UPDATE subscription_tokens SET encrypted_token=? WHERE id=?", sealed, sharedToken.ID); err != nil {
		t.Fatal(err)
	}
	_, rotated, err := (&subscription.Repository{Database: live, Vault: f.source.ConfigurationVault}).Rotate(ctx, f.user.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	portalRepo := &portal.Repository{Database: restored}
	for _, session := range []string{"backup-portal-session", "current-portal-session"} {
		if _, err := portalRepo.SessionUser(ctx, session); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("restored session resolved: %v", err)
		}
	}
	assertInt(t, restored, "SELECT count(*) FROM user_sessions", 0)
	for i, account := range accounts {
		assertText(t, restored, "SELECT password_hash FROM users WHERE id=?", "current-password-hash", account.ID)
		wantEnabled := int64(0)
		if i == 3 {
			wantEnabled = 1
		}
		assertInt(t, restored, "SELECT login_enabled FROM users WHERE id=?", wantEnabled, account.ID)
		stamp := "2026-10-21T00:00:00.000000000Z"
		if i%2 == 1 {
			stamp = "2026-10-20T00:00:00.000000000Z"
		}
		assertText(t, restored, "SELECT last_login_at FROM users WHERE id=?", stamp, account.ID)
	}
	assertText(t, restored, "SELECT password_hash FROM users WHERE id=?", newAccount.PasswordHash, newUser.ID)
	assertText(t, restored, "SELECT last_login_at FROM users WHERE id=?", newAccount.LastLoginAt, newUser.ID)
	assertInt(t, restored, "SELECT login_enabled FROM users WHERE id=?", 0, newUser.ID)
	assertText(t, restored, "SELECT status FROM users WHERE id=?", "disabled", newUser.ID)
	tokens := &subscription.Repository{Database: restored, Vault: f.source.ConfigurationVault}
	for _, item := range []struct{ userID, plaintext string }{{f.user.ID, rotated}, {accounts[3].ID, sharedPlaintext}} {
		if link, err := tokens.Link(ctx, item.userID); err != nil || link != item.plaintext {
			t.Fatalf("restored link unavailable or changed: %v", err)
		}
	}
	users := &user.Repository{Database: restored, Vault: f.source.ConfigurationVault}
	handler := &subscription.Handler{Service: &subscription.Service{Repository: tokens, Users: users,
		Sources: source.NewRegistry(&published.Source{Users: users, Nodes: &node.Repository{Database: restored}, Published: &deployment.Repository{Database: restored, Vault: f.source.ConfigurationVault}})},
		Auth: &auth.Handler{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	mux := http.NewServeMux()
	handler.Register(mux)
	for _, item := range []struct {
		token string
		code  int
	}{{rotated, http.StatusOK}, {f.token, http.StatusForbidden}} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", "/sub/"+item.token+"?format=uri", nil))
		if response.Code != item.code {
			t.Fatalf("restored subscription: got %d, want %d: %s", response.Code, item.code, response.Body.String())
		}
	}
}

func TestDatabaseRecoveryRefusesMissingOwnershipForeignMaterialsAndRegressions(t *testing.T) {
	for _, fault := range []string{"closed-owner", "foreign-key", "usage-regression", "sequence-regression", "cursor-regression", "pending-deployment", "current-file-journal", "ownerless", "copy-cancel"} {
		t.Run(fault, func(t *testing.T) {
			f := recoveryFixture(t)
			ctx := context.Background()
			operation := ctx
			database := f.source.Database
			switch fault {
			case "closed-owner":
				f.owner.Close()
			case "foreign-key":
				if err := os.WriteFile(filepath.Join(f.source.Config.NodeIdentityDirectory, "configuration.key"), bytes.Repeat([]byte{7}, 32), 0600); err != nil {
					t.Fatal(err)
				}
			case "usage-regression":
				_, err := database.Exec("UPDATE users SET used_traffic=0")
				if err != nil {
					t.Fatal(err)
				}
			case "sequence-regression":
				// The selected snapshot has a higher sequence than current evidence.
				mutatePreparedSnapshot(t, f, "UPDATE nodes SET last_sequence=10")
			case "cursor-regression":
				_, err := database.Exec("UPDATE user_traffic_cursors SET upload_bytes=0")
				if err != nil {
					t.Fatal(err)
				}
			case "pending-deployment":
				if _, err := database.Exec("UPDATE deployment_targets SET status='pending'"); err != nil {
					t.Fatal(err)
				}
			case "current-file-journal":
				if _, err := database.Exec("INSERT INTO backup_file_ops(backup_id,operation,phase,metadata_json,created_at) VALUES('unfinished','create','prepared','{}',?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
					t.Fatal(err)
				}
			case "ownerless":
				if _, err := database.Exec("UPDATE admins SET status='disabled'"); err != nil {
					t.Fatal(err)
				}
			case "copy-cancel":
				c, cancel := context.WithCancel(ctx)
				cancel()
				operation = c
			}
			candidate, err := PrepareDatabase(operation, f.owner, f.source, f.prepared, recoveryPassword)
			if err == nil || candidate.Directory != "" {
				t.Fatal("unsafe candidate prepared", fault, err)
			}
			if fault == "usage-regression" || fault == "cursor-regression" || fault == "sequence-regression" || fault == "ownerless" {
				if !errors.Is(err, db.ErrRecoveryConflict) {
					t.Fatal("expected explicit evidence conflict", err)
				}
			}
			stages, _ := filepath.Glob(filepath.Join(filepath.Dir(f.source.Config.Database), ".recovery-*"))
			if len(stages) != 0 {
				t.Fatal("failed recovery left candidate plaintext", stages)
			}
		})
	}
}

func mutatePreparedSnapshot(t *testing.T, f *fixture, query string) {
	t.Helper()
	// Make a new authentic archive from current state with this sequence, then
	// return the live sequence to zero. The stage is never patched directly.
	if _, err := f.source.Database.Exec(query); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	created, err := f.source.Create(context.Background(), root, "higher_snapshot_02", "owner", recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.prepared, err = archive.PrepareRestore(context.Background(), filepath.Join(t.TempDir(), "stages"), filepath.Join(root, created.Manifest.ID+".pcb"), recoveryPassword, f.prepared.Plan.Target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Database.Exec("UPDATE nodes SET last_sequence=0"); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseRecoveryRetainsLaterAllocationsAndCredentialRotation(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	database := f.source.Database
	f.record(t, "later-consumption", 200, 300)
	rotated, err := f.users.RotateCredential(ctx, f.device.ID, "http", false)
	if err != nil {
		t.Fatal(err)
	}
	_, newToken, err := (&subscription.Repository{Database: database}).Rotate(ctx, f.user.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	state := f.state
	state.Users = append([]desiredstate.User(nil), state.Users...)
	state.Credentials = []desiredstate.Credential{{ID: rotated.ID, UserID: f.user.ID, DeviceID: f.device.ID, Protocol: "http", Username: rotated.Username, Secret: rotated.Secret}}
	state.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	state.Inbounds[0].CredentialIDs = []string{rotated.ID}
	revision, err := f.deployments.Create(ctx, state, "owner", "rotated access")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Result(ctx, f.node.ID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	newNode, _, _, err := f.nodes.CreateEnrollment(ctx, "Later node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	newUser, err := f.users.Save(ctx, "", user.Input{Username: "later-user", DisplayName: "Later", Status: "active", TrafficLimit: 1000, MaxDevices: 2, MaxConnections: 4, AllowedProtocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	newDevice, err := f.users.CreateDevice(ctx, newUser.ID, user.DeviceInput{Name: "later-device", Protocols: []string{"http"}})
	if err != nil {
		t.Fatal(err)
	}
	// Represent a post-snapshot allocation context (node group granted).
	newGroup, err := f.nodes.SaveGroup(ctx, "", node.GroupInput{Name: "Later entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, NodeIDs: []string{newNode.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.users.Grant(ctx, newUser.ID, newGroup.ID, false); err != nil {
		t.Fatal(err)
	}
	// A deleted high revision must not make its ID reusable after restoration.
	deleted, err := f.deployments.Create(ctx, state, "owner", "deleted draft")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DELETE FROM config_revisions WHERE id=?", deleted.ID); err != nil {
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
	// budget allocations removed in Phase 4B.2
	assertText(t, candidateDB, "SELECT status FROM credentials WHERE id=?", "revoked", f.device.Credentials[0].ID)
	assertText(t, candidateDB, "SELECT status FROM credentials WHERE id=?", "active", rotated.ID)
	assertText(t, candidateDB, "SELECT status FROM users WHERE id=?", "disabled", newUser.ID)
	assertText(t, candidateDB, "SELECT status FROM devices WHERE id=?", "disabled", newDevice.ID)
	assertText(t, candidateDB, "SELECT status FROM credentials WHERE id=?", "revoked", newDevice.Credentials[0].ID)
	assertText(t, candidateDB, "SELECT status FROM nodes WHERE id=?", "disabled", newNode.ID)
	// budget allocations removed in Phase 4B.2
	assertInt(t, candidateDB, "SELECT count(*) FROM node_enrollment_tokens", 0)
	assertInt(t, candidateDB, "SELECT revision FROM node_runtime_state WHERE node_id=?", revision.ID, f.node.ID)
	subscriptions := &subscription.Repository{Database: candidateDB}
	if _, err := subscriptions.Resolve(ctx, f.token); !errors.Is(err, subscription.ErrToken) {
		t.Fatal("old subscription token reactivated", err)
	}
	if _, err := subscriptions.Resolve(ctx, newToken); err != nil {
		t.Fatal("current rotated subscription token lost", err)
	}
	deployments := &deployment.Repository{Database: candidateDB, Vault: f.source.ConfigurationVault}
	if _, err := deployments.AppliedState(ctx, f.node.ID, revision.ID); err != nil {
		t.Fatal("newer actual runtime revision lost", err)
	}
	next, err := deployments.Create(ctx, state, "owner", "after restoration")
	if err != nil || next.ID <= deleted.ID {
		t.Fatal("deleted revision number reused", next.ID, deleted.ID, err)
	}
}

func TestRecoveryFailureAfterCandidateWritesLeavesSourceAndStageUnchanged(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	// A trigger inherited from the snapshot aborts the final module. Earlier
	// module changes are rolled back and no candidate may be published.
	root := t.TempDir()
	if _, err := f.source.Database.Exec("CREATE TRIGGER fail_recovery_audit BEFORE INSERT ON admin_operations BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	created, err := f.source.Create(ctx, root, "fault_snapshot_001", "owner", recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := archive.PrepareRestore(ctx, filepath.Join(t.TempDir(), "stages"), filepath.Join(root, created.Manifest.ID+".pcb"), recoveryPassword, f.prepared.Plan.Target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.source.Database.Exec("DROP TRIGGER fail_recovery_audit"); err != nil {
		t.Fatal(err)
	}
	var before int64
	f.source.Database.QueryRow("SELECT total_changes()").Scan(&before)
	if candidate, err := PrepareDatabase(ctx, f.owner, f.source, prepared, recoveryPassword); err == nil || candidate.Directory != "" {
		t.Fatal("audit failure published candidate", err)
	}
	var after int64
	f.source.Database.QueryRow("SELECT total_changes()").Scan(&after)
	if after != before {
		t.Fatal("failed recovery altered current database")
	}
	if _, err := archive.VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256); err != nil {
		t.Fatal("failed recovery changed pinned stage", err)
	}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(f.source.Config.Database), ".recovery-*"))
	if len(paths) != 0 {
		t.Fatal("failed candidate materials retained", paths)
	}
	if _, err := f.source.Database.Exec("INSERT INTO system_settings(key,value_json) VALUES('after-failure','{}')"); err != nil {
		t.Fatal("failed preparation retained SQLite freeze", err)
	}
}

func TestCandidateVerificationRejectsEditedDatabaseManifestAndExtraFiles(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"manifest", "database", "evidence", "sidecar"} {
		t.Run(fault, func(t *testing.T) {
			path := ""
			switch fault {
			case "manifest":
				path = "candidate.json"
			case "database":
				path = "database/panel.db"
			case "evidence":
				path = "evidence/" + candidate.Manifest.EvidenceID + ".pcb"
			case "sidecar":
				path = "database/panel.db-wal"
			}
			full := filepath.Join(candidate.Directory, filepath.FromSlash(path))
			original, readErr := os.ReadFile(full)
			changed := append([]byte(nil), original...)
			if fault == "sidecar" {
				changed = []byte("stale")
			} else {
				if readErr != nil || len(changed) == 0 {
					t.Fatal(readErr)
				}
				changed[len(changed)/2] ^= 1
			}
			if err := os.WriteFile(full, changed, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyCandidate(ctx, candidate, f.prepared); err == nil {
				t.Fatal("changed recovery material accepted", fault)
			}
			if fault == "sidecar" {
				if err := os.Remove(full); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(full, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := VerifyCandidate(ctx, candidate, f.prepared); err != nil {
				t.Fatal("restored candidate no longer verifiable", err)
			}
		})
	}
}

func TestOldPendingSnapshotUsesCompletedCurrentDeploymentWithoutReplayingCommand(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	revision, err := f.deployments.Create(ctx, f.state, "owner", "pending when backed up")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	created, err := f.source.Create(ctx, root, "old_pending_0001", "owner", recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := archive.PrepareRestore(ctx, filepath.Join(t.TempDir(), "stages"), filepath.Join(root, created.Manifest.ID+".pcb"), recoveryPassword, f.prepared.Plan.Target)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Result(ctx, f.node.ID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Manifest.Snapshot.PendingOperations != 1 || candidate.Manifest.Candidate.PendingOperations != 0 {
		t.Fatal("old pending work did not reconcile with current completion")
	}
	database, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &deployment.Repository{Database: database, Vault: f.source.ConfigurationVault}
	if command, err := r.Pending(ctx, f.node.ID); err != nil || command != nil {
		t.Fatal("snapshot command replayed", command, err)
	}
	assertInt(t, database, "SELECT revision FROM node_runtime_state WHERE node_id=?", revision.ID, f.node.ID)
}

func TestRecoveryRejectsLostIssuedAllowanceRatherThanCreatingFreshUserBudget(t *testing.T) {
	f := recoveryFixture(t)
	ctx := context.Background()
	if err := f.users.Delete(ctx, f.user.ID); err != nil {
		t.Fatal(err)
	}
	if candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword); candidate.Directory != "" || !errors.Is(err, db.ErrRecoveryConflict) {
		t.Fatal("lost offline reservation treated as zero usage", err)
	}
	assertInt(t, f.source.Database, "SELECT count(*) FROM users WHERE id=?", 0, f.user.ID)
	if _, err := archive.VerifyPreparedRestore(ctx, f.prepared.Directory, f.prepared.ManifestSHA256); err != nil {
		t.Fatal("failed reconciliation edited original backup stage", err)
	}
}

func assertInt(t *testing.T, database *sql.DB, query string, want int64, args ...any) {
	t.Helper()
	var actual int64
	if err := database.QueryRow(query, args...).Scan(&actual); err != nil || actual != want {
		t.Fatal(query, actual, want, err)
	}
}
func assertText(t *testing.T, database *sql.DB, query, want string, args ...any) {
	t.Helper()
	var actual string
	if err := database.QueryRow(query, args...).Scan(&actual); err != nil || actual != want {
		t.Fatal(query, actual, want, err)
	}
}
