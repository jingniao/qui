package node

import (
	"context"
	"database/sql"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/testutil/dbtest"
	"testing"
	"time"
)

// machineRestore is a live panel database with one enrolled node.
type machineRestore struct {
	repository *Repository
	nodeID     string
}

func newMachineRestore(t *testing.T) *machineRestore {
	t.Helper()
	database, err := dbtest.Open(context.Background(), filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	repository := &Repository{Database: database, Timezone: func(context.Context, *sql.Tx) (string, error) { return "Asia/Shanghai", nil }}
	n, _, _, err := repository.CreateEnrollment(context.Background(), "machine", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &machineRestore{repository: repository, nodeID: n.ID}
}

func (m *machineRestore) snapshot(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if _, err := m.repository.Database.ExecContext(context.Background(), "VACUUM INTO ?", path); err != nil {
		t.Fatal(err)
	}
	return path
}

// restoreMachineTraffic prepares the archive as the candidate exactly as the
// offline coordinator does and returns the restored repository.
func restoreMachineTraffic(t *testing.T, archive, source string) *Repository {
	t.Helper()
	ctx := context.Background()
	candidate, err := dbtest.Open(ctx, archive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { candidate.Close() })
	if err := db.AttachRecoverySource(ctx, candidate, source); err != nil {
		t.Fatal(err)
	}
	tx, err := candidate.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := (&Repository{}).PrepareRestoreTx(ctx, tx, timestamp(time.Now())); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return &Repository{Database: candidate, Timezone: func(context.Context, *sql.Tx) (string, error) { return "Asia/Shanghai", nil }}
}

func TestMachineTrafficRestoreKeepsCurrentUsage(t *testing.T) {
	live := newMachineRestore(t)
	id := live.nodeID
	now := time.Now().UTC()
	day := now.Format(MachineDayLayout)
	// The snapshot is taken while the day still holds 50 bytes.
	storeMachineDay(t, live.repository, id, "eth0", day, 50, 5)
	storeCursor(t, live.repository, id, "eth0", machineTestBoot, 50, 5, now.Add(-time.Hour))
	archive := live.snapshot(t, "archive.db")
	// The running panel keeps accounting: more on the same day and a new day.
	storeMachineDay(t, live.repository, id, "eth0", day, 500, 50)
	storeMachineDay(t, live.repository, id, "eth0", "2026-01-02", 7, 8)
	if _, err := live.repository.Database.Exec("UPDATE node_machine_traffic_cursors SET rx_bytes=500,tx_bytes=50,observed_at=? WHERE node_id=? AND interface_name='eth0'", timestamp(now), id); err != nil {
		t.Fatal(err)
	}
	restored := restoreMachineTraffic(t, archive, live.snapshot(t, "source.db"))
	// The current rows win exactly: no lost usage and no doubled overlap.
	if rx, tx, rows := machineTotals(t, restored, id, "eth0"); rx != 507 || tx != 58 || rows != 2 {
		t.Fatalf("restored days: %d %d %d", rx, tx, rows)
	}
	var boot string
	var cursor int64
	if err := restored.Database.QueryRow("SELECT boot_id,rx_bytes FROM node_machine_traffic_cursors WHERE node_id=? AND interface_name='eth0'", id).Scan(&boot, &cursor); err != nil || boot != machineTestBoot || cursor != 500 {
		t.Fatalf("restored cursor: %q %d %v", boot, cursor, err)
	}
	// The next heartbeat counts only what happened after the cursor.
	recordHeartbeat(t, restored, id, 1, now.Add(10*time.Second), machineTestBoot, map[string][2]uint64{"eth0": {560, 50}})
	if rx, _, _ := machineTotals(t, restored, id, "eth0"); rx != 567 {
		t.Fatalf("the first heartbeat after the restore counted %d", rx)
	}
	// A restored same-boot cursor continues; a reboot after the restore only
	// rebaselines under the normal rule.
	recordHeartbeat(t, restored, id, 2, now.Add(20*time.Second), "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", map[string][2]uint64{"eth0": {9000, 9000}})
	if rx, _, _ := machineTotals(t, restored, id, "eth0"); rx != 567 {
		t.Fatalf("a changed boot after the restore counted %d", rx)
	}
}

func TestMachineTrafficRestoreEmptyCurrentTableWins(t *testing.T) {
	live := newMachineRestore(t)
	id := live.nodeID
	now := time.Now().UTC()
	storeMachineDay(t, live.repository, id, "eth0", now.Format(MachineDayLayout), 50, 5)
	storeMachineSettings(t, live.repository, id, MachineTrafficSettings{Method: machineMethodMax, InterfaceName: "eth0", ResetDay: 15})
	archive := live.snapshot(t, "archive.db")
	// The Owner cleared the current history; an empty current table is still
	// authoritative and must not resurrect the archived usage.
	for _, query := range []string{"DELETE FROM node_machine_traffic_daily", "DELETE FROM node_machine_traffic_cursors", "DELETE FROM node_machine_traffic_settings"} {
		if _, err := live.repository.Database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	restored := restoreMachineTraffic(t, archive, live.snapshot(t, "source.db"))
	for _, table := range []string{"node_machine_traffic_daily", "node_machine_traffic_cursors", "node_machine_traffic_settings"} {
		if rows := machineCount(t, restored, "SELECT count(*) FROM "+table); rows != 0 {
			t.Fatalf("%s resurrected %d archived rows", table, rows)
		}
	}
}

func TestMachineTrafficRestoreSourcePredatingTheMigration(t *testing.T) {
	live := newMachineRestore(t)
	id := live.nodeID
	now := time.Now().UTC()
	storeMachineDay(t, live.repository, id, "eth0", now.Format(MachineDayLayout), 50, 5)
	archive := live.snapshot(t, "archive.db")
	// A running database from before 0126 has none of the three tables: the
	// archived history stays as the migrated candidate holds it.
	source := live.snapshot(t, "source.db")
	older, err := dbtest.Open(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"DROP TABLE node_machine_traffic_settings", "DROP TABLE node_machine_traffic_daily", "DROP TABLE node_machine_traffic_cursors", "DELETE FROM schema_migrations WHERE version='0126_node_machine_traffic.sql'"} {
		if _, err := older.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := older.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	older.Close()
	restored := restoreMachineTraffic(t, archive, source)
	if rx, tx, rows := machineTotals(t, restored, id, "eth0"); rx != 50 || tx != 5 || rows != 1 {
		t.Fatalf("the archived history was lost: %d %d %d", rx, tx, rows)
	}
}

func TestMachineTrafficRestoreKeepsDeletedNodeRows(t *testing.T) {
	live := newMachineRestore(t)
	ctx := context.Background()
	kept := live.nodeID
	gone, _, _, err := live.repository.CreateEnrollment(ctx, "gone", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range []string{kept, gone.ID} {
		storeMachineDay(t, live.repository, id, "eth0", now.Format(MachineDayLayout), 11, 22)
		storeCursor(t, live.repository, id, "eth0", machineTestBoot, 11, 22, now)
	}
	archive := live.snapshot(t, "archive.db")
	if _, err := live.repository.Database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", timestamp(now), gone.ID); err != nil {
		t.Fatal(err)
	}
	restored := restoreMachineTraffic(t, archive, live.snapshot(t, "source.db"))
	for _, id := range []string{kept, gone.ID} {
		if rx, _, rows := machineTotals(t, restored, id, "eth0"); rx != 11 || rows != 1 {
			t.Fatalf("node %s: %d %d", id, rx, rows)
		}
	}
	if rows := machineCount(t, restored, "SELECT count(*) FROM nodes WHERE id=? AND deleted_at IS NOT NULL", gone.ID); rows != 1 {
		t.Fatal("the deleted node was resurrected")
	}
}
