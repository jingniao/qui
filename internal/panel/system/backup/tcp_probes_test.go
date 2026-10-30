package backup

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"path/filepath"
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
	if _, err := source.Database.Exec("INSERT INTO probe_targets(id,label,host,port,allow_non_public) VALUES('target','label','1.1.1.1',443,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Database.Exec("INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,result_json,created_at,deadline,status) SELECT 'probe',id,'target',?,NULL,?,?,'queued' FROM nodes LIMIT 1", string(raw), now.Format(time.RFC3339Nano), c.Deadline.AsTime().Format(time.RFC3339Nano)); err != nil {
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

func TestLatencyTelemetryExcludedFromVerifiedBackup(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	c := &pb.TCPProbeCommand{Id: "scheduled", Addresses: []string{"1.1.1.1"}, Port: 443, Attempts: 3, TimeoutMs: 3000, Deadline: timestamppb.New(now.Add(2 * time.Minute))}
	raw, _ := json.Marshal(c)
	for _, q := range []string{
		"INSERT INTO probe_targets(id,label,host,port,scheduled) VALUES('target','latency','1.1.1.1',443,1)",
		"INSERT INTO node_latency_samples(node_id,target_id,at,sent,failed) SELECT id,'target','" + now.Format(time.RFC3339Nano) + "',3,3 FROM nodes LIMIT 1",
	} {
		if _, err := source.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.Database.Exec("INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,created_at,deadline,status,source) SELECT 'scheduled',id,'target',?,?,?,'queued','scheduled' FROM nodes LIMIT 1", string(raw), now.Format(time.RFC3339Nano), c.Deadline.AsTime().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	result, err := source.Create(ctx, directory, "latency_backup_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		inventory, err := ReadInventory(ctx, database)
		if err != nil {
			return err
		}
		if inventory.Totals.LatencySamples != 0 || inventory.Totals.TCPProbes != 0 || inventory.Totals.ProbeTargets != 1 {
			t.Fatal(inventory.Totals)
		}
		var scheduled int
		if err = database.QueryRow("SELECT scheduled FROM probe_targets WHERE id='target'").Scan(&scheduled); err != nil {
			return err
		}
		if scheduled != 1 {
			t.Fatal("lost scheduled setting")
		}
		return validateLatency(ctx, database)
	})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := ReadInventory(ctx, source.Database)
	if err != nil || inventory.Totals.LatencySamples != 1 || inventory.Totals.TCPProbes != 1 {
		t.Fatal("live telemetry modified", inventory.Totals, err)
	}
	// Missing migration material must be rejected even when telemetry is empty.
	if _, err = source.Database.Exec("DROP TABLE node_latency_samples"); err != nil {
		t.Fatal(err)
	}
	if validateLatency(ctx, source.Database) == nil {
		t.Fatal("missing latency table accepted")
	}
}

func TestPreLatencyBackupValidation(t *testing.T) {
	source, _, _ := sourceFixture(t)
	ctx := context.Background()
	for _, q := range []string{"DROP TABLE node_latency_samples", "DROP INDEX node_tcp_probes_inflight", "DROP TRIGGER probe_targets_scheduled_insert", "DROP TRIGGER probe_targets_scheduled_update", "ALTER TABLE node_tcp_probes DROP COLUMN source", "ALTER TABLE node_tcp_probes DROP COLUMN dispatched_at", "ALTER TABLE probe_targets DROP COLUMN scheduled", "DELETE FROM schema_migrations WHERE version='0120_latency_samples.sql'"} {
		if _, err := source.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateLatency(ctx, source.Database); err != nil {
		t.Fatal(err)
	}
	if err := validateTCPProbes(ctx, source.Database); err != nil {
		t.Fatal(err)
	}
}
