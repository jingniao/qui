package node

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func machineLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestMachinePeriodAnchors(t *testing.T) {
	shanghai := machineLocation(t, "Asia/Shanghai")
	at := func(year int, month time.Month, day, hour int) time.Time {
		return time.Date(year, month, day, hour, 0, 0, 0, shanghai)
	}
	for _, tc := range []struct {
		name     string
		resetDay int
		now      time.Time
		start    time.Time
		end      time.Time
	}{
		{"before the reset day", 15, at(2026, 11, 10, 12), at(2026, 10, 15, 0), at(2026, 11, 15, 0)},
		{"exact reset instant", 15, at(2026, 11, 15, 0), at(2026, 11, 15, 0), at(2026, 12, 15, 0)},
		{"one nanosecond before", 15, at(2026, 11, 15, 0).Add(-time.Nanosecond), at(2026, 10, 15, 0), at(2026, 11, 15, 0)},
		{"short month clamps forward", 31, at(2026, 2, 10, 12), at(2026, 1, 31, 0), at(2026, 2, 28, 0)},
		{"clamped anchor is not reused", 31, at(2026, 3, 5, 12), at(2026, 2, 28, 0), at(2026, 3, 31, 0)},
		{"leap february", 30, at(2024, 2, 29, 12), at(2024, 2, 29, 0), at(2024, 3, 30, 0)},
		{"reset 29 in a short february", 29, at(2026, 3, 1, 12), at(2026, 2, 28, 0), at(2026, 3, 29, 0)},
		{"year rollover", 15, at(2026, 1, 5, 12), at(2025, 12, 15, 0), at(2026, 1, 15, 0)},
		{"december to january", 1, at(2026, 12, 31, 23), at(2026, 12, 1, 0), at(2027, 1, 1, 0)},
	} {
		start, end := machinePeriod(tc.resetDay, tc.now, shanghai)
		if !start.Equal(tc.start) || !end.Equal(tc.end) {
			t.Fatalf("%s: %s .. %s", tc.name, start, end)
		}
	}
}

func TestMachineDayShares(t *testing.T) {
	shanghai := machineLocation(t, "Asia/Shanghai")
	newYork := machineLocation(t, "America/New_York")
	at := func(loc *time.Location, day int, hour int) time.Time {
		return time.Date(2026, 11, day, hour, 0, 0, 0, loc)
	}
	for _, tc := range []struct {
		name string
		loc  *time.Location
		from time.Time
		to   time.Time
		// delta is one direction; want is the expected share per local day.
		delta uint64
		want  map[string]uint64
	}{
		{"inside one day", shanghai, at(shanghai, 10, 20), at(shanghai, 10, 21), 1000, map[string]uint64{"2026-11-10": 1000}},
		{"ending exactly at midnight", shanghai, at(shanghai, 10, 22), at(shanghai, 11, 0), 1000, map[string]uint64{"2026-11-10": 1000}},
		{"measured zero still has a day", shanghai, at(shanghai, 10, 20), at(shanghai, 10, 21), 0, map[string]uint64{"2026-11-10": 0}},
		{"48 hour gap", shanghai, at(shanghai, 10, 12), at(shanghai, 12, 12), 1000, map[string]uint64{"2026-11-10": 250, "2026-11-11": 500, "2026-11-12": 250}},
		{"remainder goes to the last day", shanghai, at(shanghai, 10, 12), at(shanghai, 12, 12), 1001, map[string]uint64{"2026-11-10": 250, "2026-11-11": 500, "2026-11-12": 251}},
		{"a day the clock skips", newYork, time.Date(2026, 3, 7, 0, 0, 0, 0, newYork), time.Date(2026, 3, 9, 0, 0, 0, 0, newYork), 4700, map[string]uint64{"2026-03-07": 2400, "2026-03-08": 2300}},
		{"a day the clock repeats", newYork, time.Date(2026, 10, 31, 0, 0, 0, 0, newYork), time.Date(2026, 11, 2, 0, 0, 0, 0, newYork), 4900, map[string]uint64{"2026-10-31": 2400, "2026-11-01": 2500}},
	} {
		shares := machineDayShares(tc.delta, tc.from, tc.to, tc.loc)
		if len(shares) != len(tc.want) {
			t.Fatalf("%s: %v", tc.name, shares)
		}
		var total uint64
		for day, share := range shares {
			if share != tc.want[day] {
				t.Fatalf("%s: %s = %d, want %d (%v)", tc.name, day, share, tc.want[day], shares)
			}
			total += share
		}
		if total != tc.delta {
			t.Fatalf("%s: the shares add up to %d, want %d", tc.name, total, tc.delta)
		}
	}
	if shares := machineDayShares(10, at(shanghai, 10, 12), at(shanghai, 10, 12), shanghai); shares != nil {
		t.Fatalf("an empty interval produced days: %v", shares)
	}
}

