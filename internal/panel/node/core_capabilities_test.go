package node

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// fakeCoreUpdates records the closure and the capabilities the node reported when
// it happened.
type fakeCoreUpdates struct {
	repository *Repository
	calls      int
	version    string
}

func (f *fakeCoreUpdates) PendingCoreUpdate(context.Context, string) (*pb.CoreUpdateCommand, error) {
	return nil, nil
}
func (f *fakeCoreUpdates) CoreUpdateResult(context.Context, string, *pb.CoreUpdateResult) error {
	return nil
}
func (f *fakeCoreUpdates) CloseStaleCoreUpdates(ctx context.Context, nodeID string) error {
	f.calls++
	var stored []byte
	if err := f.repository.Database.QueryRowContext(ctx, "SELECT capabilities_json FROM nodes WHERE id=?", nodeID).Scan(&stored); err != nil {
		return err
	}
	capabilities := &pb.Capabilities{}
	if err := protojson.Unmarshal(stored, capabilities); err != nil {
		return err
	}
	f.version = capabilities.CoreVersion
	return nil
}

// A reconnect must not close the node's unconfirmed core update before the
// connection stored the capabilities of the core that is installed now: the
// closure compares the requested version with exactly those capabilities.
func TestStaleCoreUpdateClosureWaitsForStoredCapabilities(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &Repository{Database: database}
	n, _, _, err := repository.CreateEnrollment(ctx, "Update node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := func(sequence uint64, message *pb.AgentMessage) {
		t.Helper()
		message.NodeId, message.Sequence, message.MessageId, message.Timestamp = n.ID, sequence, "message", timestamppb.Now()
		if _, err := repository.Record(ctx, n.ID, message); err != nil {
			t.Fatal(err)
		}
	}
	store(1, &pb.AgentMessage{Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27"}}})
	recorder := &fakeCoreUpdates{repository: repository}
	control := &Control{Repository: repository, CoreUpdates: recorder}
	session := &coreUpdateSession{}
	if err := control.coreUpdateCapabilitiesStored(ctx, n.ID, &pb.AgentMessage{Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}, session); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 0 {
		t.Fatal("closure ran before the connection reported its capabilities")
	}
	// The node reconnects reporting the version it now runs.
	fresh := &pb.AgentMessage{Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.28"}}}
	store(2, fresh)
	if err := control.coreUpdateCapabilitiesStored(ctx, n.ID, fresh, session); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 1 || recorder.version != "26.3.28" {
		t.Fatalf("closure used %q after %d calls", recorder.version, recorder.calls)
	}
	// One closure per connection, even when the node reports again.
	if err := control.coreUpdateCapabilitiesStored(ctx, n.ID, fresh, session); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 1 {
		t.Fatalf("closure repeated within one connection: %d", recorder.calls)
	}
}
