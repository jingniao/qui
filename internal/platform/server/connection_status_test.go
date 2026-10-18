package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

func connectionSnapshotJSON(t *testing.T, nodeID string, count uint64) []byte {
	t.Helper()
	encoded, err := json.Marshal(traffic.ConnectionSnapshot{
		NodeID: nodeID, Revision: 1, CoreEpoch: "epoch", ObservedAt: time.Now().UTC(),
		Scope: traffic.ConnectionScope, Connections: count,
		Accounts: []traffic.ConnectionAccount{{CredentialID: "cred", Connections: count}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// TestConnectionStatusIsAdvisoryAndNeverBreaksTheStream checks that a valid live
// connection snapshot is stored, while a snapshot naming another node or
// carrying invalid JSON is ignored without ending the stream.
func TestConnectionStatusIsAdvisoryAndNeverBreaksTheStream(t *testing.T) {
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	repository := &node.Repository{Database: app.Database}
	pending, token, _, err := repository.CreateEnrollment(context.Background(), "live-node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	connection := trustedClient(t, address, app.Authority.PEM, nil)
	client := pb.NewAgentControlClient(connection)
	directory := t.TempDir()
	csr, err := identity.CSR(directory)
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := client.Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.SaveEnrollment(directory, enrollment.NodeId, enrollment.CertificatePem, enrollment.CaPem); err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(filepath.Join(directory, "client.crt"), filepath.Join(directory, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, &certificate)).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pb.AgentMessage{NodeId: pending.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{}}}); err != nil {
		t.Fatal(err)
	}
	if welcome, err := stream.Recv(); err != nil || welcome.GetWelcome() == nil {
		t.Fatal("missing Welcome", err)
	}

	send := func(sequence uint64, id, payload string) {
		message := &pb.AgentMessage{NodeId: pending.ID, MessageId: id, Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_ConnectionStatus{ConnectionStatus: &pb.ConnectionStatus{SnapshotJson: []byte(payload)}}}
		if err := stream.Send(message); err != nil {
			t.Fatalf("send %s: %v", id, err)
		}
		ack, err := stream.Recv()
		if err != nil || ack.GetAck().AgentSequence != sequence {
			t.Fatalf("ack %s: %v", id, err)
		}
	}

	send(1, "valid", string(connectionSnapshotJSON(t, pending.ID, 12)))
	send(2, "mismatch", string(connectionSnapshotJSON(t, "other-node", 5)))
	send(3, "invalid", "{not json")

	if snapshot, ok := app.connectionStore.Snapshot(pending.ID); !ok || snapshot.Connections != 12 {
		t.Fatalf("valid snapshot not stored: %+v %v", snapshot, ok)
	}
	if snapshots := app.connectionStore.Snapshots(); len(snapshots) != 1 || snapshots[0].NodeID != pending.ID {
		t.Fatalf("mismatched or invalid snapshot was stored: %+v", snapshots)
	}

	// The stream is still alive: a following heartbeat is still acked.
	heartbeat := &pb.AgentMessage{NodeId: pending.ID, MessageId: "heartbeat", Sequence: 4, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{CpuPercent: proto.Float64(11)}}}
	if err := stream.Send(heartbeat); err != nil {
		t.Fatal(err)
	}
	if ack, err := stream.Recv(); err != nil || ack.GetAck().AgentSequence != 4 {
		t.Fatal("stream ended after advisory connection status", err)
	}
}