// storeReported writes the heartbeat the panel keeps for a node.
func storeReported(t *testing.T, r *Repository, id string, at time.Time, heartbeat *pb.Heartbeat) {
	t.Helper()
	data, err := protojson.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec("UPDATE nodes SET last_heartbeat=?,heartbeat_json=? WHERE id=?", timestamp(at), string(data), id); err != nil {
		t.Fatal(err)
	}
}

func storeMachineDay(t *testing.T, r *Repository, id, iface, day string, rx, tx int64) {
	t.Helper()
	if _, err := r.Database.Exec("INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES(?,?,?,?,?) ON CONFLICT(node_id,interface_name,local_date) DO UPDATE SET rx_bytes=excluded.rx_bytes,tx_bytes=excluded.tx_bytes", id, iface, day, rx, tx); err != nil {
		t.Fatal(err)
	}
}

func storeMachineSettings(t *testing.T, r *Repository, id string, settings MachineTrafficSettings) {
	t.Helper()
	tx, err := r.Database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := saveMachineTrafficSettingsTx(context.Background(), tx, id, settings); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func machineSummary(t *testing.T, r *Repository, id string, now time.Time) MachineTrafficSummary {
	t.Helper()
	items, _, err := r.machineTrafficSummaries(context.Background(), id, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d summaries for one node", len(items))
	}
	return items[0]
}

// reportedHeartbeat is the host report of a live server.
func reportedHeartbeat(boot string, interfaces ...string) *pb.Heartbeat {
	heartbeat := &pb.Heartbeat{}
	if boot != "" {
		heartbeat.BootId = proto.String(boot)
	}
	heartbeat.DefaultRouteInterface = proto.String("eth0")
	for _, name := range interfaces {
		heartbeat.Interfaces = append(heartbeat.Interfaces, &pb.InterfaceCounters{Name: name})
	}
	return heartbeat
}

func TestMachineSummaryMeasuredStaleAndUnknown(t *testing.T) {
	now := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)
	r, id := machineFixture(t)
	// A server that never reported: no interface, no rows, no cursor.
	summary := machineSummary(t, r, id, now)
	if summary.Status != machineStatusUnknown || summary.InterfaceName != nil || summary.RxBytes != nil || summary.TxBytes != nil || summary.UsedBytes != nil || summary.LastObservedAt != nil {
		t.Fatalf("empty node: %+v", summary)
	}
	if summary.PeriodStart != "2026-11-01T00:00:00+08:00" || summary.PeriodEnd != "2026-12-01T00:00:00+08:00" || summary.NextResetAt != summary.PeriodEnd {
		t.Fatalf("period: %+v", summary)
	}
	// A fresh report of the resolved interface with period rows is measured.
	storeReported(t, r, id, now.Add(-5*time.Second), reportedHeartbeat(machineTestBoot, "eth0", "eth1"))
	storeMachineDay(t, r, id, "eth0", "2026-11-01", 100, 10)
	storeMachineDay(t, r, id, "eth0", "2026-11-09", 200, 20)
	storeMachineDay(t, r, id, "eth1", "2026-11-09", 999, 999)
	summary = machineSummary(t, r, id, now)
	if summary.Status != machineStatusMeasured || summary.InterfaceName == nil || *summary.InterfaceName != "eth0" {
		t.Fatalf("automatic selection: %+v", summary)
	}
	// Only the selected interface is summed; interfaces are never stitched.
	if summary.RxBytes == nil || *summary.RxBytes != "300" || summary.TxBytes == nil || *summary.TxBytes != "30" || summary.UsedBytes == nil || *summary.UsedBytes != "330" {
		t.Fatalf("automatic sums: %+v", summary)
	}
	// An explicit selection recomputes the whole period from that interface.
	storeMachineSettings(t, r, id, MachineTrafficSettings{Method: machineMethodOutbound, InterfaceName: "eth1", ResetDay: 1})
	summary = machineSummary(t, r, id, now)
	if summary.InterfaceName == nil || *summary.InterfaceName != "eth1" || summary.RxBytes == nil || *summary.RxBytes != "999" || summary.UsedBytes == nil || *summary.UsedBytes != "999" {
		t.Fatalf("explicit selection: %+v", summary)
	}
	if summary.Settings.InterfaceName != "eth1" || summary.Settings.AllowanceBytes != nil || summary.Settings.ResetDay != 1 {
		t.Fatalf("settings echo: %+v", summary.Settings)
	}
}

