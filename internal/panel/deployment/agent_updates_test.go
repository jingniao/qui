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
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/id"
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
			items, total, err := reopened.ListAllAgentUpdates(ctx, 1, 20)
			if err != nil || total != 1 || len(items) != 1 {
				t.Fatal(items, total, err)
			}
			listedData, err := json.Marshal(items[0])
			if err != nil {
				t.Fatal(err)
			}
			var listed AgentUpdateTask
			if err := json.Unmarshal(listedData, &listed); err != nil || listed.Status != status || listed.Result == nil {
				t.Fatal("invalid retained task history", listed, err)
			}
		})
	}
}

// PruneAgentReleases deletes older Agent releases nothing references, keeps the
// catalogue row a task points to, and reports the files to keep: the newest
// release plus any an unfinished task still needs.
func TestPruneAgentReleasesKeepsNewestAndReferenced(t *testing.T) {
	r, catalogue, _, nodeID, revision, old, keys := agentTaskFixture(t)
	ctx := context.Background()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	save := func(version string, sequence int64) system.ReleaseEntry {
		t.Helper()
		manifest := old.Signed.Manifest
		manifest.Version, manifest.Sequence = version, sequence
		signed, err := release.Sign(manifest, private)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := catalogue.SaveRelease(ctx, signed, public, "owner", "local")
		if err != nil {
			t.Fatal(err)
		}
		return entry
	}
	unused := save("2.0.1", old.Signed.Manifest.Sequence+1)
	if _, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, old.ID, "owner", "local", revision.ID); err != nil {
		t.Fatal(err)
	}
	newest := save("2.0.2", old.Signed.Manifest.Sequence+2)
	keep, err := catalogue.PruneAgentReleases(ctx)
	if err != nil || len(keep) != 2 {
		t.Fatalf("keep %+v %v", keep, err)
	}
	for _, m := range keep {
		if m != old.Signed.Manifest && m != newest.Signed.Manifest {
			t.Fatalf("kept %+v", m)
		}
	}
	rows := func() map[string]bool {
		got := map[string]bool{}
		list, _, err := catalogue.ListReleases(ctx, 1, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range list {
			got[entry.ID] = true
		}
		return got
	}
	if got := rows(); got[unused.ID] || !got[old.ID] || !got[newest.ID] {
		t.Fatalf("catalogue rows %v", got)
	}
	// A finished task keeps its catalogue row but no longer needs the files.
	if _, err := r.Database.Exec("UPDATE agent_update_tasks SET status='success'"); err != nil {
		t.Fatal(err)
	}
	keep, err = catalogue.PruneAgentReleases(ctx)
	if err != nil || len(keep) != 1 || keep[0] != newest.Signed.Manifest {
		t.Fatalf("keep after finish %+v %v", keep, err)
	}
	if got := rows(); !got[old.ID] || !got[newest.ID] {
		t.Fatalf("referenced row removed %v", got)
	}
}

func (r *Repository) QueueAgentUpdate(ctx context.Context, catalogue ReleaseAccess, keys map[string]release.TrustedKey, nodeID, entryID, actor, ip string, expected int64) (AgentUpdateTask, error) {
	if expected < 1 || actor == "" || len(actor) > 128 {
		return AgentUpdateTask{}, release.ErrUpdate
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return AgentUpdateTask{}, err
	}
	defer tx.Rollback()
	if err := nodeAvailable(ctx, tx, nodeID, ""); err != nil {
		return AgentUpdateTask{}, err
	}
	var enabled, supported bool
	var platform string

	var revision int64
	err = tx.QueryRowContext(ctx, "SELECT n.enabled,n.operating_system,coalesce(c.agent_supported,0),s.revision FROM nodes n LEFT JOIN node_update_capabilities c ON c.node_id=n.id JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=?", nodeID).Scan(&enabled, &platform, &supported, &revision)
	if err != nil {
		return AgentUpdateTask{}, err
	}
	if !enabled || revision != expected {
		return AgentUpdateTask{}, ErrNodeBusy
	}
	if !supported {
		return AgentUpdateTask{}, ErrAgentUpdateUnsupported
	}
	entry, err := catalogue.ReleaseTx(ctx, tx, entryID)
	if err != nil {
		return AgentUpdateTask{}, err
	}
	m := entry.Signed.Manifest
	if m.Component != "proxy-agent" || platform != m.OS+"/"+m.Architecture {
		return AgentUpdateTask{}, release.ErrPolicy
	}
	if err := entry.Signed.Verify(release.Policy{Component: m.Component, OS: m.OS, Architecture: m.Architecture, Now: time.Now().UTC(), Keys: keys}); err != nil {
		return AgentUpdateTask{}, err
	}
	taskID, err := id.New()
	if err != nil {
		return AgentUpdateTask{}, err
	}
	commandID, err := id.New()
	if err != nil {
		return AgentUpdateTask{}, err
	}
	commandID = "aup_" + commandID
	t := AgentUpdateTask{ID: taskID, NodeID: nodeID, ReleaseID: entryID, Revision: expected, Status: "queued", Generation: 1, CommandID: commandID, OriginalID: commandID, CreatedBy: actor, CreatedAt: now(), UpdatedAt: now()}
	request := release.AgentUpdateRequest{SchemaVersion: 1, ID: commandID, NodeID: nodeID, Revision: expected, Operation: "install", Release: entry.Signed}
	data, _ := json.Marshal(request)
	if _, err := tx.ExecContext(ctx, "INSERT INTO agent_update_tasks("+agentTaskColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL)", t.ID, t.NodeID, t.ReleaseID, t.Revision, t.Status, t.Generation, t.CommandID, t.OriginalID, t.CreatedBy, t.CreatedAt, t.UpdatedAt); err != nil {
		return t, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO agent_update_attempts(command_id,task_id,generation,operation,request_json,created_at) VALUES(?,?,1,'install',?,?)", commandID, taskID, data, t.CreatedAt); err != nil {
		return t, err
	}
	if err := catalogue.RecordTx(ctx, tx, actor, "queue_agent_update", taskID, "成功", ip); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

var ErrAgentUpdateUnsupported = errors.New("agent web upgrade is not enabled")
