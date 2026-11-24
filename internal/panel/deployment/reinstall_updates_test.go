package deployment

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/agentidentity"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

func TestReinstallSupersedesUnfinishedUpdatesAndRestoresConfiguration(t *testing.T) {
	for _, scenario := range []struct{ kind, status string }{
		{"system", "queued"}, {"system", "running"}, {"system", "unconfirmed"}, {"system", "failed"},
		{"worker", "queued"}, {"worker", "running"}, {"worker", "failed_closed"},
		{"worker", "recover"},
		{"core", "queued"}, {"core", "running"}, {"core", "failed_closed"},
	} {
		t.Run(scenario.kind+"/"+scenario.status, func(t *testing.T) {
			kind := scenario.kind
			r, catalogue, nodes, nodeID, revision, entry, keys := agentTaskFixture(t)
			nodes.RecoveryUpdates = r
			ctx := context.Background()
			var taskID, table string
			switch kind {
			case "system":
				q := release.AgentSystemUpdateRequest{SchemaVersion: 1, Operation: "upgrade", TaskID: "system-task", AttemptID: "system-attempt", NodeID: nodeID, Revision: revision.ID, TargetVersion: "2.0.0"}
				data, err := json.Marshal(q)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.Database.Exec("INSERT INTO agent_system_update_tasks VALUES('system-task',?,?,'2.0.0','queued',1,'system-attempt','owner',?,?,'agent_install',NULL,'')", nodeID, revision.ID, now(), now()); err != nil {
					t.Fatal(err)
				}
				if _, err := r.Database.Exec("INSERT INTO agent_system_update_attempts(attempt_id,task_id,generation,request_json,created_at) VALUES('system-attempt','system-task',1,?,?)", data, now()); err != nil {
					t.Fatal(err)
				}
				if scenario.status == "failed" {
					failed := false
					result := release.AgentSystemUpdateResult{SchemaVersion: 1, TaskID: q.TaskID, AttemptID: q.AttemptID, NodeID: nodeID, Revision: revision.ID, TargetVersion: q.TargetVersion, Status: "failed", Phase: "agent_install", Error: "original installation error", StartedAt: now(), FinishedAt: now(), Recovery: release.AgentSystemUpdateRecovery{Attempted: true, Success: &failed}}
					data, err = json.Marshal(result)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := r.AgentSystemUpdateEvent(ctx, nodeID, &pb.AgentSystemUpdateEvent{ResultJson: data}); err != nil {
						t.Fatal(err)
					}
				}
				taskID, table = "system-task", "agent_system_update_tasks"
			case "worker":
				task, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID)
				if err != nil {
					t.Fatal(err)
				}
				taskID, table = task.ID, "agent_update_tasks"
				if scenario.status == "recover" {
					if err := nodes.AgentUpdateSupport(ctx, nodeID, true, true); err != nil {
						t.Fatal(err)
					}
					if _, err := r.PendingAgentUpdate(ctx, nodeID); err != nil {
						t.Fatal(err)
					}
					if _, err := r.RetryAgentRecovery(ctx, catalogue, task.ID, "owner", "local", AgentRecoveryConfirmation{NodeID: nodeID, Revision: revision.ID, CommandID: task.CommandID, Generation: 1, Unconfirmed: true}); err != nil {
						t.Fatal(err)
					}
				}
			case "core":
				task, err := queueCoreForTest(r, catalogue, nodeID, revision.ID)
				if err != nil {
					t.Fatal(err)
				}
				taskID, table = task.ID, "core_update_tasks"
			}
			statusBefore := scenario.status
			if statusBefore == "recover" {
				statusBefore = "queued"
			}
			if _, err := r.Database.Exec("UPDATE "+table+" SET status=? WHERE id=?", statusBefore, taskID); err != nil {
				t.Fatal(err)
			}
			var originalResult []byte
			if err := r.Database.QueryRow("SELECT result_json FROM "+table+" WHERE id=?", taskID).Scan(&originalResult); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Database.Exec("UPDATE node_identities SET fingerprint=?,expires_at='2999-01-01T00:00:00Z' WHERE node_id=?", strings.Repeat("a", 64), nodeID); err != nil {
				t.Fatal(err)
			}
			_, token, _, err := nodes.IssueRecovery(ctx, nodeID, "owner", time.Hour, false)
			if err != nil {
				t.Fatal(err)
			}
			var before string
			if err := r.Database.QueryRow("SELECT status FROM "+table+" WHERE id=?", taskID).Scan(&before); err != nil || before != statusBefore {
				t.Fatalf("issuing a code changed the old task: %q %v", before, err)
			}
			authority, err := pki.LoadOrCreate(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			control := &node.Control{Repository: nodes, Authority: authority}
			request := reinstallEnrollmentRequest(t, token)
			peerContext := peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{}})
			if _, err := control.Enroll(peerContext, request); err != nil {
				t.Fatal(err)
			}
			var status string
			if err := r.Database.QueryRow("SELECT status FROM "+table+" WHERE id=?", taskID).Scan(&status); err != nil || status != "superseded" {
				t.Fatalf("old update still occupies replacement: status=%q error=%v", status, err)
			}
			var preserved []byte
			if err := r.Database.QueryRow("SELECT result_json FROM "+table+" WHERE id=?", taskID).Scan(&preserved); err != nil || !bytes.Equal(originalResult, preserved) {
				t.Fatalf("execution result changed: %s %v", preserved, err)
			}
			if kind == "system" {
				attempts, total, err := r.ListAgentSystemAttempts(ctx, taskID, 1, 20)
				if err != nil || total != 1 || len(attempts) != 1 || attempts[0].Status != "superseded" {
					t.Fatalf("lost system attempt: %+v %d %v", attempts, total, err)
				}
				progress, err := json.Marshal(release.AgentSystemUpdateProgress{AttemptID: "system-attempt", Sequence: 1, Phase: "late", ObservedAt: now()})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.AgentSystemUpdateEvent(ctx, nodeID, &pb.AgentSystemUpdateEvent{ProgressJson: progress}); err == nil {
					t.Fatal("replacement accepted old system event")
				}
				if _, err := r.RetryAgentSystemUpdate(ctx, catalogue, taskID, "owner", "local", AgentRecoveryConfirmation{}); err == nil {
					t.Fatal("superseded system task retried")
				}
			}
			if kind == "worker" {
				attempts, total, err := r.ListAgentUpdateAttempts(ctx, taskID, 1, 20)
				wantAttempts := 1
				if scenario.status == "recover" {
					wantAttempts = 2
				}
				if err != nil || total != wantAttempts || len(attempts) != wantAttempts || attempts[0].Status != "superseded" {
					t.Fatalf("lost worker attempt: %+v %d %v", attempts, total, err)
				}
				for _, attempt := range attempts {
					data, err := json.Marshal(release.UpdateResult{ID: attempt.CommandID, NodeID: nodeID, Revision: revision.ID, TargetDigest: entry.Signed.Manifest.SHA256, Status: "failed_closed", Code: "installation_failed"})
					if err != nil {
						t.Fatal(err)
					}
					if err := r.AgentUpdateResult(ctx, nodeID, &pb.AgentUpdateResult{ResultJson: data}); err == nil {
						t.Fatal("replacement accepted old worker result")
					}
				}
				for _, unconfirmed := range []bool{true, false} {
					if _, err := r.RetryAgentRecovery(ctx, catalogue, taskID, "owner", "local", AgentRecoveryConfirmation{NodeID: nodeID, Revision: revision.ID, CommandID: attempts[0].CommandID, Generation: attempts[0].Generation, Unconfirmed: unconfirmed}); err == nil {
						t.Fatal("superseded worker recovery retried")
					}
				}
			}
			if kind == "core" {
				var command, digest, component string
				if err := r.Database.QueryRow("SELECT command_id,sha256,component FROM core_update_tasks WHERE id=?", taskID).Scan(&command, &digest, &component); err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(release.UpdateResult{ID: command, NodeID: nodeID, Revision: revision.ID, TargetDigest: digest, Status: "failed_closed", Code: "history_invalid"})
				if err != nil {
					t.Fatal(err)
				}
				if err := r.CoreUpdateResult(ctx, nodeID, &pb.CoreUpdateResult{Component: component, ResultJson: data}); err == nil {
					t.Fatal("replacement accepted old core result")
				}
			}
			items, total, err := r.TaskHistory(ctx, "all", "superseded", "已由重装接替", 1, 20)
			if err != nil || total != 1 || len(items) != 1 || items[0].ID != taskID {
				t.Fatalf("superseded history missing: %+v %d %v", items, total, err)
			}
			if reserved, err := r.AgentUpdateReserved(ctx, nodeID); err != nil || reserved {
				t.Fatalf("replacement reserved by old Agent: %v %v", reserved, err)
			}
			if command, err := r.PendingAgentSystemUpdate(ctx, nodeID); err != nil || command != nil {
				t.Fatalf("old system command resent: %v %v", command, err)
			}
			if command, err := r.PendingAgentUpdate(ctx, nodeID); err != nil || command != nil {
				t.Fatalf("old worker command resent: %v %v", command, err)
			}
			if command, err := r.PendingCoreUpdate(ctx, nodeID); err != nil || command != nil {
				t.Fatalf("old core command resent: %v %v", command, err)
			}
			if _, err := r.RedeployPublished(ctx, nodeID, "recovery"); err != nil {
				t.Fatalf("replacement configuration blocked: %v", err)
			}
		})
	}
}