func TestMachineSummaryStalenessAndIdentity(t *testing.T) {
	now := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		reported  time.Time
		heartbeat *pb.Heartbeat
		want      string
	}{
		{"fresh same boot", now.Add(-5 * time.Second), reportedHeartbeat(machineTestBoot, "eth0"), machineStatusMeasured},
		{"heartbeat too old", now.Add(-2 * time.Minute), reportedHeartbeat(machineTestBoot, "eth0"), machineStatusStale},
		{"missing boot id", now.Add(-5 * time.Second), reportedHeartbeat("", "eth0"), machineStatusStale},
		{"interface not reported", now.Add(-5 * time.Second), reportedHeartbeat(machineTestBoot, "eth1"), machineStatusStale},
	} {
		r, id := machineFixture(t)
		storeReported(t, r, id, tc.reported, tc.heartbeat)
		storeMachineDay(t, r, id, "eth0", "2026-11-05", 5, 6)
		summary := machineSummary(t, r, id, now)
		if summary.Status != tc.want || summary.InterfaceName == nil || *summary.InterfaceName != "eth0" {
			t.Fatalf("%s: %+v", tc.name, summary)
		}
		// Known rows stay visible even when the current measurement is not.
		if summary.RxBytes == nil || *summary.RxBytes != "5" || summary.UsedBytes == nil || *summary.UsedBytes != "11" {
			t.Fatalf("%s totals: %+v", tc.name, summary)
		}
	}
}

func TestMachineSummaryWithoutRouteIsUnknown(t *testing.T) {
	now := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)
	r, id := machineFixture(t)
	// The host reported an interface list but no IPv4 default route, so nothing
	// may be guessed or selected by traffic volume.
	heartbeat := reportedHeartbeat(machineTestBoot, "eth0")
	heartbeat.DefaultRouteInterface = nil
	storeReported(t, r, id, now.Add(-5*time.Second), heartbeat)
	storeMachineDay(t, r, id, "eth0", "2026-11-05", 5, 6)
	summary := machineSummary(t, r, id, now)
	if summary.Status != machineStatusUnknown || summary.InterfaceName != nil || summary.UsedBytes != nil {
		t.Fatalf("no route: %+v", summary)
	}
}

