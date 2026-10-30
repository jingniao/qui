package node

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLatencyEffectiveTargetsAndSlowQueue(t *testing.T) {
	if LatencyInterval != 60*time.Second {
		t.Fatal(LatencyInterval)
	}
	r, id := latencyFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := r.Database.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		exec("INSERT INTO probe_targets(id,label,host,port,scheduled) VALUES(?,?,'1.1.1.1',443,1)", fmt.Sprint(i), fmt.Sprint(i))
	}
	if err := r.scheduleLatency(ctx, now); err != nil {
		t.Fatal(err)
	}
	c, err := r.pendingTCPProbe(ctx, id)
	if err != nil || c == nil || !c.Deadline.AsTime().Equal(now.Add(2*time.Minute)) {
		t.Fatal(c, err)
	}
	for _, seconds := range []int{20, 40, 60, 80, 100} {
		tick := now.Add(time.Duration(seconds) * time.Second)
		exec("UPDATE nodes SET last_heartbeat=? WHERE id=?", timestamp(tick), id)
		if err = r.scheduleLatency(ctx, tick); err != nil {
			t.Fatal(err)
		}
		if n := probeCount(t, r, "SELECT count(*) FROM node_tcp_probes WHERE status='queued'"); n != 5 {
			t.Fatal("slow queue grew", seconds, n)
		}
	}
	exec("DELETE FROM node_tcp_probes")
	exec("UPDATE nodes SET latency_targets_custom=1 WHERE id=?", id)
	exec("INSERT INTO node_latency_targets(node_id,target_id) VALUES(?,'manual')", id)
	if err = r.scheduleLatency(ctx, now); err != nil {
		t.Fatal(err)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_tcp_probes WHERE target_id='manual'"); n != 1 {
		t.Fatal(n)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_tcp_probes"); n != 1 {
		t.Fatal(n)
	}
	exec("DELETE FROM probe_targets WHERE id='manual'")
	if n := probeCount(t, r, "SELECT count(*) FROM node_latency_targets"); n != 0 {
		t.Fatal("cascade", n)
	}
	exec("DELETE FROM node_tcp_probes")
	if err = r.scheduleLatency(ctx, now); err != nil {
		t.Fatal(err)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_tcp_probes"); n != 0 {
		t.Fatal("custom empty used defaults", n)
	}
}

func TestNodeLatencyMigrationCapsInsertAndMove(t *testing.T) {
	r, id := latencyFixture(t)
	other, _, _, err := r.CreateEnrollment(context.Background(), "other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		target := fmt.Sprint(i)
		if _, err = r.Database.Exec("INSERT INTO probe_targets(id,label,host,port) VALUES(?,?,'1.1.1.1',443)", target, target); err != nil {
			t.Fatal(err)
		}
		node := id
		if i == 5 {
			node = other.ID
		}
		if _, err = r.Database.Exec("INSERT INTO node_latency_targets VALUES(?,?)", node, target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = r.Database.Exec("INSERT INTO node_latency_targets VALUES(?,'5')", id); err == nil {
		t.Fatal("sixth insert accepted")
	}
	if _, err = r.Database.Exec("UPDATE node_latency_targets SET node_id=? WHERE node_id=?", id, other.ID); err == nil {
		t.Fatal("sixth move accepted")
	}
	if _, err = r.Database.Exec("UPDATE nodes SET latency_targets_custom=2 WHERE id=?", id); err == nil {
		t.Fatal("invalid flag accepted")
	}
	if _, err = r.Database.Exec("DELETE FROM nodes WHERE id=?", other.ID); err != nil {
		t.Fatal(err)
	}
	if n := probeCount(t, r, "SELECT count(*) FROM node_latency_targets WHERE node_id=?", other.ID); n != 0 {
		t.Fatal(n)
	}
}
