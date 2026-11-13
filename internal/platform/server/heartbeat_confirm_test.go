package server

import (
	"context"
	"crypto/tls"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"proxycontrol/internal/agent/identity"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// v1.5.4 item 1: the Agent keeps apply results in session memory, so a session
// that ends before the result is sent loses it. A fresh heartbeat naming the
// pending revision as successful with healthy cores settles it the same way
// the lost ApplyResult would have; anything else leaves it pending.
func TestHeartbeatConfirmsPendingRevision(t *testing.T) {
	t.Parallel()
	healthy, unhealthy := true, false
	for _, test := range []struct {
		name    string
		offset  int64 // added to the pending revision
		healthy *bool
		want    string
	}{
		{"same revision healthy", 0, &healthy, "success"},
		{"same revision unhealthy", 0, &unhealthy, "pending"},
		{"same revision health unknown", 0, nil, "pending"},
		{"older revision", -1, &healthy, "pending"},
		{"newer revision", 1, &healthy, "pending"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			app := testServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			n, token, _, err := app.nodeRepository.CreateEnrollment(ctx, "heartbeat confirm", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			address, stop := startControl(t, app)
			defer stop()
			dir := t.TempDir()
			csr, err := identity.CSR(dir)
			if err != nil {
				t.Fatal(err)
			}
			enrolled, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, nil)).Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if err := identity.SaveEnrollment(dir, enrolled.NodeId, enrolled.CertificatePem, enrolled.CaPem); err != nil {
				t.Fatal(err)
			}
			pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"))
			if err != nil {
				t.Fatal(err)
			}
			state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
			baseline, err := app.deployments.Create(ctx, state, "owner", "baseline")
			if err != nil {
				t.Fatal(err)
			}
			if err := app.deployments.Deploy(ctx, baseline.ID); err != nil {
				t.Fatal(err)
			}
			if err := app.deployments.Result(ctx, n.ID, &pb.ApplyResult{Revision: baseline.ID, Success: true, Code: "success"}); err != nil {
				t.Fatal(err)
			}
			pending, err := app.deployments.Create(ctx, state, "owner", "pending")
			if err != nil {
				t.Fatal(err)
			}
			if err := app.deployments.Deploy(ctx, pending.ID); err != nil {
				t.Fatal(err)
			}

			stream, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, &pair)).Connect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{}}}); err != nil {
				t.Fatal(err)
			}
			if welcome, err := stream.Recv(); err != nil || welcome.GetWelcome() == nil {
				t.Fatal(welcome, err)
			}
			revision := pending.ID + test.offset
			heartbeat := &pb.Heartbeat{CurrentRevision: revision, SuccessfulRevision: revision, CoreHealthy: test.healthy}
			if test.healthy != nil && !*test.healthy {
				heartbeat.CoreError = "core_unhealthy"
			}
			if err := stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "heartbeat", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: heartbeat}}); err != nil {
				t.Fatal(err)
			}
			if ack, err := stream.Recv(); err != nil || ack.GetAck() == nil {
				t.Fatal(ack, err)
			}
			var got string
			if err := app.Database.QueryRowContext(ctx, "SELECT status FROM deployment_targets WHERE revision=? AND node_id=?", pending.ID, n.ID).Scan(&got); err != nil || got != test.want {
				t.Fatalf("target status %q, want %q: %v", got, test.want, err)
			}
			if test.want == "success" {
				var runtime int64
				if err := app.Database.QueryRowContext(ctx, "SELECT revision FROM node_runtime_state WHERE node_id=?", n.ID).Scan(&runtime); err != nil || runtime != pending.ID {
					t.Fatalf("runtime revision %d, want %d: %v", runtime, pending.ID, err)
				}
				// The real result arriving later is the same fact: accepted
				// as a no-op. A contradicting failure is still refused.
				if err := app.deployments.Result(ctx, n.ID, &pb.ApplyResult{Revision: pending.ID, Success: true, Code: "success"}); err != nil {
					t.Fatal("late success result refused:", err)
				}
				if err := app.deployments.Result(ctx, n.ID, &pb.ApplyResult{Revision: pending.ID, Code: "apply_failed"}); err == nil {
					t.Fatal("late failure result accepted after confirmation")
				}
			}
			stream.CloseSend()
		})
	}
}