func TestMachineSummaryMethodsAndResetSlices(t *testing.T) {
	now := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)
	r, id := machineFixture(t)
	storeReported(t, r, id, now.Add(-5*time.Second), reportedHeartbeat(machineTestBoot, "eth0"))
	// Opposite peaks per day: the method applies to the period sums, not to the
	// daily maxima.
	storeMachineDay(t, r, id, "eth0", "2026-11-03", 100, 10)
	storeMachineDay(t, r, id, "eth0", "2026-11-04", 10, 100)
	for _, tc := range []struct{ method, used string }{
		{machineMethodSum, "220"},
		{machineMethodOutbound, "110"},
		{machineMethodMax, "110"},
	} {
		storeMachineSettings(t, r, id, MachineTrafficSettings{Method: tc.method, ResetDay: 1})
		if summary := machineSummary(t, r, id, now); summary.UsedBytes == nil || *summary.UsedBytes != tc.used {
			t.Fatalf("%s: %+v", tc.method, summary)
		}
	}
	// A reset day moves the period; the retained rows are never deleted or
	// rewritten by a settings change.
	storeMachineSettings(t, r, id, MachineTrafficSettings{Method: machineMethodSum, ResetDay: 4})
	summary := machineSummary(t, r, id, now)
	if summary.PeriodStart != "2026-11-04T00:00:00+08:00" || summary.RxBytes == nil || *summary.RxBytes != "10" || *summary.TxBytes != "100" {
		t.Fatalf("reset day: %+v", summary)
	}
	if rows := machineCount(t, r, "SELECT count(*) FROM node_machine_traffic_daily"); rows != 2 {
		t.Fatalf("a settings change deleted retained rows: %d", rows)
	}
}

