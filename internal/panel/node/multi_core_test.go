package node

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"testing"
	"time"
)

func TestMultiCoreCapabilitiesAndHealthPersistWithoutInventedValues(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(ctx, "multi", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	caps := &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, Cores: []*pb.CoreCapability{{Name: "xray", Version: "26.3.27", DeviceGateSupported: true}, {Name: "sing-box", Version: "1.14.3"}}}
	if err := validateMultiCapabilities(caps); err != nil {
		t.Fatal(err)
	}
	for i, payload := range []any{caps, &pb.Heartbeat{Cores: []*pb.CoreStatus{{Name: "xray", Healthy: true}, {Name: "sing-box", Healthy: false, Error: "core_unhealthy"}}}} {
		m := &pb.AgentMessage{NodeId: n.ID, Sequence: uint64(i + 1), MessageId: "m", Timestamp: timestamppb.Now()}
		switch p := payload.(type) {
		case *pb.Capabilities:
			m.Payload = &pb.AgentMessage_Capabilities{Capabilities: p}
		case *pb.Heartbeat:
			m.Payload = &pb.AgentMessage_Heartbeat{Heartbeat: p}
		}
		if _, err := r.Record(ctx, n.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.Get(ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Capabilities.Cores) != 2 || len(got.Heartbeat.Cores) != 2 || got.Capabilities.Cores[1].Healthy != nil || !strings.Contains(string(raw), `"healthy":false`) {
		t.Fatal(string(raw))
	}
	legacy := &pb.Capabilities{CoreName: "xray"}
	cores := EnabledCores(legacy)
	if len(cores) != 1 || cores[0].Healthy != nil {
		t.Fatal(cores)
	}
	caps.Cores = append(caps.Cores, caps.Cores[0])
	if validateMultiCapabilities(caps) == nil {
		t.Fatal("duplicate core accepted")
	}
}

// An Agent that only decorates its scalar capabilities reports one named core
// without the multi-core flag; that report must stay a valid one-core node, and
// a contradictory name or a second core must still be refused.
func TestSingleDecoratedCoreWithoutMultiCoreFlagIsAccepted(t *testing.T) {
	decorated := &pb.Capabilities{CoreName: "xray", Cores: []*pb.CoreCapability{{Name: "xray", Version: "26.3.27"}}}
	if err := validateMultiCapabilities(decorated); err != nil {
		t.Fatal(err)
	}
	if cores := EnabledCores(decorated); len(cores) != 1 || cores[0].Name != "xray" {
		t.Fatal(cores)
	}
	decorated.Cores[0].Name = "sing-box"
	if validateMultiCapabilities(decorated) == nil {
		t.Fatal("contradictory core list accepted")
	}
	two := &pb.Capabilities{CoreName: "xray", Cores: []*pb.CoreCapability{{Name: "xray"}, {Name: "sing-box"}}}
	if validateMultiCapabilities(two) == nil {
		t.Fatal("two cores without the multi-core flag accepted")
	}
}
