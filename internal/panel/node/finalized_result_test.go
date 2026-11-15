package node

import (
	"bytes"
	"context"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"log/slog"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
)

type finalizedDeployment struct{ result error }

func (d finalizedDeployment) State(context.Context, int64) (desiredstate.State, error) {
	return desiredstate.State{}, nil
}
func (d finalizedDeployment) Pending(context.Context, string) (*pb.ApplyCommand, error) {
	return nil, nil
}
func (d finalizedDeployment) PendingRollback(context.Context, string) (*pb.RollbackCommand, error) {
	return nil, nil
}
func (d finalizedDeployment) Result(context.Context, string, *pb.ApplyResult) error { return d.result }

type receiptStream struct {
	*fakeAgentStream
	messages []*pb.AgentMessage
}

func (s *receiptStream) Recv() (*pb.AgentMessage, error) {
	if len(s.messages) == 0 {
		return nil, io.EOF
	}
	message := s.messages[0]
	s.messages = s.messages[1:]
	return message, nil
}
func TestControlAcknowledgesFinalizedApplyButRejectsUnknown(t *testing.T) {
	for _, ignored := range []bool{true, false} {
		t.Run(map[bool]string{true: "finalized", false: "unknown"}[ignored], func(t *testing.T) {
			p := newRecoveryPanel(t)
			n, leaf := enrolledAgentNode(t, p)
			base := newFakeAgentStream(t, leaf, n.ID, "hello")
			stream := &receiptStream{fakeAgentStream: base, messages: []*pb.AgentMessage{base.hello, {NodeId: n.ID, MessageId: "result", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_ApplyResult{ApplyResult: &pb.ApplyResult{Revision: 77, Success: true}}}, {NodeId: n.ID, MessageId: "heartbeat", Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}}}
			var result error = errors.New("unknown revision")
			if ignored {
				result = &IgnoredDeploymentResult{Reason: "failed"}
			}
			notified := 0
			var logs bytes.Buffer
			control := &Control{Repository: p.Repository, Authority: p.authority, Logger: slog.New(slog.NewTextHandler(&logs, nil)), Deployment: finalizedDeployment{result: result}, DeploymentNotify: func(int64) { notified++ }}
			err := control.Connect(stream)
			if !ignored {
				if status.Code(err) != codes.FailedPrecondition {
					t.Fatal(err)
				}
				return
			}
			if err != nil && !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			acks := 0
			for _, message := range stream.sent {
				if message.GetAck() != nil {
					acks++
				}
			}
			if acks != 2 || notified != 0 {
				t.Fatalf("acks=%d notifications=%d", acks, notified)
			}
			if strings.Count(logs.String(), "level=WARN") != 1 || !strings.Contains(logs.String(), "revision=77") || !strings.Contains(logs.String(), n.ID) {
				t.Fatal("missing single classified warning", logs.String())
			}
		})
	}
}
