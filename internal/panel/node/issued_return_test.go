package node

import (
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
)

func TestBudgetRetirementResultEnvelopeRejectsUnknownAndUnboundedData(t *testing.T) {
	for _, result := range []*pb.BudgetRetirementResult{
		{Success: true, Code: "success", ReceiptJson: []byte("{}")},
		{RequestId: "request", Success: true, Code: "success"},
		{RequestId: "request", Code: "retirement_failed", ReceiptJson: []byte("{}")},
		{RequestId: "request", Code: "shell"},
		{RequestId: "request", Success: true, Code: "success", ReceiptJson: make([]byte, traffic.MaxBudgetRetirementBytes+1)},
	} {
		message := &pb.AgentMessage{NodeId: "node", MessageId: "message", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_BudgetRetirementResult{BudgetRetirementResult: result}}
		if err := validateMessage("node", message); err == nil {
			t.Fatal("invalid retirement result accepted")
		}
	}
}
