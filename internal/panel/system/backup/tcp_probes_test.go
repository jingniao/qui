package backup

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestTCPProbeBackupValidationAndInventory(t *testing.T) {
	source, _, _ := sourceFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	c := &pb.TCPProbeCommand{Id: "probe", Addresses: []string{"1.1.1.1"}, Port: 443, Attempts: 1, TimeoutMs: 100, Deadline: timestamppb.New(now.Add(time.Minute))}
	raw, _ := json.Marshal(c)
	if _, err := source.Database.Exec("INSERT INTO probe_targets VALUES('target','label','1.1.1.1',443,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Database.Exec("INSERT INTO node_tcp_probes SELECT 'probe',id,'target',?,NULL,?,?,'queued' FROM nodes LIMIT 1", string(raw), now.Format(time.RFC3339Nano), c.Deadline.AsTime().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := validateTCPProbes(ctx, source.Database); err != nil {
		t.Fatal(err)
	}
	inventory, err := ReadInventory(ctx, source.Database)
	if err != nil || inventory.Totals.ProbeTargets != 1 || inventory.Totals.TCPProbes != 1 {
		t.Fatal(inventory.Totals, err)
	}
	if _, err = source.Database.Exec("UPDATE probe_targets SET host='127.0.0.1'"); err != nil {
		t.Fatal(err)
	}
	if validateTCPProbes(ctx, source.Database) == nil {
		t.Fatal("non-public target without flag accepted")
	}
}
