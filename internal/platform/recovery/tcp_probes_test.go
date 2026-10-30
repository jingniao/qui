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
		if _, err := f.source.Database.Exec("INSERT INTO probe_targets(id,label,host,port,allow_non_public) VALUES('target','saved','1.1.1.1',443,0)"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,result_json,created_at,deadline,status) VALUES('probe',?,'target',?,NULL,?,?,'queued')", f.node.ID, string(raw), now.Format(time.RFC3339Nano), c.Deadline.AsTime().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("UPDATE probe_targets SET scheduled=1"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.source.Database.Exec("INSERT INTO node_latency_samples(node_id,target_id,at,sent,failed) VALUES(?,'target',?,3,3)", f.node.ID, now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		c.Id = "scheduled-probe"
		raw, _ = json.Marshal(c)
		if _, err := f.source.Database.Exec("INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,created_at,deadline,status,source) VALUES(?,?,'target',?,?,?,'queued','scheduled')", c.Id, f.node.ID, string(raw), now.Format(time.RFC3339Nano), c.Deadline.AsTime().Format(time.RFC3339Nano)); err != nil {
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
	assertInt(t, restored, "SELECT count(*) FROM probe_targets WHERE scheduled=1", 1)
	assertInt(t, restored, "SELECT count(*) FROM node_latency_samples", 0)
	assertInt(t, restored, "SELECT count(*) FROM node_tcp_probes WHERE source='scheduled' OR status='queued'", 0)
	assertInt(t, restored, "SELECT count(*) FROM node_tcp_probes WHERE status='expired'", 1)
	assertInt(t, restored, "SELECT count(*) FROM nodes WHERE tcp_probe_supported<>0", 0)
}
