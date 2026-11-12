package deployment

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// recoveryFixture is the smallest panel a redeploy needs: one node and the
// repository that owns its published revisions.
type recoveryFixture struct {
	repository *Repository
	nodes      *node.Repository
	database   *sql.DB
	nodeID     string
}

func newRecoveryFixture(t *testing.T) *recoveryFixture {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(directory, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	configurationVault, err := vault.Open(filepath.Join(directory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, _, _, err := nodes.CreateEnrollment(ctx, "Recovery", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &recoveryFixture{repository: &Repository{Database: database, Vault: configurationVault}, nodes: nodes, database: database, nodeID: n.ID}
}

// publish stores and applies the node's first revision with an inbound intent.
func (f *recoveryFixture) publish(t *testing.T, intent []string) Revision {
	t.Helper()
	ctx := context.Background()
	state := desiredstate.State{SchemaVersion: 1, NodeID: f.nodeID, Logging: desiredstate.Logging{DomainOnly: true}, Users: []desiredstate.User{{ID: "user-1", Enabled: true}}, Credentials: []desiredstate.Credential{{ID: "credential-1", UserID: "user-1", DeviceID: "device-1", Protocol: "socks5", Username: "user", Secret: "secret"}}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Enabled: true, Listen: "127.0.0.1", Port: 1080, DefaultOutbound: "direct", CredentialIDs: []string{"credential-1"}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	revision, err := f.repository.CreateWithInboundIntent(ctx, state, intent, "owner", "published")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repository.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.repository.Result(ctx, f.nodeID, &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	return revision
}

// recovery row is the node module's record of the consumed code.
func (f *recoveryFixture) recovery(t *testing.T) {
	t.Helper()
	if _, err := f.database.Exec("INSERT INTO node_identity_recoveries(id,node_id,token_hash,previous_fingerprint,owner,revoked_on_issue,status,redeploy_state,created_at,expires_at) VALUES('recovery-1',?,?,?,'owner',0,'consumed','none','now','later')", f.nodeID, strings.Repeat("e", 64), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}

// A recovery redeploy clones what the node runs: the same state, the same
// inbound intent and a new revision identity.
func TestRedeployPublishedClonesStateAndIntent(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	published := f.publish(t, []string{"socks"})
	clone, err := f.repository.RedeployPublished(ctx, f.nodeID, "recovery")
	if err != nil {
		t.Fatal(err)
	}
	if clone == published.ID {
		t.Fatal("the redeploy reused the published revision")
	}
	before, err := f.repository.State(ctx, published.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := f.repository.State(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	before.Revision, after.Revision = 0, 0
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("the clone differs from the published state:\n%+v\n%+v", before, after)
	}
	intent, err := f.repository.InboundIntent(ctx, clone)
	if err != nil || len(intent) != 1 || intent[0] != "socks" {
		t.Fatalf("inbound intent: %v %v", intent, err)
	}
	var summary string
	if err := f.database.QueryRow("SELECT summary FROM config_revisions WHERE id=?", clone).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if want := "身份恢复：重新下发 r" + strconv.FormatInt(published.ID, 10); summary != want {
		t.Fatalf("summary %q, want %q", summary, want)
	}
	var pending int
	if err := f.database.QueryRow("SELECT count(*) FROM deployment_targets WHERE revision=? AND node_id=? AND status='pending'", clone, f.nodeID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("the clone was not queued for the node: %d", pending)
	}
}

// A node that never ran a published revision has nothing to redeploy.
func TestRedeployPublishedReportsNothingPublished(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	if _, err := f.repository.RedeployPublished(ctx, f.nodeID, "recovery"); !errors.Is(err, ErrNothingPublished) {
		t.Fatalf("redeploy without a published revision: %v", err)
	}
	var revisions int
	if err := f.database.QueryRow("SELECT count(*) FROM config_revisions").Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatalf("the refused redeploy created %d revisions", revisions)
	}
}

// The node's apply result finishes the recovery redeploy record.
func TestResultMarksRecoveryRedeployOutcome(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	f.publish(t, []string{"socks"})
	f.recovery(t)
	for _, item := range []struct {
		name   string
		code   string
		state  string
		record string
	}{
		{"success", "success", "success", ""},
		{"failure", "core_failed", "failed", "core_failed"},
	} {
		t.Run(item.name, func(t *testing.T) {
			revision, err := f.repository.RedeployPublished(ctx, f.nodeID, "recovery")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.database.Exec("UPDATE node_identity_recoveries SET redeploy_state='queued',redeploy_revision=? WHERE node_id=?", revision, f.nodeID); err != nil {
				t.Fatal(err)
			}
			if err := f.repository.Result(ctx, f.nodeID, &pb.ApplyResult{Revision: revision, Success: item.state == "success", Code: item.code}); err != nil {
				t.Fatal(err)
			}
			var state, code string
			if err := f.database.QueryRow("SELECT redeploy_state,redeploy_code FROM node_identity_recoveries WHERE node_id=?", f.nodeID).Scan(&state, &code); err != nil {
				t.Fatal(err)
			}
			if state != item.state || code != item.record {
				t.Fatalf("recovery redeploy recorded %q %q", state, code)
			}
		})
	}
}
