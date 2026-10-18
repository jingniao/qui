package deployment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/release"
)

func coreNodeFixture(t *testing.T, platform, coreName string) (*Repository, *system.Repository, string, Revision) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(root, "key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	n, token, _, err := nodes.CreateEnrollment(ctx, "Core task node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := nodes.ConsumeEnrollment(ctx, node.Hash(token), n.ID, "core-host", platform, "test-agent", "certificate", "fingerprint", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := nodes.Record(ctx, n.ID, &pb.AgentMessage{NodeId: n.ID, MessageId: "capability", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: coreName, CoreVersion: "26.3.27"}}}); err != nil {
		t.Fatal(err)
	}
	if err := nodes.CoreUpdateSupport(ctx, n.ID, true); err != nil {
		t.Fatal(err)
	}
	r := &Repository{Database: database, Vault: key}
	state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	revision, err := r.Create(ctx, state, "owner", "baseline")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Result(ctx, n.ID, &pb.ApplyResult{Revision: revision.ID, Success: true, Code: "success"}); err != nil {
		t.Fatal(err)
	}
	return r, &system.Repository{Database: database}, n.ID, revision
}

// coreAssetURLForTest is an official asset URL for the host platform.
func coreAssetURLForTest() string {
	asset := "Xray-linux-64.zip"
	if runtime.GOOS == "windows" {
		asset = "Xray-windows-64.zip"
	}
	if runtime.GOARCH == "arm64" {
		asset = strings.Replace(asset, "-64.", "-arm64-v8a.", 1)
	}
	return "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/" + asset
}

// TestCoreUpdateQueueValidation covers every rejection the Owner can trigger
// before a node is asked to do anything.
func TestCoreUpdateQueueValidation(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	assetURL := coreAssetURLForTest()
	platform := runtime.GOOS + "/" + runtime.GOARCH
	r, catalogue, nodeID, revision := coreNodeFixture(t, platform, "xray")
	ctx := context.Background()
	cases := []struct {
		name     string
		expected int64
		input    CoreUpdateInput
		accepted bool
	}{
		{"accepted", revision.ID, coreTarget("26.3.27", assetURL, digest), true},
		{"pre-release version", revision.ID, coreTarget("26.3.27-beta.1", assetURL, digest), false},
		{"unstable version", revision.ID, coreTarget("latest", assetURL, digest), false},
		{"short digest", revision.ID, coreTarget("26.3.27", assetURL, strings.Repeat("ab", 31)), false},
		{"uppercase digest", revision.ID, coreTarget("26.3.27", assetURL, strings.ToUpper(digest)), false},
		{"http url", revision.ID, coreTarget("26.3.27", strings.Replace(assetURL, "https", "http", 1), digest), false},
		{"foreign host", revision.ID, coreTarget("26.3.27", "https://example.com/Xray-linux-64.zip", digest), false},
		{"foreign project", revision.ID, coreTarget("26.3.27", "https://github.com/SagerNet/sing-box/releases/download/v1.14.3/sing-box-1.14.3-linux-amd64.tar.gz", digest), false},
		{"wrong asset platform", revision.ID, coreTarget("26.3.27", "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-freebsd-riscv64.zip", digest), false},
		{"wrong component", revision.ID, CoreUpdateInput{Component: "sing-box", Version: "26.3.27", URL: assetURL, SHA256: digest}, false},
		{"stale revision", revision.ID + 1, coreTarget("26.3.27", assetURL, digest), false},
		{"missing revision", 0, coreTarget("26.3.27", assetURL, digest), false},
	}
	for _, item := range cases {
		task, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", item.expected, item.input)
		if !item.accepted {
			if err == nil {
				t.Fatalf("%s: accepted", item.name)
			}
			continue
		}
		if err != nil || task.Sequence != 1 || task.Status != "queued" || !strings.HasPrefix(task.CommandID, "upd_") {
			t.Fatalf("%s: %+v %v", item.name, task, err)
		}
	}
	if _, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.27", assetURL, digest)); err == nil {
		t.Fatal("second active core update accepted")
	}
}

// TestCoreUpdateDispatchReconnectClosureAndClear checks the reserved command,
// the reconnect closure of an unconfirmed task and the Owner clear action.
func TestCoreUpdateDispatchReconnectClosureAndClear(t *testing.T) {
	digest := strings.Repeat("cd", 32)
	assetURL := strings.Replace(coreAssetURLForTest(), "v26.3.27", "v26.3.28", 1)
	platform := runtime.GOOS + "/" + runtime.GOARCH
	r, catalogue, nodeID, revision := coreNodeFixture(t, platform, "xray")
	ctx := context.Background()
	task, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.28", assetURL, digest))
	if err != nil {
		t.Fatal(err)
	}
	command, err := r.PendingCoreUpdate(ctx, nodeID)
	if err != nil || command == nil {
		t.Fatal(err)
	}
	request, err := release.DecodeCoreUpdateRequest(command.RequestJson)
	if err != nil {
		t.Fatal(err)
	}
	if request.ID != task.CommandID || request.Revision != revision.ID || request.Sequence != 1 || request.URL != assetURL || request.SHA256 != digest {
		t.Fatalf("unexpected command: %+v", request)
	}
	// The node reconnects still reporting the old core version.
	if err := r.CloseStaleCoreUpdates(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	items, _, err := r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || len(items) != 1 || items[0].Status != "failed_closed" {
		t.Fatalf("stale task not closed: %+v %v", items, err)
	}
	if _, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.28", assetURL, digest)); err != nil {
		t.Fatalf("closed task still blocks a new update: %v", err)
	}
	// The node reconnects reporting the requested version this time.
	if _, err := r.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", `{"coreName":"xray","coreVersion":"26.3.28"}`, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PendingCoreUpdate(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if err := r.CloseStaleCoreUpdates(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	items, _, err = r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || len(items) != 2 {
		t.Fatalf("task list after reconnect: %+v %v", items, err)
	}
	closed := map[string]bool{}
	for _, item := range items {
		closed[item.Status] = true
	}
	if !closed["success"] || !closed["failed_closed"] {
		t.Fatalf("unexpected task statuses: %+v", items)
	}
	cleared, err := r.ClearCoreUpdate(ctx, catalogue, task.ID, "owner", "local")
	if err != nil || cleared.Status != "cleared" {
		t.Fatalf("clear failed: %+v %v", cleared, err)
	}
	if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{ResultJson: []byte(`{}`)}); err == nil {
		t.Fatal("malformed result accepted")
	}
}

