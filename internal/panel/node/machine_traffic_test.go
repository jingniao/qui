package node

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"proxycontrol/internal/testutil/dbtest"
	pb "proxycontrol/pkg/contracts/agentpb"
	"sort"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

const machineTestBoot = "1c0b5a6e-7f6b-4f2f-9c3a-1b2c3d4e5f60"

// machineFixture returns a repository with a fixed panel zone and one enrolled
// node.
func machineFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	return machineFixtureInZone(t, "Asia/Shanghai")
}

func machineFixtureInZone(t *testing.T, zone string) (*Repository, string) {
	t.Helper()
	database, err := dbtest.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	r := &Repository{Database: database, Timezone: func(context.Context, *sql.Tx) (string, error) { return zone, nil }}
	n, _, _, err := r.CreateEnrollment(context.Background(), "machine", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return r, n.ID
}

// heartbeatMessage builds one heartbeat message with the given counters.
func heartbeatMessage(id string, sequence uint64, at time.Time, boot string, counters map[string][2]uint64) *pb.AgentMessage {
	h := &pb.Heartbeat{}
	if boot != "" {
		h.BootId = &boot
	}
	names := make([]string, 0, len(counters))
	for name := range counters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		h.Interfaces = append(h.Interfaces, &pb.InterfaceCounters{Name: name, ReceiveBytes: counters[name][0], TransmitBytes: counters[name][1]})
	}
	return &pb.AgentMessage{NodeId: id, Sequence: sequence, MessageId: fmt.Sprintf("hb-%d", sequence), Timestamp: timestamppb.New(at), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: h}}
}

func recordHeartbeat(t *testing.T, r *Repository, id string, sequence uint64, at time.Time, boot string, counters map[string][2]uint64) {
	t.Helper()
	if _, err := r.Record(context.Background(), id, heartbeatMessage(id, sequence, at, boot, counters)); err != nil {
		t.Fatal(err)
	}
}

// machineTotals sums every retained daily row of one interface.
func machineTotals(t *testing.T, r *Repository, id, iface string) (int64, int64, int) {
	t.Helper()
	var rx, tx int64
	var rows int
	if err := r.Database.QueryRow("SELECT coalesce(sum(rx_bytes),0),coalesce(sum(tx_bytes),0),count(*) FROM node_machine_traffic_daily WHERE node_id=? AND interface_name=?", id, iface).Scan(&rx, &tx, &rows); err != nil {
		t.Fatal(err)
	}
	return rx, tx, rows
}

