package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/config"
	contract "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAccessMTLSLostAcknowledgementReplayAndStaleSequence(t *testing.T) {
	ctx := context.Background()
	app := testServer(t)
	app.Config.ShutdownTimeout = time.Second
	address, stop := startControl(t, app)
	defer stop()
	nodes := &node.Repository{Database: app.Database}
	n, token, _, err := nodes.CreateEnrollment(ctx, "Replay", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	ca := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(ca, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	c := config.DefaultAgent()
	c.ControlAddress = address
	c.DataDirectory = filepath.Join(directory, "agent")
	c.TLS.ClientCA = ca
	if err := control.Enroll(ctx, c, token, "replay-test"); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(directory, "agent", "identity", "client.crt"), filepath.Join(directory, "agent", "identity", "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	client := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, &pair))
	revision := accessRevision(t, app, n.ID)
	batch := contract.Batch{ID: "lost-ack", NodeID: n.ID, Records: []contract.Rollup{telemetryRow(n.ID, revision, "replay.example")}}
	data, _ := json.Marshal(batch)
	firstCtx, firstCancel := context.WithCancel(ctx)
	first, err := client.Connect(firstCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Recv(); err != nil {
		t.Fatal(err)
	}
	if err := first.Send(&pb.AgentMessage{NodeId: n.ID, Sequence: 1, MessageId: "first", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_AccessBatch{AccessBatch: &pb.AccessBatch{BatchJson: data}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		last, err := nodes.LastSequence(ctx, n.ID)
		if err == nil && last == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("access receipt not committed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Lose the response after both transactions committed, then reconnect from
	// the old locally confirmed sequence. The new stream must recognize the ID.
	firstCancel()
	for {
		record, err := nodes.Get(ctx, n.ID)
		if err == nil && record.Status == "offline" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first stream did not close")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The old stream's cleanup persists offline before releasing its in-memory
	// connection slot, so a reconnect in that window is refused with
	// AlreadyExists; a real Agent retries with backoff, and so does this test.
	secondCtx, secondCancel := context.WithCancel(ctx)
	defer secondCancel()
	var second pb.AgentControl_ConnectClient
	var welcome *pb.ServerMessage
	for {
		second, err = client.Connect(secondCtx)
		if err != nil {
			t.Fatal(err)
		}
		second.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "rehello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{LastAckedSequence: 0}}})
		welcome, err = second.Recv()
		if status.Code(err) != codes.AlreadyExists || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || welcome.GetWelcome().LastAcceptedSequence != 1 {
		t.Fatal("sequence not restored", err)
	}
	second.Send(&pb.AgentMessage{NodeId: n.ID, Sequence: 2, MessageId: "replay", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_AccessBatch{AccessBatch: &pb.AccessBatch{BatchJson: data}}})
	ack, err := second.Recv()
	if err != nil || ack.GetAck().AccessBatchId != batch.ID || ack.GetAck().AgentSequence != 2 {
		t.Fatal("replayed batch acknowledgement wrong", err)
	}
	var connections int64
	if err := app.Database.QueryRow("SELECT sum(connections) FROM access_rollups").Scan(&connections); err != nil || connections != 1 {
		t.Fatal("lost ACK duplicated access", connections, err)
	}
	batch.ID = "new-on-stale-sequence"
	data, _ = json.Marshal(batch)
	second.Send(&pb.AgentMessage{NodeId: n.ID, Sequence: 1, MessageId: "stale", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_AccessBatch{AccessBatch: &pb.AccessBatch{BatchJson: data}}})
	if _, err := second.Recv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("stale sequence created new batch", err)
	}
	var receipts int
	app.Database.QueryRow("SELECT count(*) FROM access_batches").Scan(&receipts)
	if receipts != 1 {
		t.Fatal("stale batch was stored", receipts)
	}
}