// TestCoreUpdateUndispatchableLegacyTaskIsClosed covers the row migration 0107
// backfills for a historical task: it keeps status 'queued' with an empty direct
// URL, so no schema-2 command can be built from it. The task must be closed
// terminally with a reason instead of failing the node's control stream on every
// reconnect, and the node's update slot must stay usable afterwards.
func TestCoreUpdateUndispatchableLegacyTaskIsClosed(t *testing.T) {
	r, catalogue, nodeID, revision := coreNodeFixture(t, runtime.GOOS+"/"+runtime.GOARCH, "xray")
	ctx := context.Background()
	// Exactly what 0107 writes for a historical task: placeholder version and
	// digest, empty URL, still queued.
	if _, err := r.Database.Exec("INSERT INTO core_update_tasks("+coreTaskColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)", "task-legacy", nodeID, revision.ID, "xray", "0.0.0", "", strings.Repeat("0", 64), 1, "queued", "upd_legacy", "owner", now(), now()); err != nil {
		t.Fatal(err)
	}
	command, err := r.PendingCoreUpdate(ctx, nodeID)
	if err != nil || command != nil {
		t.Fatalf("undispatchable task not closed quietly: %+v %v", command, err)
	}
	items, total, err := r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("task list: %+v %d %v", items, total, err)
	}
	legacy := items[0]
	if legacy.Status != "failed_closed" || legacy.Result == nil || legacy.Result.Status != "failed_closed" || legacy.Result.Code != legacyCoreUpdateCode || legacy.Result.NodeID != nodeID || legacy.Result.Revision != revision.ID {
		t.Fatalf("legacy task closed without a reason: %+v", legacy)
	}
	if again, err := r.PendingCoreUpdate(ctx, nodeID); err != nil || again != nil {
		t.Fatalf("closed task still dispatched: %+v %v", again, err)
	}
	queued, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.27", coreAssetURLForTest(), strings.Repeat("ab", 32)))
	if err != nil {
		t.Fatalf("closed task still blocks a new update: %v", err)
	}
	command, err = r.PendingCoreUpdate(ctx, nodeID)
	if err != nil || command == nil {
		t.Fatalf("valid task not dispatched: %+v %v", command, err)
	}
	request, err := release.DecodeCoreUpdateRequest(command.RequestJson)
	if err != nil || request.ID != queued.CommandID || request.Sequence != queued.Sequence || request.URL != coreAssetURLForTest() {
		t.Fatalf("unexpected command: %+v %v", request, err)
	}
}

func coreTarget(version, assetURL, digest string) CoreUpdateInput {
	return CoreUpdateInput{Component: "xray", Version: version, URL: assetURL, SHA256: digest}
}

// queueCoreForTest reserves a valid core update, used by tests that only need
// the node's active core-update slot to be taken.
func queueCoreForTest(r *Repository, audit ReleaseAccess, nodeID string, revisionID int64) (CoreUpdateTask, error) {
	return r.QueueCoreUpdate(context.Background(), audit, nodeID, "owner", "local", revisionID, coreTarget("26.3.27", coreAssetURLForTest(), strings.Repeat("ab", 32)))
}

// coreTaskFixture keeps the signed catalogue fixture the Agent self-update
// tests need, on top of a node with one confirmed revision.
func coreTaskFixture(t *testing.T) (*Repository, *system.Repository, *node.Repository, string, Revision, system.ReleaseEntry, map[string]release.TrustedKey) {
	t.Helper()
	r, catalogue, nodeID, revision := coreNodeFixture(t, runtime.GOOS+"/"+runtime.GOARCH, "xray")
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	signed, err := release.Sign(release.Manifest{SchemaVersion: 1, Component: "xray", Version: "26.3.27", OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: 12, Bytes: 123, SHA256: strings.Repeat("b", 64), IssuedAt: now, ExpiresAt: now.Add(time.Hour)}, private)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := catalogue.SaveRelease(context.Background(), signed, public, "owner", "local")
	if err != nil {
		t.Fatal(err)
	}
	return r, catalogue, &node.Repository{Database: r.Database}, nodeID, revision, entry, map[string]release.TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{"xray"}}}
}

type failingCoreAudit struct{ ReleaseAccess }

func (f failingCoreAudit) RecordTx(context.Context, *sql.Tx, string, string, string, string, string) error {
	return errors.New("audit storage fault")
}