func machineCount(t *testing.T, r *Repository, query string, args ...any) int {
	t.Helper()
	var count int
	if err := r.Database.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// storeCursor writes an interface cursor directly, as an earlier heartbeat
// would have left it.
func storeCursor(t *testing.T, r *Repository, id, iface, boot string, rx, tx int64, at time.Time) {
	t.Helper()
	if _, err := r.Database.Exec("INSERT INTO node_machine_traffic_cursors(node_id,interface_name,boot_id,rx_bytes,tx_bytes,observed_at) VALUES(?,?,?,?,?,?)", id, iface, boot, rx, tx, timestamp(at)); err != nil {
		t.Fatal(err)
	}
}

func TestMachineFirstSampleIsOnlyABaseline(t *testing.T) {
	r, id := machineFixture(t)
	now := time.Now().UTC()
	recordHeartbeat(t, r, id, 1, now, machineTestBoot, map[string][2]uint64{"eth0": {1000, 2000}})
	if rx, tx, rows := machineTotals(t, r, id, "eth0"); rows != 0 || rx != 0 || tx != 0 {
		t.Fatalf("the first sample counted usage since boot: %d %d %d", rx, tx, rows)
	}
	var boot string
	var rx int64
	if err := r.Database.QueryRow("SELECT boot_id,rx_bytes FROM node_machine_traffic_cursors WHERE node_id=? AND interface_name='eth0'", id).Scan(&boot, &rx); err != nil || boot != machineTestBoot || rx != 1000 {
		t.Fatalf("baseline cursor: %q %d %v", boot, rx, err)
	}
	// A newly discovered interface on an already known node is baselined too.
	recordHeartbeat(t, r, id, 2, now.Add(5*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {1000, 2000}, "eth1": {7, 9}})
	if _, _, rows := machineTotals(t, r, id, "eth1"); rows != 0 {
		t.Fatalf("a new interface counted usage since boot: %d", rows)
	}
}

func TestMachineSecondSampleCountsExactDeltas(t *testing.T) {
	r, id := machineFixture(t)
	now := time.Now().UTC()
	recordHeartbeat(t, r, id, 1, now, machineTestBoot, map[string][2]uint64{"eth0": {1000, 2000}, "eth1": {50, 60}})
	recordHeartbeat(t, r, id, 2, now.Add(10*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {1250, 2100}, "eth1": {50, 60}})
	if rx, tx, rows := machineTotals(t, r, id, "eth0"); rx != 250 || tx != 100 || rows == 0 {
		t.Fatalf("eth0: %d %d %d", rx, tx, rows)
	}
	if rx, tx, rows := machineTotals(t, r, id, "eth1"); rx != 0 || tx != 0 || rows == 0 {
		t.Fatalf("a measured zero interval wrote %d %d %d", rx, tx, rows)
	}
	var rx int64
	if err := r.Database.QueryRow("SELECT rx_bytes FROM node_machine_traffic_cursors WHERE node_id=? AND interface_name='eth0'", id).Scan(&rx); err != nil || rx != 1250 {
		t.Fatalf("cursor not advanced: %d %v", rx, err)
	}
}

func TestMachineReplayStaleAndOlderSamples(t *testing.T) {
	r, id := machineFixture(t)
	now := time.Now().UTC()
	recordHeartbeat(t, r, id, 1, now, machineTestBoot, map[string][2]uint64{"eth0": {1000, 1000}})
	recordHeartbeat(t, r, id, 2, now.Add(10*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {1100, 1000}})
	// An interval may cross local midnight and fill two day rows.
	_, _, before := machineTotals(t, r, id, "eth0")
	// The same sequence again is a replay and changes nothing.
	recordHeartbeat(t, r, id, 2, now.Add(10*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {1100, 1000}})
	if rx, _, rows := machineTotals(t, r, id, "eth0"); rows != before || rx != 100 {
		t.Fatalf("a replayed sequence changed the usage: %d %d", rx, rows)
	}
	// An older observation cannot be differenced.
	recordHeartbeat(t, r, id, 3, now.Add(5*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {1200, 1000}})
	if rx, _, _ := machineTotals(t, r, id, "eth0"); rx != 100 {
		t.Fatalf("an older observation was counted: %d", rx)
	}
	// A stale message is not sampled at all.
	stale := heartbeatMessage(id, 4, now.Add(-2*time.Minute), machineTestBoot, map[string][2]uint64{"eth0": {1900, 1000}})
	if _, err := r.Record(context.Background(), id, stale); err != nil {
		t.Fatal(err)
	}
	if rx, _, _ := machineTotals(t, r, id, "eth0"); rx != 100 {
		t.Fatalf("a stale heartbeat was counted: %d", rx)
	}
}

func TestMachineOutageRecoveryAndContinuity(t *testing.T) {
	r, id := machineFixture(t)
	now := time.Now().UTC()
	// A long Agent or panel outage without a reboot: the cumulative counters
	// recover the whole gap.
	storeCursor(t, r, id, "eth0", machineTestBoot, 100, 200, now.Add(-72*time.Hour))
	recordHeartbeat(t, r, id, 1, now, machineTestBoot, map[string][2]uint64{"eth0": {100 + 4096, 200 + 8192}})
	if rx, tx, _ := machineTotals(t, r, id, "eth0"); rx != 4096 || tx != 8192 {
		t.Fatalf("the outage gap was not recovered: %d %d", rx, tx)
	}
	// A reconnect or a panel restart does not disturb the same boot.
	recordHeartbeat(t, r, id, 2, now.Add(20*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {100 + 4096 + 1, 200 + 8192 + 2}})
	if rx, tx, _ := machineTotals(t, r, id, "eth0"); rx != 4097 || tx != 8194 {
		t.Fatalf("continuity after a restart: %d %d", rx, tx)
	}
}

func TestMachineRebaselinesNeverCountAsTraffic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		boot   string
		rx, tx uint64
	}{
		{"changed boot", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", 9000, 9000},
		{"receive decreased", machineTestBoot, 10, 5000},
		{"transmit decreased", machineTestBoot, 5000, 10},
	} {
		r, id := machineFixture(t)
		now := time.Now().UTC()
		recordHeartbeat(t, r, id, 1, now, machineTestBoot, map[string][2]uint64{"eth0": {1000, 2000}})
		recordHeartbeat(t, r, id, 2, now.Add(10*time.Second), tc.boot, map[string][2]uint64{"eth0": {tc.rx, tc.tx}})
		if rx, tx, rows := machineTotals(t, r, id, "eth0"); rows != 0 || rx != 0 || tx != 0 {
			t.Fatalf("%s counted %d %d %d", tc.name, rx, tx, rows)
		}
		var boot string
		var rx int64
		if err := r.Database.QueryRow("SELECT boot_id,rx_bytes FROM node_machine_traffic_cursors WHERE node_id=? AND interface_name='eth0'", id).Scan(&boot, &rx); err != nil || boot != tc.boot || rx != int64(tc.rx) {
			t.Fatalf("%s did not rebaseline: %q %d %v", tc.name, boot, rx, err)
		}
	}
}

