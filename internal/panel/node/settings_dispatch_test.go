package node

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

type settingsDeployment struct{ state desiredstate.State }

func (d *settingsDeployment) State(context.Context, int64) (desiredstate.State, error) {
	return d.state, nil
}
func (*settingsDeployment) Pending(context.Context, string) (*pb.ApplyCommand, error) {
	return nil, nil
}
func (*settingsDeployment) PendingRollback(context.Context, string) (*pb.RollbackCommand, error) {
	return nil, nil
}
func (*settingsDeployment) Result(context.Context, string, *pb.ApplyResult) error { return nil }

// Dispatch reads the persisted report after accepting the session's message.
// Exercise both command boundaries against an old/replayed session, report
// withdrawal, and an unknown future contract that must replace previous grants.
func TestSettingsCommandsRequireFreshSessionAndCurrentContract(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &Repository{Database: database}
	n, _, _, err := repository.CreateEnrollment(ctx, "Settings node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	state := desiredstate.State{SchemaVersion: 1, Revision: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true},
		Inbounds: []desiredstate.Inbound{{ID: "in", Protocol: "vless", Listen: "0.0.0.0", Port: 24443, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: &desiredstate.RealityServerSettings{
				PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
				HandshakeTarget: "target.example:443", ServerNames: []string{"target.example"}, ShortIDs: []string{"abcd"},
			}}}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	deployment := &settingsDeployment{state: state}
	control := &Control{Repository: repository, Deployment: deployment}
	session := &settingsContractSession{}
	sequence := uint64(0)
	store := func(capabilities *pb.Capabilities, fresh bool) *pb.AgentMessage {
		t.Helper()
		if fresh {
			sequence++
		}
		message := &pb.AgentMessage{NodeId: n.ID, MessageId: "capabilities", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}
		if err := validateMessage(n.ID, message); err != nil {
			t.Fatal(err)
		}
		if fresh {
			if _, err := repository.Record(ctx, n.ID, message); err != nil {
				t.Fatal(err)
			}
		}
		session.record(message, fresh)
		return message
	}
	check := func(want bool) {
		t.Helper()
		data, err := json.Marshal(deployment.state)
		if err != nil {
			t.Fatal(err)
		}
		apply, err := control.canDispatchApply(ctx, n.ID, &pb.ApplyCommand{DesiredStateJson: data}, session)
		if err != nil || apply != want {
			t.Fatalf("apply allowed=%v want=%v: %v", apply, want, err)
		}
		rollback, err := control.canDispatchRollback(ctx, n.ID, &pb.RollbackCommand{Revision: 1}, session)
		if err != nil || rollback != want {
			t.Fatalf("rollback allowed=%v want=%v: %v", rollback, want, err)
		}
	}
	known := func() *pb.Capabilities {
		return &pb.Capabilities{CoreName: "xray", Protocols: reportedVLESS, ServerSettingsContractVersion: 1, ServerSettingsCombinations: []*pb.SettingsCombination{reportReality()}}
	}
	store(known(), true)
	check(true)
	// Reconnection forgets the prior session. A replayed old report cannot open it.
	session = &settingsContractSession{}
	check(false)
	store(known(), false)
	check(false)
	store(&pb.Capabilities{CoreName: "xray", Protocols: reportedVLESS}, true)
	check(false)
	store(known(), true)
	check(true)
	for _, multi := range []bool{false, true} {
		future := &pb.Capabilities{CoreName: "xray", Protocols: reportedVLESS,
			ServerSettingsContractVersion: 2, ServerSettingsCombinations: []*pb.SettingsCombination{{Protocol: "vless", Direction: "future-direction", Security: "future-security", Transport: "future-transport", Flow: "future-flow"}}}
		if multi {
			future.MultiCoreSupported = true
			future.Cores = []*pb.CoreCapability{{Name: "xray", Protocols: reportedVLESS, ServerSettingsContractVersion: 2, ServerSettingsCombinations: future.ServerSettingsCombinations}}
		}
		store(future, true)
		check(false)
		current, err := repository.Get(ctx, n.ID)
		if err != nil || SelectedCore(current.Capabilities, "").ServerSettingsContractVersion != 2 {
			t.Fatalf("future report did not replace old grant: %v", err)
		}
	}
	// Legacy queued commands remain available to a peer with no new report.
	deployment.state.Inbounds[0].Settings = desiredstate.ProtocolSettings{}
	session = &settingsContractSession{}
	check(true)
}

func TestFutureSettingsReportsKeepResourceBounds(t *testing.T) {
	for _, combination := range []*pb.SettingsCombination{nil, {Protocol: strings.Repeat("x", 65)}} {
		if validateSettingsContract(2, []*pb.SettingsCombination{combination}) == nil {
			t.Fatal("unbounded future report accepted")
		}
	}
	if validateSettingsContract(2, make([]*pb.SettingsCombination, maxSettingsCombinations+1)) == nil {
		t.Fatal("too many future combinations accepted")
	}
}
