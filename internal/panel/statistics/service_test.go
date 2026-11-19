package statistics

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/system"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/testutil/dbtest"
)

func fixture(t *testing.T) (*Service, *sql.DB, string, time.Time) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panel.db")
	writer, err := dbtest.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	n, _, _, err := (&node.Repository{Database: writer}).CreateEnrollment(ctx, "first", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := db.OpenReadPool(ctx, path, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	nodes := &node.Repository{Database: reader, Timezone: (&system.Repository{Database: writer}).ReadTimezoneTx}
	s := New(reader, nodes)
	now := time.Date(2026, 11, 17, 23, 37, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	return s, writer, n.ID, now
}

func TestSharedVersionCutoffLateTrafficAndFailure(t *testing.T) {
	s, writer, id, now := fixture(t)
	ctx := context.Background()
	read := func(id string) View {
		t.Helper()
		v, err := s.Read(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := read(""); v.Status != "pending" || v.Data != nil {
		t.Fatal(v)
	}
	if _, err := writer.Exec(`UPDATE nodes SET heartbeat_json='{"defaultRouteInterface":"eth0"}' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES(?,'eth0','2026-11-18',4,0)`, id); err != nil {
		t.Fatal(err)
	}
	other, _, _, err := (&node.Repository{Database: writer}).CreateEnrollment(ctx, "second", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	add := func(nodeID, epoch string, at time.Time, bytes int) {
		t.Helper()
		if _, err := writer.Exec("INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray',?,?,?,0)", nodeID, epoch, at.Format(time.RFC3339Nano), bytes); err != nil {
			t.Fatal(err)
		}
	}
	add(id, "month", now.Add(-48*time.Hour), 7)
	add(id, "today", now.Add(-time.Hour), 11)
	add(id, "at-cutoff", now, 50)
	add(other.ID, "today", now.Add(-time.Hour), 3)
	s.refresh(ctx)
	all, one := read(""), read(id)
	if all.Status != "ready" || one.Version == 0 || one.Version != all.Version || !all.AsOf.Equal(now) || all.Data.TodayUpload != 14 || all.Data.TrafficObservations != 2 || len(all.Data.Trend) != 1 || all.Data.Trend[0].Upload != 14 || len(all.Data.MachineTraffic) != 2 {
		t.Fatalf("all=%+v data=%+v", all, all.Data)
	}
	if one.Data.TodayUpload != 11 || one.Data.Trend[0].Upload != 11 || len(one.Data.NodeTraffic) != 1 || one.Data.NodeTraffic[0].MonthUpload != 18 || len(one.Data.MachineTraffic) != 1 || one.Data.MachineTraffic[0].NodeID != id {
		t.Fatal(one.Data)
	}
	if _, err := s.Read(ctx, "missing"); !errors.Is(err, node.ErrNotFound) {
		t.Fatal(err)
	}
	add(id, "late", now.Add(-30*time.Minute), 2)
	if v := read(id); v.Version != one.Version || v.Data.TodayUpload != 11 {
		t.Fatal(v)
	}
	s.refresh(ctx)
	if v := read(id); v.Version <= one.Version || v.Data.TodayUpload != 13 {
		t.Fatal(v)
	}
	if _, err := writer.Exec("ALTER TABLE traffic_sample_hours RENAME TO unavailable_hours"); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`UPDATE node_machine_traffic_daily SET rx_bytes=9 WHERE node_id=?`, id); err != nil {
		t.Fatal(err)
	}
	s.refresh(ctx)
	if v := read(id); v.Status != "stale" || !strings.Contains(v.Error, "traffic_sample_hours") || v.Data.TodayUpload != 13 {
		t.Fatal(v)
	}
	if used := read(id).Data.MachineTraffic[0].UsedBytes; used == nil || *used != "4" {
		t.Fatal("proxy failure mixed newer machine data with a previous snapshot", used)
	}
	if _, err := writer.Exec("INSERT INTO node_machine_traffic_settings(node_id,method,interface_name,reset_day) VALUES(?,'max','eth1',7)", id); err != nil {
		t.Fatal(err)
	}
	if v := read(""); v.Data != nil || v.Status != "pending" {
		t.Fatal("old configuration remained visible", v)
	}
	s.refresh(ctx)
	if v := read(""); v.Status != "error" || v.Data != nil || v.Error == "" {
		t.Fatal(v)
	}
}

func TestSharedStatisticsConfigurationDayAndDeletion(t *testing.T) {
	s, writer, id, now := fixture(t)
	ctx := context.Background()
	s.refresh(ctx)
	for _, sqlText := range []string{
		`UPDATE nodes SET heartbeat_json='{"defaultRouteInterface":"eth1"}'`,
		`INSERT INTO system_settings(key,value_json) VALUES('timezone','"America/New_York"') ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`,
		`UPDATE nodes SET deleted_at='2026-11-17T23:37:00Z'`,
	} {
		if _, err := writer.Exec(sqlText); err != nil {
			t.Fatal(err)
		}
		v, err := s.Read(ctx, "")
		if err != nil || v.Status != "pending" || v.Data != nil {
			t.Fatal(sqlText, v, err)
		}
		s.refresh(ctx)
	}
	if _, err := s.Read(ctx, id); !errors.Is(err, node.ErrNotFound) {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return now.Add(24 * time.Hour) }
	if v, err := s.Read(ctx, ""); err != nil || v.Status != "pending" || v.Data != nil {
		t.Fatal(v, err)
	}
}

func TestSharedWorkerWakeAgeAndShutdown(t *testing.T) {
	s, writer, _, now := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	await := func() View {
		t.Helper()
		limit, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		for {
			v, err := s.Read(limit, "")
			if err != nil || v.Error != "" {
				t.Fatal(v, err)
			}
			if v.Status == "ready" {
				return v
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	first := await()
	if !first.GeneratedAt.Equal(now) || *first.AgeSeconds != 0 {
		t.Fatal(first)
	}
	if _, err := writer.Exec(`INSERT INTO system_settings(key,value_json) VALUES('timezone','"Europe/Berlin"') ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`); err != nil {
		t.Fatal(err)
	}
	updated := await()
	if updated.Version <= first.Version || updated.Data.Timezone != "Europe/Berlin" {
		t.Fatal(updated)
	}
	cancel()
	<-done
	if _, err := s.Read(context.Background(), ""); err == nil {
		t.Fatal("stopped worker returned an old snapshot")
	}
	fresh := New(s.pool, s.nodes)
	if v, err := fresh.Read(context.Background(), ""); err != nil || v.Data != nil || v.Version != 0 || v.Status != "pending" {
		t.Fatal("new worker published previous lifecycle data", v, err)
	}
}

func TestSharedStatisticsGenerationAndMeasuredZero(t *testing.T) {
	s, writer, id, now := fixture(t)
	ctx := context.Background()
	generation := func(s *Service) string {
		t.Helper()
		v, err := s.Read(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct{ Generation string }
		if err := json.Unmarshal(b, &envelope); err != nil || envelope.Generation == "" {
			t.Fatal("missing service generation", string(b), err)
		}
		return envelope.Generation
	}
	first := generation(s)
	if first == generation(New(s.pool, s.nodes)) {
		t.Fatal("reopened service reused old generation")
	}
	if _, err := writer.Exec("INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','zero',?,0,0)", id, now.Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	s.refresh(ctx)
	v, err := s.Read(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v.Data.NodeTraffic)
	if err != nil || !strings.Contains(string(b), `"todayMeasured":true`) || first != generation(s) {
		t.Fatal("measured zero and generation", string(b), err)
	}
}

func TestSharedStatisticsConcurrentWriteUsesOneReadTransaction(t *testing.T) {
	s, writer, id, now := fixture(t)
	ctx := context.Background()
	tx, err := db.BeginRead(ctx, s.pool)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	config, err := s.configuration(ctx, tx, now)
	if err != nil {
		t.Fatal(err)
	}
	update, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer update.Rollback()
	if _, err = update.ExecContext(ctx, "INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','new',?,9,0)", id, now.Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = update.ExecContext(ctx, "INSERT INTO node_machine_traffic_settings(node_id,interface_name) VALUES(?,'eth3')", id); err != nil {
		t.Fatal(err)
	}
	if _, err = update.ExecContext(ctx, `INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES(?,'eth3','2026-11-18',2,6),(?,'eth3','2026-11-19',19,23)`, id, id); err != nil {
		t.Fatal(err)
	}
	if err = update.Commit(); err != nil {
		t.Fatal(err)
	}
	inFlight, err := s.build(ctx, tx, config, now)
	if err != nil {
		t.Fatal(err)
	}
	if inFlight.Data.TodayUpload != 0 || len(inFlight.Data.Trend) != 0 || inFlight.Data.NodeTraffic[0].MonthUpload != 0 || inFlight.Data.MachineTraffic[0].Settings.InterfaceName != "" {
		t.Fatal("mixed snapshots", inFlight.Data)
	}
	tx.Close()
	s.refresh(ctx)
	newer, err := s.Read(ctx, id)
	if err != nil || newer.Data.TodayUpload != 9 || newer.Data.Trend[0].Upload != 9 || newer.Data.NodeTraffic[0].MonthUpload != 9 || newer.Data.MachineTraffic[0].Settings.InterfaceName != "eth3" {
		t.Fatal("new snapshot", newer, err)
	}
	if used := newer.Data.MachineTraffic[0].UsedBytes; used == nil || *used != "8" {
		t.Fatal("machine usage included a local day after asOf", used)
	}
}

func TestSharedStatisticsDeletedHistoryAndMachineError(t *testing.T) {
	ctx := context.Background()
	s, writer, id, now := fixture(t)
	retired, _, _, err := (&node.Repository{Database: writer}).CreateEnrollment(ctx, "retired", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id     string
		amount int
	}{{id, 3}, {retired.ID, 7}} {
		if _, err := writer.Exec("INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','accounted',?,?,1)", row.id, now.Add(-time.Hour).Format(time.RFC3339Nano), row.amount); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := writer.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", now.Format(time.RFC3339Nano), retired.ID); err != nil {
		t.Fatal(err)
	}
	s.refresh(ctx)
	all, err := s.Read(ctx, "")
	if err != nil || all.Data.TodayUpload != 10 || all.Data.TodayDownload != 2 || all.Data.TrafficObservations != 2 || len(all.Data.Trend) != 1 || all.Data.Trend[0].Upload != 10 || len(all.Data.NodeTraffic) != 1 || all.Data.NodeTraffic[0].TodayUpload != 3 {
		t.Errorf("global totals and live ranking: %+v %v", all.Data, err)
	}
	if _, err := writer.Exec("INSERT INTO node_machine_traffic_settings(node_id,interface_name) VALUES(?,'eth1')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES(?,'eth1','2026-11-18',9223372036854775807,1)", id); err != nil {
		t.Fatal(err)
	}
	s.refresh(ctx)
	one, err := s.Read(ctx, id)
	if err != nil || one.Status != "ready" || one.Data == nil || one.Data.TodayUpload != 3 || len(one.Data.MachineTraffic) != 0 {
		t.Fatalf("machine error blocked proxy traffic: %+v %v", one, err)
	}
	data, _ := json.Marshal(one.Data)
	if !strings.Contains(string(data), `"machineError":"machine traffic counter overflow"`) {
		t.Fatal("machine error hidden", string(data))
	}
}

func TestSharedOlderReaderLeavesChangedConfigurationQueued(t *testing.T) {
	s, writer, _, _ := fixture(t)
	ctx := context.Background()
	s.refresh(ctx)
	type pinnedKey struct{}
	pinned, release := make(chan struct{}), make(chan struct{})
	readZone := s.nodes.Timezone
	s.nodes.Timezone = func(ctx context.Context, tx *sql.Tx) (string, error) {
		zone, err := readZone(ctx, tx)
		if ctx.Value(pinnedKey{}) != nil {
			close(pinned)
			<-release
		}
		return zone, err
	}
	done := make(chan error, 1)
	go func() { _, err := s.Read(context.WithValue(ctx, pinnedKey{}, true), ""); done <- err }()
	<-pinned
	if _, err := writer.Exec(`INSERT INTO system_settings(key,value_json) VALUES('timezone','"Europe/Berlin"') ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json`); err != nil {
		close(release)
		t.Fatal(err)
	}
	v, err := s.Read(ctx, "")
	close(release)
	if err != nil || v.Status != "pending" {
		t.Fatal(v, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if s.requested == nil || s.requested.zone != "Europe/Berlin" {
		t.Fatal("old reader canceled the queued configuration refresh")
	}
}

func TestSharedEmptyInventorySkipsMachineDailyRows(t *testing.T) {
	s, writer, _, now := fixture(t)
	if _, err := writer.Exec("UPDATE nodes SET deleted_at=?", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("ALTER TABLE node_machine_traffic_daily RENAME TO unavailable_daily"); err != nil {
		t.Fatal(err)
	}
	s.refresh(context.Background())
	v, err := s.Read(context.Background(), "")
	if err != nil || v.Status != "ready" || v.Data.MachineError != "" || v.Data.MachineTraffic == nil || len(v.Data.MachineTraffic) != 0 {
		t.Fatal("empty inventory queried machine history", v.Data, err)
	}
}

func TestSharedMachineFailurePreservesCompleteVersionUntilRecovery(t *testing.T) {
	s, writer, id, now := fixture(t)
	ctx := context.Background()
	if _, err := writer.Exec("INSERT INTO node_machine_traffic_settings(node_id,interface_name) VALUES(?,'eth1')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("INSERT INTO node_machine_traffic_daily(node_id,interface_name,local_date,rx_bytes,tx_bytes) VALUES(?,'eth1','2026-11-18',4,5)", id); err != nil {
		t.Fatal(err)
	}
	s.refresh(ctx)
	first, err := s.Read(ctx, id)
	if err != nil || first.Status != "ready" || *first.Data.MachineTraffic[0].UsedBytes != "9" {
		t.Fatal(first, err)
	}
	s.Now = func() time.Time { return now.Add(30 * time.Second) }
	if _, err := writer.Exec("UPDATE node_machine_traffic_daily SET rx_bytes=9223372036854775807"); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','new',?,11,0)", id, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	s.refresh(ctx)
	stale, err := s.Read(ctx, id)
	if err != nil || stale.Status != "stale" || !strings.Contains(stale.Error, "machine traffic counter overflow") || stale.Version != first.Version || !stale.AsOf.Equal(*first.AsOf) || *stale.AgeSeconds != 30 || stale.Data.TodayUpload != 0 || len(stale.Data.MachineTraffic) != 1 || *stale.Data.MachineTraffic[0].UsedBytes != "9" {
		t.Fatal("lost complete snapshot during machine failure", stale, err)
	}
	if _, err := writer.Exec("UPDATE node_machine_traffic_daily SET rx_bytes=7"); err != nil {
		t.Fatal(err)
	}
	s.refresh(ctx)
	fresh, err := s.Read(ctx, id)
	if err != nil || fresh.Status != "ready" || fresh.Version <= first.Version || fresh.Error != "" || fresh.Data.TodayUpload != 11 || *fresh.Data.MachineTraffic[0].UsedBytes != "12" {
		t.Fatal(fresh, err)
	}
}