func TestMachineMissingIdentityOrInterfacesWritesNothing(t *testing.T) {
	r, id := machineFixture(t)
	now := time.Now().UTC()
	recordHeartbeat(t, r, id, 1, now, "", map[string][2]uint64{"eth0": {1000, 1000}})
	recordHeartbeat(t, r, id, 2, now.Add(10*time.Second), "not-a-boot-id", map[string][2]uint64{"eth0": {2000, 2000}})
	recordHeartbeat(t, r, id, 3, now.Add(20*time.Second), machineTestBoot, nil)
	if rows := machineCount(t, r, "SELECT count(*) FROM node_machine_traffic_daily"); rows != 0 {
		t.Fatalf("an unidentified heartbeat wrote %d days", rows)
	}
	if rows := machineCount(t, r, "SELECT count(*) FROM node_machine_traffic_cursors"); rows != 0 {
		t.Fatalf("an unidentified heartbeat wrote %d cursors", rows)
	}
}

func TestMachineSkipsDuplicateAndOutOfRangeCounters(t *testing.T) {
	r, id := machineFixture(t)
	now := time.Now().UTC()
	first := heartbeatMessage(id, 1, now, machineTestBoot, map[string][2]uint64{"eth0": {1000, 1000}})
	heartbeat := first.GetHeartbeat()
	heartbeat.Interfaces = append(heartbeat.Interfaces,
		&pb.InterfaceCounters{Name: "eth0", ReceiveBytes: 5, TransmitBytes: 5},
		&pb.InterfaceCounters{Name: "lo", ReceiveBytes: 1, TransmitBytes: 1},
		&pb.InterfaceCounters{Name: "huge", ReceiveBytes: math.MaxUint64, TransmitBytes: 1},
	)
	if _, err := r.Record(context.Background(), id, first); err != nil {
		t.Fatal(err)
	}
	// eth0 is ambiguous, lo is loopback and huge cannot be stored as an int64.
	if rows := machineCount(t, r, "SELECT count(*) FROM node_machine_traffic_cursors"); rows != 0 {
		t.Fatalf("unusable counters produced %d cursors", rows)
	}
	second := heartbeatMessage(id, 2, now.Add(10*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {1000, 1000}, "eth1": {10, 10}})
	second.GetHeartbeat().Interfaces = append(second.GetHeartbeat().Interfaces, &pb.InterfaceCounters{Name: "huge", ReceiveBytes: math.MaxUint64, TransmitBytes: 5})
	if _, err := r.Record(context.Background(), id, second); err != nil {
		t.Fatal(err)
	}
	if rows := machineCount(t, r, "SELECT count(*) FROM node_machine_traffic_cursors"); rows != 2 {
		t.Fatalf("other interfaces were dropped with the unusable one: %d", rows)
	}
}

func TestMachineDailyOverflowRollsBackTheHeartbeat(t *testing.T) {
	r, id := machineFixtureInZone(t, "UTC")
	now := time.Now().UTC()
	day := now.In(time.UTC).Format(MachineDayLayout)
	if _, err := r.Database.Exec("INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES(?,?,?,?,0)", id, "eth0", day, int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	storeCursor(t, r, id, "eth0", machineTestBoot, 100, 0, now.Add(-10*time.Second))
	if _, err := r.Record(context.Background(), id, heartbeatMessage(id, 1, now, machineTestBoot, map[string][2]uint64{"eth0": {200, 0}})); err == nil {
		t.Fatal("an overflowing daily row was accepted")
	}
	if rows := machineCount(t, r, "SELECT count(*) FROM nodes WHERE id=? AND last_sequence=0", id); rows != 1 {
		t.Fatal("the heartbeat was acknowledged although its usage was not stored")
	}
	if rx, _, _ := machineTotals(t, r, id, "eth0"); rx != math.MaxInt64 {
		t.Fatalf("the rollback did not restore the row: %d", rx)
	}
}
