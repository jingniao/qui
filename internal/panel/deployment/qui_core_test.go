package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

func TestQuiCoreRequiresAdvertisedCapability(t *testing.T) {
	if !supportsQuiCore(&pb.Capabilities{CoreName: "sing-box", SingboxUserStats: true}) {
		t.Fatal("advertised qui source not offered")
	}
	for _, caps := range []*pb.Capabilities{nil, {}, {CoreName: "sing-box", CoreVersion: "1.14.4"}} {
		if supportsQuiCore(caps) {
			t.Fatal("qui source offered without the Phase 9 capability")
		}
	}
}

func TestQueueQuiCoreAcceptsAdvertisedCapability(t *testing.T) {
	r, audit, nodeID, revision := coreNodeFixture(t, "linux/amd64", "sing-box")
	// Repository reads protobuf JSON, as persisted by the control connection.
	caps, err := json.Marshal(&pb.Capabilities{CoreName: "sing-box", CoreVersion: "1.14.4", SingboxUserStats: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(caps), nodeID); err != nil {
		t.Fatal(err)
	}
	input := CoreUpdateInput{Component: "sing-box", Version: "1.14.4", URL: "https://github.com/jingniao/qui/releases/download/v1.1.0/sing-box-1.14.4-qui.1-linux-amd64.tar.gz", SHA256: strings.Repeat("ab", 32)}
	if _, err := r.QueueCoreUpdate(context.Background(), audit, nodeID, "owner", "local", revision.ID, input); err != nil {
		t.Fatal(err)
	}
}

func TestQueueQuiCoreRefusesAgentWithoutCapability(t *testing.T) {
	r, audit, nodeID, revision := coreNodeFixture(t, "linux/amd64", "sing-box")
	input := CoreUpdateInput{Component: "sing-box", Version: "1.14.4", URL: "https://github.com/jingniao/qui/releases/download/v1.1.0/sing-box-1.14.4-qui.1-linux-amd64.tar.gz", SHA256: strings.Repeat("ab", 32)}
	if err := ValidateCoreUpdateTarget(input.Component, input.Version, input.URL, input.SHA256, "linux/amd64"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.QueueCoreUpdate(context.Background(), audit, nodeID, "owner", "local", revision.ID, input); !errors.Is(err, release.ErrPolicy) {
		t.Fatalf("qui source reached an old Agent: %v", err)
	}
	var count int
	if err := r.Database.QueryRow("SELECT count(*) FROM core_update_tasks").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected source created task: %d, %v", count, err)
	}
	input.URL = "https://github.com/SagerNet/sing-box/releases/download/v1.14.4/sing-box-1.14.4-linux-amd64.tar.gz"
	if _, err := r.QueueCoreUpdate(context.Background(), audit, nodeID, "owner", "local", revision.ID, input); err != nil {
		t.Fatalf("official source regressed: %v", err)
	}
}
