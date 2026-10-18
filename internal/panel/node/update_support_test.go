package node

import (
	"context"
	"google.golang.org/protobuf/proto"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestAgentRecoveryCapabilityReconnectionClearsOldGrant(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(ctx, "recovery", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CoreUpdateSupport(ctx, n.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ update, recovery, want bool }{{true, true, true}, {true, false, false}, {false, true, false}, {true, true, true}} {
		if err := r.AgentUpdateSupport(ctx, n.ID, step.update, step.recovery); err != nil {
			t.Fatal(err)
		}
		var core, update, recovery bool
		if err := database.QueryRow("SELECT core_supported,agent_supported,agent_recovery_supported FROM node_update_capabilities WHERE node_id=?", n.ID).Scan(&core, &update, &recovery); err != nil {
			t.Fatal(err)
		}
		if !core || update != step.update || recovery != step.want {
			t.Fatal("capability isolation or downgrade failed", core, update, recovery)
		}
	}
	if err := r.AgentUpdateSupport(ctx, n.ID, true); err != nil {
		t.Fatal(err)
	}
	var recovery bool
	if err := database.QueryRow("SELECT agent_recovery_supported FROM node_update_capabilities WHERE node_id=?", n.ID).Scan(&recovery); err != nil || recovery {
		t.Fatal("old peer retained recovery permission", err)
	}
	data, err := proto.Marshal(&pb.Hello{AgentUpdateSupported: true})
	if err != nil {
		t.Fatal(err)
	}
	var hello pb.Hello
	if proto.Unmarshal(data, &hello) != nil || hello.AgentRecoverySupported {
		t.Fatal("missing Hello field grants recovery")
	}
	data, err = proto.Marshal(&pb.Welcome{AgentUpdateSupported: true})
	if err != nil {
		t.Fatal(err)
	}
	var welcome pb.Welcome
	if proto.Unmarshal(data, &welcome) != nil || welcome.AgentRecoverySupported {
		t.Fatal("missing Welcome field grants recovery")
	}
}