func reinstallEnrollmentRequest(t *testing.T, token string) *pb.EnrollRequest {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	return &pb.EnrollRequest{EnrollmentToken: token, Hostname: "replacement", OperatingSystem: "Linux", AgentVersion: "1.11.0", CsrPem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})}
}

func TestReinstallFailureRollsBackIdentityAndOldUpdates(t *testing.T) {
	r, catalogue, nodes, nodeID, revision, entry, keys := agentTaskFixture(t)
	nodes.RecoveryUpdates = r
	ctx := context.Background()
	task, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	previous := strings.Repeat("b", 64)
	if _, err := r.Database.Exec("UPDATE node_identities SET fingerprint=?,expires_at='2999-01-01T00:00:00Z' WHERE node_id=?", previous, nodeID); err != nil {
		t.Fatal(err)
	}
	_, token, _, err := nodes.IssueRecovery(ctx, nodeID, "owner", time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	// Fail a real statement after the identity and task changes have run.
	if _, err := r.Database.Exec("CREATE TRIGGER reject_reinstall BEFORE UPDATE ON nodes BEGIN SELECT RAISE(ABORT,'reinstall transaction rejected'); END"); err != nil {
		t.Fatal(err)
	}
	authority, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	control := &node.Control{Repository: nodes, Authority: authority}
	peerContext := peer.NewContext(ctx, &peer.Peer{AuthInfo: credentials.TLSInfo{}})
	request := reinstallEnrollmentRequest(t, token)
	if _, err := control.Enroll(peerContext, request); err == nil {
		t.Fatal("failed enrollment succeeded")
	}
	var fingerprint, status string
	var consumed, retirements int
	if err := r.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", nodeID).Scan(&fingerprint); err != nil || fingerprint != previous {
		t.Fatalf("identity not rolled back: %s %v", fingerprint, err)
	}
	if err := r.Database.QueryRow("SELECT status FROM agent_update_tasks WHERE id=?", task.ID).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("old task not rolled back: %s %v", status, err)
	}
	if err := r.Database.QueryRow("SELECT count(*) FROM node_enrollment_tokens WHERE token_hash=? AND consumed_at IS NOT NULL", node.Hash(token)).Scan(&consumed); err != nil || consumed != 0 {
		t.Fatalf("token consumed on failure: %d %v", consumed, err)
	}
	if err := r.Database.QueryRow("SELECT count(*) FROM node_identity_retirements WHERE node_id=?", nodeID).Scan(&retirements); err != nil || retirements != 0 {
		t.Fatalf("identity retired on failure: %d %v", retirements, err)
	}
	if _, err := r.Database.Exec("DROP TRIGGER reject_reinstall"); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Enroll(peerContext, request); err != nil {
		t.Fatalf("rolled-back token cannot be retried: %v", err)
	}
}

