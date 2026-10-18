package node

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestMessageValidationRejectsAmbiguousOrInvalidPayloads(t *testing.T) {
	valid := &pb.AgentMessage{NodeId: "node-a", MessageId: "message-1", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}
	if err := validateMessage("node-a", valid); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*pb.AgentMessage){func(m *pb.AgentMessage) { m.NodeId = "node-b" }, func(m *pb.AgentMessage) { m.Sequence = 0 }, func(m *pb.AgentMessage) { m.Sequence = math.MaxUint64 }, func(m *pb.AgentMessage) { m.MessageId = "" }, func(m *pb.AgentMessage) { m.Timestamp = nil }, func(m *pb.AgentMessage) { m.Payload = nil }, func(m *pb.AgentMessage) { m.GetHeartbeat().CpuPercent = proto.Float64(math.NaN()) }, func(m *pb.AgentMessage) { m.GetHeartbeat().MemoryPercent = proto.Float64(101) }, func(m *pb.AgentMessage) { m.GetHeartbeat().PublicIp = "not-an-IP" }, func(m *pb.AgentMessage) { m.GetHeartbeat().CurrentRevision = -1 }, func(m *pb.AgentMessage) {
		m.Payload = &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "unknown"}}
	}, func(m *pb.AgentMessage) {
		m.Payload = &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{Protocols: []*pb.ProtocolCapability{{Protocol: "vless"}, {Protocol: "vless"}}}}
	}}
	for index, mutate := range mutations {
		message := proto.Clone(valid).(*pb.AgentMessage)
		mutate(message)
		if validateMessage("node-a", message) == nil {
			t.Fatalf("accepted invalid message case %d", index)
		}
	}
}

func TestCoreHealthPayloadAndDegradedLifecycle(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &Repository{Database: database}
	n, _, _, err := repository.CreateEnrollment(ctx, "Accounting node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	message := &pb.AgentMessage{NodeId: n.ID, MessageId: "health", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{CoreHealthy: proto.Bool(false), CoreError: "accounting_interrupted"}}}
	if err := validateMessage(n.ID, message); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*pb.Heartbeat){
		func(h *pb.Heartbeat) { h.CoreError = "arbitrary error or private content" },
		func(h *pb.Heartbeat) { h.CoreHealthy = nil },
		func(h *pb.Heartbeat) { h.CoreHealthy = proto.Bool(true) },
	} {
		invalid := proto.Clone(message).(*pb.AgentMessage)
		mutate(invalid.GetHeartbeat())
		if err := validateMessage(n.ID, invalid); err == nil {
			t.Fatal("invalid or contradictory core health accepted")
		}
	}
	if _, err := repository.Record(ctx, n.ID, message); err != nil {
		t.Fatal(err)
	}
	current, err := repository.Get(ctx, n.ID)
	if err != nil || current.Status != "degraded" || current.Heartbeat.CoreHealthy == nil || current.Heartbeat.GetCoreHealthy() || current.Heartbeat.CoreError != "accounting_interrupted" {
		t.Fatal("interrupted proxy reported as healthy node", current, err)
	}
	encoded, err := json.Marshal(current.Heartbeat)
	if err != nil || !json.Valid(encoded) {
		t.Fatal(err)
	}
	var fields map[string]any
	json.Unmarshal(encoded, &fields)
	if healthy, exists := fields["core_healthy"]; !exists || healthy != false {
		t.Fatal("false health observation disappeared from HTTP payload", string(encoded))
	}
	if err := repository.Offline(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	current, err = repository.Get(ctx, n.ID)
	if err != nil || current.Status != "offline" {
		t.Fatal("degraded node did not become offline on disconnect", current, err)
	}
	message.Sequence++
	if _, err := repository.Record(ctx, n.ID, message); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE nodes SET last_heartbeat=? WHERE id=?", timestamp(time.Now().Add(-time.Minute)), n.ID); err != nil {
		t.Fatal(err)
	}
	current, err = repository.Get(ctx, n.ID)
	if err != nil || current.Status != "offline" {
		t.Fatal("stale degraded node did not become offline", current, err)
	}
	message.Sequence++
	message.GetHeartbeat().CoreHealthy = proto.Bool(true)
	message.GetHeartbeat().CoreError = ""
	if _, err := repository.Record(ctx, n.ID, message); err != nil {
		t.Fatal(err)
	}
	current, err = repository.Get(ctx, n.ID)
	if err != nil || current.Status != "online" {
		t.Fatal("healthy core did not recover node status", current, err)
	}
}

func TestNodeRepositoryRejectsSequenceGapsAndDisabledUpdates(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &Repository{Database: database}
	n, _, _, err := repository.CreateEnrollment(ctx, "Node A", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	message := &pb.AgentMessage{NodeId: n.ID, MessageId: "test", Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}
	if _, err := repository.Record(ctx, n.ID, message); !errors.Is(err, ErrSequence) {
		t.Fatal("sequence gap accepted", err)
	}
	message.Sequence = 1
	if accepted, err := repository.Record(ctx, n.ID, message); err != nil || accepted != 1 {
		t.Fatal(accepted, err)
	}
	if err := repository.Update(ctx, n.ID, n.Name, false); err != nil {
		t.Fatal(err)
	}
	message.Sequence = 2
	if _, err := repository.Record(ctx, n.ID, message); !errors.Is(err, ErrIdentity) {
		t.Fatal("disabled node updated", err)
	}
	current, err := repository.Get(ctx, n.ID)
	if err != nil || current.Status != "disabled" || current.LastSequence != 1 {
		t.Fatal("disabled state changed", err)
	}
}
