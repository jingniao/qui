package deployment

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestOptionalNodeAgentVersion(t *testing.T) {
	r, _, n, _ := coreNodeFixture(t, "linux/amd64", "xray")
	for _, hasUser := range []bool{false, true} {
		c := &pb.Capabilities{CoreOptionalSupported: true, MultiCoreSupported: true}
		if hasUser {
			c.CoreName = "xray"
			c.Cores = []*pb.CoreCapability{{Name: "xray"}}
		}
		raw, err := protojson.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), n); err != nil {
			t.Fatal(err)
		}
		for _, version := range []string{"1.8.0", "1.8.9", "1.9.0", "1.9.0-rc.1"} {
			err := requireAgentVersion(context.Background(), r.Database, n, version)
			blocked := !hasUser && version[:3] == "1.8"
			if blocked && !errors.Is(err, ErrAgentVersionUnsupported) || !blocked && err != nil {
				t.Fatalf("user=%v version=%s: %v", hasUser, version, err)
			}
		}
	}
}

func TestOptionalLastCoreDependenciesAndQueue(t *testing.T) {
	r, audit, n, rev := coreNodeFixture(t, "linux/amd64", "xray")
	setMultiCore(t, r, n, false)
	ctx := context.Background()
	_, err := r.QueueRemoveCore(ctx, audit, n, "xray", "owner", "", rev.ID)
	var conflict *CoreRemovalConflict
	if !errors.As(err, &conflict) || conflict.Code != "last_core" {
		t.Fatal(err)
	}
	c := &pb.Capabilities{CoreName: "xray", MultiCoreSupported: true, CoreOptionalSupported: true, Cores: []*pb.CoreCapability{{Name: "xray"}}}
	raw, err := protojson.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), n); err != nil {
		t.Fatal(err)
	}
	// A disabled draft outbound still blocks removal; the direct outbound in a
	// published base revision is not a user-owned dependency.
	sealed, err := r.Vault.Seal([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("INSERT INTO outbounds(id,node_id,name,protocol,address,port,enabled,encrypted_settings,support_tcp,support_udp,failure_policy,created_at) VALUES('optional_blocker',?,'blocker','socks5','example.com',1080,0,?,1,0,'Block','2025-01-01T00:00:00Z')", n, sealed); err != nil {
		t.Fatal(err)
	}
	_, err = r.QueueRemoveCore(ctx, audit, n, "xray", "owner", "", rev.ID)
	if !errors.As(err, &conflict) || conflict.Code != "user_core_in_use" || !reflect.DeepEqual(conflict.OutboundIDs, []string{"optional_blocker"}) {
		t.Fatal(conflict, err)
	}
	if _, err := r.Database.Exec("DELETE FROM outbounds WHERE id='optional_blocker'"); err != nil {
		t.Fatal(err)
	}
	task, err := r.QueueRemoveCore(ctx, audit, n, "xray", "owner", "", rev.ID)
	if err != nil || task.Status != "queued" {
		t.Fatal(task, err)
	}
}
