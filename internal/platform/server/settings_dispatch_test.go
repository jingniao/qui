package server

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// A queued command can outlive the Agent/core report that authorized it. Real
// mTLS streams must keep acknowledging old/replayed peers while withholding
// both apply and rollback until this connection reports the exact combination.
func TestSettingsDispatchAfterReconnectAndCapabilityWithdrawal(t *testing.T) {
	// Not parallel: its deadlines failed under parallel -race load (docs/build-speedup-plan.md 3).
	for _, operation := range []string{"apply", "rollback", "rollback_empty"} {
		t.Run(operation, func(t *testing.T) {
			rollback := operation != "apply"
			app := testServer(t)
			address, stop := startControl(t, app)
			defer stop()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			nodes := &node.Repository{Database: app.Database}
			n, token, _, err := nodes.CreateEnrollment(ctx, "Settings stream node", time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			csr, err := identity.CSR(directory)
			if err != nil {
				t.Fatal(err)
			}
			response, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, nil)).Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "settings", OperatingSystem: "linux/amd64", AgentVersion: "settings-test"})
			if err != nil {
				t.Fatal(err)
			}
			if err := identity.SaveEnrollment(directory, response.NodeId, response.CertificatePem, response.CaPem); err != nil {
				t.Fatal(err)
			}
			pair, err := tls.LoadX509KeyPair(filepath.Join(directory, "client.crt"), filepath.Join(directory, "client.key"))
			if err != nil {
				t.Fatal(err)
			}
			client := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, &pair))
			sessionCtx, endSession := context.WithCancel(ctx)
			defer endSession()
			stream, err := client.Connect(sessionCtx)
			if err != nil {
				t.Fatal(err)
			}
			hello := func(last uint64) {
				t.Helper()
				if err := stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{LastAckedSequence: last}}}); err != nil {
					t.Fatal(err)
				}
				message, err := stream.Recv()
				if err != nil || message.GetWelcome() == nil {
					t.Fatalf("welcome: %v", err)
				}
			}
			ack := func(message *pb.AgentMessage) {
				t.Helper()
				if err := stream.Send(message); err != nil {
					t.Fatal(err)
				}
				response, err := stream.Recv()
				if err != nil || response.GetAck() == nil || response.GetAck().AgentSequence != message.Sequence {
					t.Fatalf("expected ack for %d, got %v: %v", message.Sequence, response, err)
				}
			}
			capabilities := func(sequence uint64, supported bool) *pb.AgentMessage {
				caps := &pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}
				if supported {
					caps.ServerSettingsContractVersion = 1
					caps.ServerSettingsCombinations = []*pb.SettingsCombination{{Protocol: "vless", Direction: "inbound", Security: "reality", Transport: "tcp"}}
				}
				return &pb.AgentMessage{NodeId: n.ID, MessageId: "capabilities", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: caps}}
			}
			heartbeat := func(sequence uint64) *pb.AgentMessage {
				return &pb.AgentMessage{NodeId: n.ID, MessageId: "heartbeat", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}
			}
			hello(0)
			original := capabilities(1, true)
			ack(original)
			endSession()
			deadline := time.Now().Add(2 * time.Second)
			for {
				current, err := nodes.Get(ctx, n.ID)
				if err == nil && current.Status == "offline" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("first session did not close")
				}
				time.Sleep(10 * time.Millisecond)
			}
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true},
				Inbounds: []desiredstate.Inbound{{ID: "in", Protocol: "vless", Listen: "0.0.0.0", Port: 24443, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Security: "reality", Reality: &desiredstate.RealityServerSettings{
					PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), HandshakeTarget: "target.example:443", ServerNames: []string{"target.example"}, ShortIDs: []string{"abcd"},
				}}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
			revision, err := app.deployments.Create(ctx, state, "owner", "Queued before reconnect")
			if err != nil {
				t.Fatal(err)
			}
			if operation == "rollback_empty" {
				if _, err := app.Database.Exec("INSERT INTO node_runtime_state(node_id,revision) VALUES(?,?)", n.ID, revision.ID); err != nil {
					t.Fatal(err)
				}
				revision.ID = 0
			}
			// Model a command queued under the previous supported report, then a
			// disconnect/downgrade before its first delivery.
			query := "INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(?,?,'pending',?)"
			if rollback {
				query = "INSERT INTO deployment_commands(revision,node_id,status,created_at,id,operation) VALUES(?,?,'pending',?,'settings-rollback','rollback')"
			}
			if _, err := app.Database.Exec(query, revision.ID, n.ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			// The panel marks the node offline before it drops the old session from
			// its connected set, so a reconnect right after the status change can
			// still be refused as already connected, as a real Agent would see
			// before its retry; retry the same way.
			for deadline := time.Now().Add(5 * time.Second); ; {
				stream, err = client.Connect(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err := stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{LastAckedSequence: 1}}}); err != nil {
					t.Fatal(err)
				}
				message, err := stream.Recv()
				if err == nil && message.GetWelcome() != nil {
					break
				}
				if status.Code(err) != codes.AlreadyExists || time.Now().After(deadline) {
					t.Fatalf("welcome: %v", err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if operation == "rollback_empty" {
				// A legacy peer sends only a heartbeat, without a fresh settings
				// report. Clearing runtime must not attempt to read revision zero.
				ack(heartbeat(2))
				command, err := stream.Recv()
				if err != nil || command.GetRollbackCommand() == nil || command.GetRollbackCommand().Revision != 0 {
					t.Fatalf("legacy rollback-to-empty unavailable: %v, %v", command, err)
				}
				ack(&pb.AgentMessage{NodeId: n.ID, MessageId: "cleared", Sequence: 3, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_ApplyResult{ApplyResult: &pb.ApplyResult{CommandId: command.GetRollbackCommand().CommandId, Revision: 0, Success: true, Code: "success"}}})
				var runtimeRows int
				if err := app.Database.QueryRow("SELECT count(*) FROM node_runtime_state WHERE node_id=?", n.ID).Scan(&runtimeRows); err != nil || runtimeRows != 0 {
					t.Fatalf("rollback-to-empty did not clear runtime: rows=%d, %v", runtimeRows, err)
				}
				return
			}
			ack(original)               // exact accepted-envelope replay must not open the session
			ack(heartbeat(2))           // catches an unexpected command following the prior ACK
			ack(capabilities(3, false)) // downgraded Agent explicitly withdraws support
			ack(heartbeat(4))           // stream stays healthy; command remains withheld
			ack(capabilities(5, true))  // normal fresh reconnect report opens the gate
			command, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			if rollback && command.GetRollbackCommand() == nil || !rollback && command.GetApplyCommand() == nil {
				t.Fatalf("expected queued command after current report, got %v", command)
			}
		})
	}
}
