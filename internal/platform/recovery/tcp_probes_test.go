package recovery

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestRestoreTCPProbeDefinitionsExpiresRequests(t *testing.T) {
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		now := time.Now().UTC()
		c := &pb.TCPProbeCommand{Id: "probe", Addresses: []string{"1.1.1.1"}, Port: 443, Attempts: 1, TimeoutMs: 100, Deadline: timestamppb.New(now.Add(time.Minute))}
		raw, _ := json.Marshal(c)
		if _, err := f.source.Database.Exec("INSERT INTO probe_targets VALUES('target','saved','1.1.1.1',443,0)"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("INSERT INTO node_tcp_probes VALUES('probe',?,'target',?,NULL,?,?,'queued')", f.node.ID, string(raw), now.Format(time.RFC3339Nano), c.Deadline.AsTime().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	})
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := db.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT count(*) FROM probe_targets", 1)
	assertInt(t, restored, "SELECT count(*) FROM node_tcp_probes WHERE status='expired'", 1)
	assertInt(t, restored, "SELECT count(*) FROM nodes WHERE tcp_probe_supported<>0", 0)
}
