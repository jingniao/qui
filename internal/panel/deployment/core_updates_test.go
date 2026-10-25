package deployment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
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
// the reconnect closure of an unconfirmed task, the late outcome a reconnected
// node still delivers and the Owner clear action.
func TestCoreUpdateDispatchReconnectClosureAndClear(t *testing.T) {
	digest := strings.Repeat("cd", 32)
	assetURL := strings.Replace(coreAssetURLForTest(), "v26.3.27", "v26.3.28", 1)
	platform := runtime.GOOS + "/" + runtime.GOARCH
	r, catalogue, nodeID, revision := coreNodeFixture(t, platform, "xray")
	ctx := context.Background()
	// A reconnect stores the capabilities of the core that runs now, and only
	// then is a stale update closed.
	nodes := &node.Repository{Database: r.Database}
	sequence := uint64(1)
	reportCapabilities := func(version string) {
		t.Helper()
		sequence++
		message := &pb.AgentMessage{NodeId: nodeID, MessageId: "capabilities", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: version}}}
		if _, err := nodes.Record(ctx, nodeID, message); err != nil {
			t.Fatal(err)
		}
	}
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
	reportCapabilities("26.3.27")
	if err := r.CloseStaleCoreUpdates(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	items, _, err := r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || len(items) != 1 || items[0].Status != "failed_closed" || items[0].Result != nil {
		t.Fatalf("stale task not closed without an outcome: %+v %v", items, err)
	}
	// The outcome the Agent produced while the stream was down is still recorded.
	result, err := json.Marshal(release.UpdateResult{ID: task.CommandID, NodeID: nodeID, Revision: revision.ID, TargetDigest: digest, Status: "success", Code: "success", InstalledDigest: strings.Repeat("ef", 32), InstalledVersion: "26.3.28", InstalledSequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	late := &pb.CoreUpdateResult{ResultJson: result}
	if err := r.CoreUpdateResult(ctx, nodeID, late); err != nil {
		t.Fatalf("late success result rejected: %v", err)
	}
	items, _, err = r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || len(items) != 1 || items[0].Status != "success" || items[0].Result == nil || items[0].Result.InstalledVersion != "26.3.28" {
		t.Fatalf("late result not recorded: %+v %v", items, err)
	}
	if err := r.CoreUpdateResult(ctx, nodeID, late); err != nil {
		t.Fatalf("identical replay rejected: %v", err)
	}
	conflicting, err := json.Marshal(release.UpdateResult{ID: task.CommandID, NodeID: nodeID, Revision: revision.ID, TargetDigest: digest, Status: "rejected", Code: "checksum_mismatch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{ResultJson: conflicting}); err == nil {
		t.Fatal("a second, different outcome was accepted")
	}
	// The closed task frees the slot; this time the node reports the requested
	// version, so the reconnect closes the new task as a success.
	second, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.28", assetURL, digest))
	if err != nil {
		t.Fatalf("closed task still blocks a new update: %v", err)
	}
	if _, err := r.PendingCoreUpdate(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	reportCapabilities("26.3.28")
	if err := r.CloseStaleCoreUpdates(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	items, _, err = r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || len(items) != 2 || items[0].ID != second.ID || items[0].Status != "success" || items[1].Status != "success" {
		t.Fatalf("task list after reconnect: %+v %v", items, err)
	}
	// A running task can still be cleared, and a result that arrives afterwards
	// changes no row, so the panel must report it instead of acknowledging it.
	third, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.28", assetURL, digest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.PendingCoreUpdate(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	cleared, err := r.ClearCoreUpdate(ctx, catalogue, third.ID, "owner", "local")
	if err != nil || cleared.Status != "cleared" {
		t.Fatalf("clear failed: %+v %v", cleared, err)
	}
	unrecorded, err := json.Marshal(release.UpdateResult{ID: third.CommandID, NodeID: nodeID, Revision: revision.ID, TargetDigest: digest, Status: "rejected", Code: "checksum_mismatch"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{ResultJson: unrecorded}); err == nil {
		t.Fatal("result for a cleared task was acknowledged without being recorded")
	}
	if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{ResultJson: []byte(`{}`)}); err == nil {
		t.Fatal("malformed result accepted")
	}
}

// TestCoreUpdateClosedAsSuccessStillAcceptsTheReceipt is the reconnect case of
// 2026-10-25: the node comes back already running the requested core version, so
// CloseStaleCoreUpdates closes the running task as a success without a result.
// The authenticated receipt the Agent delivers next is the only proof of what
// happened on the node and used to affect no row, which dropped the control
// stream instead of recording it.
func TestCoreUpdateClosedAsSuccessStillAcceptsTheReceipt(t *testing.T) {
	digest := strings.Repeat("cd", 32)
	assetURL := strings.Replace(coreAssetURLForTest(), "v26.3.27", "v26.3.28", 1)
	r, catalogue, nodeID, revision := coreNodeFixture(t, runtime.GOOS+"/"+runtime.GOARCH, "xray")
	ctx := context.Background()
	nodes := &node.Repository{Database: r.Database}
	sequence := uint64(1)
	reportCapabilities := func(version string) {
		t.Helper()
		sequence++
		message := &pb.AgentMessage{NodeId: nodeID, MessageId: "capabilities", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: version}}}
		if _, err := nodes.Record(ctx, nodeID, message); err != nil {
			t.Fatal(err)
		}
	}
	task, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.28", assetURL, digest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.PendingCoreUpdate(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	reportCapabilities("26.3.28")
	if err := r.CloseStaleCoreUpdates(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	items, _, err := r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || len(items) != 1 || items[0].Status != "success" || items[0].Result != nil {
		t.Fatalf("the reconnect did not close the task as a result-less success: %+v %v", items, err)
	}

	receipt, err := json.Marshal(release.UpdateResult{ID: task.CommandID, NodeID: nodeID, Revision: revision.ID, TargetDigest: digest, Status: "success", Code: "success", InstalledDigest: strings.Repeat("ef", 32), InstalledVersion: "26.3.28", InstalledSequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{ResultJson: receipt}); err != nil {
		t.Fatalf("the receipt of a task closed as success was rejected: %v", err)
	}
	items, _, err = r.ListCoreUpdates(ctx, 1, 20)
	if err != nil || len(items) != 1 || items[0].Status != "success" || items[0].Result == nil || items[0].Result.InstalledVersion != "26.3.28" {
		t.Fatalf("the receipt was not recorded: %+v %v", items, err)
	}
	if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{ResultJson: receipt}); err != nil {
		t.Fatalf("identical replay rejected: %v", err)
	}

	// A task the Owner cleared stays closed: its receipt must not be
	// acknowledged, because no row would record it.
	cleared, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, coreTarget("26.3.28", assetURL, digest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.PendingCoreUpdate(ctx, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ClearCoreUpdate(ctx, catalogue, cleared.ID, "owner", "local"); err != nil {
		t.Fatal(err)
	}
	unrecorded, err := json.Marshal(release.UpdateResult{ID: cleared.CommandID, NodeID: nodeID, Revision: revision.ID, TargetDigest: digest, Status: "success", Code: "success", InstalledDigest: strings.Repeat("ef", 32), InstalledVersion: "26.3.28", InstalledSequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{ResultJson: unrecorded}); err == nil {
		t.Fatal("result for a cleared task was acknowledged without being recorded")
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