func TestRetiredIdentityCannotDispatchReplacementUpdates(t *testing.T) {
	for _, kind := range []string{"system", "worker", "core"} {
		t.Run(kind, func(t *testing.T) {
			r, catalogue, _, nodeID, revision, entry, keys := agentTaskFixture(t)
			ctx := context.Background()
			switch kind {
			case "system":
				q := release.AgentSystemUpdateRequest{SchemaVersion: 1, Operation: "upgrade", TaskID: "task", AttemptID: "attempt", NodeID: nodeID, Revision: revision.ID, TargetVersion: "2.0.0"}
				data, err := json.Marshal(q)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := r.Database.Exec("INSERT INTO agent_system_update_tasks VALUES('task',?,?,'2.0.0','queued',1,'attempt','owner',?,?,'',NULL,'')", nodeID, revision.ID, now(), now()); err != nil {
					t.Fatal(err)
				}
				if _, err := r.Database.Exec("INSERT INTO agent_system_update_attempts(attempt_id,task_id,generation,request_json,created_at) VALUES('attempt','task',1,?,?)", data, now()); err != nil {
					t.Fatal(err)
				}
			case "worker":
				if _, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID); err != nil {
					t.Fatal(err)
				}
			case "core":
				if _, err := queueCoreForTest(r, catalogue, nodeID, revision.ID); err != nil {
					t.Fatal(err)
				}
			}
			stale := agentidentity.WithIdentity(ctx, nodeID, "retired-fingerprint")
			var err error
			switch kind {
			case "system":
				_, err = r.PendingAgentSystemUpdate(stale, nodeID)
			case "worker":
				_, err = r.PendingAgentUpdate(stale, nodeID)
			case "core":
				_, err = r.PendingCoreUpdate(stale, nodeID)
			}
			if err != agentidentity.ErrRetired {
				t.Fatalf("retired identity claimed %s command: %v", kind, err)
			}
		})
	}
}
