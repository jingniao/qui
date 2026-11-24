package node

import (
	"context"
	"errors"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

type livenessPacket struct {
	message *pb.AgentMessage
	err     error
}
type livenessStream struct {
	*fakeAgentStream
	incoming chan livenessPacket
}

func (s *livenessStream) Recv() (*pb.AgentMessage, error) {
	s.mu.Lock()
	hello := s.hello
	s.hello = nil
	s.mu.Unlock()
	if hello != nil {
		return hello, nil
	}
	packet, ok := <-s.incoming
	if !ok {
		return nil, io.EOF
	}
	return packet.message, packet.err
}

// Occupy the actual sole SQL connection after Welcome. The receiver must
// register the message and observe a broken stream while ValidateIdentity waits.
func TestLivenessWhileDatabaseConnectionIsOccupied(t *testing.T) {
	p := newRecoveryPanel(t)
	n, leaf := enrolledAgentNode(t, p)
	p.Database.SetMaxOpenConns(1)
	live := NewLiveness(nil)
	p.Repository.Liveness = live
	stream := &livenessStream{fakeAgentStream: newFakeAgentStream(t, leaf, n.ID, "hello"), incoming: make(chan livenessPacket, 2)}
	defer close(stream.incoming)
	control := &Control{Repository: p.Repository, Authority: p.authority, Liveness: live}
	done := make(chan error, 1)
	go func() { done <- control.Connect(stream) }()
	waitForWelcome(t, stream.fakeAgentStream)
	conn, err := p.Database.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream.incoming <- livenessPacket{message: &pb.AgentMessage{NodeId: n.ID, MessageId: "blocked-heartbeat", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}}
	waitLive := func(check func(LivenessFact) bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if check(live.Snapshot(time.Now())[n.ID]) {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("liveness did not change independently of SQL")
	}
	waitLive(func(f LivenessFact) bool { return f.Connected && f.InFlightCount == 1 })
	now := time.Now().Add(46 * time.Second)
	fact := live.Snapshot(now)[n.ID]
	if ConfirmedOfflineWithLiveness("online", time.Now().Add(-time.Hour).Format(time.RFC3339Nano), 0, now, fact) || !fact.ProcessingDelayed {
		t.Fatalf("blocked message evidence: %+v", fact)
	}
	stream.mu.Lock()
	for _, sent := range stream.sent {
		if sent.GetAck() != nil {
			t.Error("Ack preceded database commit")
		}
	}
	stream.mu.Unlock()
	stream.incoming <- livenessPacket{err: errors.New("transport failed")}
	waitLive(func(f LivenessFact) bool { return !f.Connected && f.InFlightCount == 0 })
	// The independent receive error cancels the SQL wait. Release the connection
	// only after observing invalidation, so Offline cleanup can finish normally.
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stream error hidden")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not stop")
	}
	var sequence uint64
	if err := p.Database.QueryRow("SELECT last_sequence FROM nodes WHERE id=?", n.ID).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	if sequence != 0 {
		t.Fatalf("uncommitted message accepted: %d", sequence)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	for _, sent := range stream.sent {
		if sent.GetAck() != nil {
			t.Fatal("failed message acknowledged")
		}
	}
}