func TestMachineSettingsDefaultsAndValidation(t *testing.T) {
	r, id := machineFixture(t)
	settings, err := r.MachineTrafficSettings(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Method != machineMethodSum || settings.InterfaceName != "" || settings.AllowanceBytes != nil || settings.ResetDay != 1 {
		t.Fatalf("defaults: %+v", settings)
	}
	// Reading the defaults never writes them.
	if rows := machineCount(t, r, "SELECT count(*) FROM node_machine_traffic_settings"); rows != 0 {
		t.Fatalf("GET wrote %d settings rows", rows)
	}
	allowance := int64(1 << 40)
	saved := MachineTrafficSettings{Method: machineMethodMax, InterfaceName: "eth1", AllowanceBytes: &allowance, ResetDay: 31}
	storeMachineSettings(t, r, id, saved)
	stored, err := r.MachineTrafficSettings(context.Background(), id)
	if err != nil || stored.Method != machineMethodMax || stored.InterfaceName != "eth1" || stored.AllowanceBytes == nil || *stored.AllowanceBytes != allowance || stored.ResetDay != 31 {
		t.Fatalf("stored settings: %+v %v", stored, err)
	}
	// The editor sends the whole object again; clearing the allowance replaces it.
	storeMachineSettings(t, r, id, MachineTrafficSettings{Method: machineMethodSum, ResetDay: 1})
	stored, err = r.MachineTrafficSettings(context.Background(), id)
	if err != nil || stored.AllowanceBytes != nil || stored.InterfaceName != "" {
		t.Fatalf("replacement left stale settings: %+v %v", stored, err)
	}
	tooBig, zero := int64(1)<<53, int64(0)
	for _, tc := range []struct {
		name     string
		settings MachineTrafficSettings
	}{
		{"unknown method", MachineTrafficSettings{Method: "maximum", ResetDay: 1}},
		{"loopback interface", MachineTrafficSettings{Method: machineMethodSum, InterfaceName: "lo", ResetDay: 1}},
		{"interface with colon", MachineTrafficSettings{Method: machineMethodSum, InterfaceName: "eth0:1", ResetDay: 1}},
		{"interface too long", MachineTrafficSettings{Method: machineMethodSum, InterfaceName: "0123456789abcdef", ResetDay: 1}},
		{"interface with space", MachineTrafficSettings{Method: machineMethodSum, InterfaceName: "eth0 1", ResetDay: 1}},
		{"zero allowance", MachineTrafficSettings{Method: machineMethodSum, AllowanceBytes: &zero, ResetDay: 1}},
		{"allowance above the exact range", MachineTrafficSettings{Method: machineMethodSum, AllowanceBytes: &tooBig, ResetDay: 1}},
		{"reset day zero", MachineTrafficSettings{Method: machineMethodSum}},
		{"reset day above 31", MachineTrafficSettings{Method: machineMethodSum, ResetDay: 32}},
	} {
		if ValidateMachineTrafficSettings(tc.settings) == nil {
			t.Fatalf("%s was accepted", tc.name)
		}
	}
	if allowance != 1<<40 || ValidateMachineTrafficSettings(MachineTrafficSettings{Method: machineMethodMax, InterfaceName: "eth1", AllowanceBytes: &allowance, ResetDay: 31}) != nil {
		t.Fatal("valid settings were rejected")
	}
	if !ValidMachineTrafficInterface("", true) || ValidMachineTrafficInterface("", false) {
		t.Fatal("the empty interface must be allowed only for settings")
	}
	if !ValidBootID(machineTestBoot) || ValidBootID("1c0b5a6e7f6b4f2f9c3a1b2c3d4e5f60") || ValidBootID("") {
		t.Fatal("boot id validation")
	}
	if !ValidMachineDay("2026-02-28") || ValidMachineDay("2026-2-8") || ValidMachineDay("2026-02-30") {
		t.Fatal("day validation")
	}
}

func TestMachineBatchMatchesSingleAndSkipsDeleted(t *testing.T) {
	now := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)
	r, first := machineFixture(t)
	second, _, _, err := r.CreateEnrollment(context.Background(), "second", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first, second.ID} {
		storeReported(t, r, id, now.Add(-5*time.Second), reportedHeartbeat(machineTestBoot, "eth0"))
		storeMachineDay(t, r, id, "eth0", "2026-11-05", 7, 8)
	}
	items, zone, err := r.machineTrafficSummaries(context.Background(), "", now)
	if err != nil || zone != "Asia/Shanghai" || len(items) != 2 {
		t.Fatalf("batch: %v %q %v", items, zone, err)
	}
	for _, item := range items {
		single, _, err := r.machineTrafficSummaries(context.Background(), item.NodeID, now)
		if err != nil || len(single) != 1 || single[0].UsedBytes == nil || *single[0].UsedBytes != *item.UsedBytes {
			t.Fatalf("single differs from batch: %+v %+v %v", single, item, err)
		}
	}
	// A logically deleted node keeps its rows but disappears from both.
	if _, err := r.Database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", timestamp(now), second.ID); err != nil {
		t.Fatal(err)
	}
	items, _, err = r.machineTrafficSummaries(context.Background(), "", now)
	if err != nil || len(items) != 1 || items[0].NodeID != first {
		t.Fatalf("deleted node in the batch: %v %v", items, err)
	}
	if single, _, err := r.machineTrafficSummaries(context.Background(), second.ID, now); err != nil || len(single) != 0 {
		t.Fatalf("deleted node in the single summary: %v %v", single, err)
	}
	if rows := machineCount(t, r, "SELECT count(*) FROM node_machine_traffic_daily WHERE node_id=?", second.ID); rows != 1 {
		t.Fatalf("a soft delete removed %d daily rows", rows)
	}
}

// In America/Santiago the clock jumps from 00:00 to 01:00 on 2026-09-06, so that
// day has no midnight. The split and the period anchor must start the day at the
// first hour that exists instead of looping on the normalized 23:00.
func TestMachineSkippedMidnight(t *testing.T) {
	santiago := machineLocation(t, "America/Santiago")
	from, to := time.Date(2026, 9, 5, 22, 0, 0, 0, santiago), time.Date(2026, 9, 6, 2, 0, 0, 0, santiago)
	shares := machineDayShares(3000, from, to, santiago)
	if len(shares) != 2 || shares["2026-09-05"] != 2000 || shares["2026-09-06"] != 1000 {
		t.Fatalf("shares %v", shares)
	}
	start, _ := machinePeriod(6, time.Date(2026, 9, 10, 12, 0, 0, 0, santiago), santiago)
	if want := time.Date(2026, 9, 6, 4, 0, 0, 0, time.UTC); !start.Equal(want) {
		t.Fatalf("period start %s, want %s", start.UTC(), want)
	}
}
