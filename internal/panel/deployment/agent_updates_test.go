package deployment

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

func agentTaskFixture(t *testing.T) (*Repository, *system.Repository, *node.Repository, string, Revision, system.ReleaseEntry, map[string]release.TrustedKey) {
	t.Helper()
	r, catalogue, nodes, nodeID, revision, core, _ := coreTaskFixture(t)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := core.Signed.Manifest
	manifest.Component = "proxy-agent"
	manifest.Version = "2.0.0"
	signed, err := release.Sign(manifest, private)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := catalogue.SaveRelease(context.Background(), signed, public, "owner", "local")
	if err != nil {
		t.Fatal(err)
	}
	if err := nodes.AgentUpdateSupport(context.Background(), nodeID, true); err != nil {
		t.Fatal(err)
	}
	return r, catalogue, nodes, nodeID, revision, entry, map[string]release.TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{"proxy-agent"}}}
}

func TestAgentUpdateReservationReplayAndResults(t *testing.T) {
	for _, status := range []string{"success", "rolled_back", "rejected", "failed_closed"} {
		t.Run(status, func(t *testing.T) {
			r, catalogue, _, nodeID, revision, entry, keys := agentTaskFixture(t)
			ctx := context.Background()
			task, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID); !errors.Is(err, ErrNodeBusy) {
				t.Fatal("duplicate reservation", err)
			}
			if _, err := queueCoreForTest(r, catalogue, nodeID, revision.ID); !errors.Is(err, ErrNodeBusy) {
				t.Fatal("core update bypassed reservation", err)
			}
			if _, err := r.RollbackNode(ctx, nodeID, revision.ID); !errors.Is(err, ErrNodeBusy) {
				t.Fatal("rollback bypassed reservation", err)
			}
			// Recreate the repository to ensure dispatch relies on persisted state.
			reopened := &Repository{Database: r.Database, Vault: r.Vault}
			if err := reopened.AgentUpdateResult(ctx, nodeID, nil); err == nil {
				t.Fatal("missing result accepted")
			}
			first, err := reopened.PendingAgentUpdate(ctx, nodeID)
			if err != nil || first == nil {
				t.Fatal(err)
			}
			second, err := reopened.PendingAgentUpdate(ctx, nodeID)
			if err != nil || second == nil || !bytes.Equal(first.RequestJson, second.RequestJson) {
				t.Fatal("dispatch changed on replay", err)
			}
			request, err := release.DecodeAgentUpdateRequest(first.RequestJson)
			if err != nil || request.ID != task.CommandID {
				t.Fatal(err)
			}
			result := release.UpdateResult{ID: request.ID, NodeID: nodeID, Revision: revision.ID, TargetDigest: entry.Signed.Manifest.SHA256, Status: status}
			switch status {
			case "success":
				result.Code = "success"
				result.InstalledDigest = result.TargetDigest
				result.InstalledVersion = entry.Signed.Manifest.Version
				result.InstalledSequence = entry.Signed.Manifest.Sequence
			case "rolled_back":
				result.Code = "rolled_back"
				result.InstalledDigest = strings.Repeat("a", 64)
				result.InstalledVersion = "1.0.0"
			case "rejected":
				result.Code = "trust_failed"
			case "failed_closed":
				result.Code = "installation_failed"
			}
			send := func(node string, value release.UpdateResult) error {
				data, _ := json.Marshal(value)
				return reopened.AgentUpdateResult(ctx, node, &pb.AgentUpdateResult{ResultJson: data})
			}
			if err := send("foreign-node", result); err == nil {
				t.Fatal("foreign result accepted")
			}
			wrong := result
			wrong.Revision++
			if err := send(nodeID, wrong); err == nil {
				t.Fatal("wrong revision accepted")
			}
			if status == "success" {
				wrong = result
				wrong.InstalledVersion = "9.0.0"
				if send(nodeID, wrong) == nil {
					t.Fatal("wrong installed version accepted")
				}
			}
			if err := send(nodeID, result); err != nil {
				t.Fatal(err)
			}
			if err := send(nodeID, result); err != nil {
				t.Fatal("repeated result rejected", err)
			}
			wrong = result
			wrong.Status = "rejected"
			wrong.Code = "preflight_failed"
			if err := send(nodeID, wrong); err == nil {
				t.Fatal("immutable result changed")
			}
			pending, err := reopened.PendingAgentUpdate(ctx, nodeID)
			if err != nil || pending != nil {
				t.Fatal("finished command dispatched again", err)
			}
			reserved, err := reopened.AgentUpdateReserved(ctx, nodeID)
			if err != nil || reserved != (status == "failed_closed") {
				t.Fatal("incorrect reservation", reserved, err)
			}
			items, total, err := reopened.ListAgentUpdates(ctx, 1, 20)
			if err != nil || total != 1 || len(items) != 1 || items[0].Status != status || items[0].Result == nil {
				t.Fatal(items, total, err)
			}
		})
	}
}

func TestAgentUpdateValidationAndAuditLeaveNoPartialTask(t *testing.T) {
	for _, mode := range []string{"audit", "legacy", "revision", "platform", "trust", "component", "core_busy"} {
		t.Run(mode, func(t *testing.T) {
			r, catalogue, nodes, nodeID, revision, entry, keys := agentTaskFixture(t)
			ctx := context.Background()
			var access ReleaseAccess = catalogue
			expected := revision.ID
			switch mode {
			case "audit":
				access = failingCoreAudit{catalogue}
			case "legacy":
				if err := nodes.AgentUpdateSupport(ctx, nodeID, false); err != nil {
					t.Fatal(err)
				}
			case "revision":
				expected++
			case "platform":
				if _, err := r.Database.Exec("UPDATE nodes SET operating_system='unsupported'"); err != nil {
					t.Fatal(err)
				}
			case "trust":
				keys = map[string]release.TrustedKey{}
			case "component":
				if err := r.Database.QueryRow("SELECT id FROM release_catalogue WHERE component='xray'").Scan(&entry.ID); err != nil {
					t.Fatal(err)
				}
			case "core_busy":
				if _, err := queueCoreForTest(r, catalogue, nodeID, revision.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.QueueAgentUpdate(ctx, access, keys, nodeID, entry.ID, "owner", "local", expected); err == nil {
				t.Fatal("invalid task accepted")
			}
			var tasks, attempts int
			if err := r.Database.QueryRow("SELECT count(*) FROM agent_update_tasks").Scan(&tasks); err != nil {
				t.Fatal(err)
			}
			if err := r.Database.QueryRow("SELECT count(*) FROM agent_update_attempts").Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if tasks != 0 || attempts != 0 {
				t.Fatal("partial task committed", tasks, attempts)
			}
		})
	}
}
