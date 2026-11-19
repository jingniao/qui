package access

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"proxycontrol/internal/platform/db"
)

func TestTrafficHoursExactChangesAndSourceSwitch(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	stamp := timestamp(start.Add(time.Second))
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := r.Database.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// A core and an account epoch coexist until a node cursor selects the core.
	exec(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','e.node',?,11,17)`, record.NodeID, stamp)
	exec(`INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,'e','credential',?,'u','d',23,29)`, record.NodeID, stamp)
	check := func(up, down, samples int64) {
		t.Helper()
		got, err := ReadNodeTraffic(ctx, r.Database, start, start.Add(time.Hour), record.NodeID, false)
		want := []NodeTrafficTotal{{NodeID: record.NodeID, Upload: up, Download: down, Samples: samples}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v %v want %+v", got, err, want)
		}
		// A partial hour still reads exact detail rather than rounding the bucket.
		got, err = ReadNodeTraffic(ctx, r.Database, start.Add(time.Second), start.Add(time.Second+time.Nanosecond), record.NodeID, false)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("boundary got %+v %v want %+v", got, err, want)
		}
	}
	check(34, 46, 2)
	var indexed int
	if err := r.Database.QueryRow(`SELECT count(*) FROM traffic_sample_hours WHERE node_id=? AND ready=1`, record.NodeID).Scan(&indexed); err != nil || indexed != 2 {
		t.Fatalf("index rows=%d: %v", indexed, err)
	}
	// Updating the same sample increases bytes, not the number of samples.
	exec(`UPDATE core_traffic_samples SET upload_bytes=upload_bytes+5 WHERE node_id=?`, record.NodeID)
	check(39, 46, 2)
	// Even a zero-byte cursor switches the entire historical account epoch off.
	exec(`INSERT INTO node_traffic_cursors(node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at) VALUES(?,'e.node','node','xray',1,0,0,?)`, record.NodeID, stamp)
	check(16, 17, 1)
	exec(`DELETE FROM node_traffic_cursors WHERE node_id=?`, record.NodeID)
	check(39, 46, 2)
	exec(`DELETE FROM account_traffic_samples WHERE node_id=?`, record.NodeID)
	check(16, 17, 1)
}

func TestTrafficHoursBackfillAndCompaction(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if _, err := r.Database.Exec(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','e.node',?,7,13)`, record.NodeID, timestamp(start.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	// Models the migration's unprepared bucket; exact reads remain available.
	if _, err := r.Database.Exec(`UPDATE traffic_sample_hours SET ready=0,upload_bytes=0,download_bytes=0,samples=0`); err != nil {
		t.Fatal(err)
	}
	before, err := ReadNodeTraffic(ctx, r.Database, start, start.Add(time.Hour), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNodeTrafficHours(ctx, r.Database, 1); err != nil {
		t.Fatal(err)
	}
	after, err := ReadNodeTraffic(ctx, r.Database, start, start.Add(time.Hour), "", true)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("backfill: %+v %+v %v", before, after, err)
	}
	// Moving detail to historical storage must not increase or lose any byte.
	if _, err := r.compactCoreTraffic(ctx, start.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, err = ReadNodeTraffic(ctx, r.Database, start, start.Add(time.Hour), "", true)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("compaction: %+v %+v %v", before, after, err)
	}
	// A late report updates the same old hour and remains exactly additive.
	if _, err := r.Database.Exec(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','e.node',?,2,3)`, record.NodeID, timestamp(start.Add(4*time.Second))); err != nil {
		t.Fatal(err)
	}
	after, err = ReadNodeTraffic(ctx, r.Database, start, start.Add(time.Hour), "", true)
	if err != nil || len(after) != 1 || after[0].Upload != 23 || after[0].Download != 42 || after[0].Samples != 4 {
		t.Fatalf("late report: %+v %v", after, err)
	}
}

func TestTrafficHoursOverflowPreservesLedgerAndReportsReadError(t *testing.T) {
	r, _, record := fixture(t)
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	if _, err := r.Database.Exec(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','e.node',?,?,0)`, record.NodeID, timestamp(start), int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','e.node',?,1,0)`, record.NodeID, timestamp(start.Add(time.Second))); err != nil {
		t.Fatalf("display index blocked source accounting: %v", err)
	}
	var count int
	if err := r.Database.QueryRow(`SELECT count(*) FROM core_traffic_samples WHERE node_id=?`, record.NodeID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("source missing: count=%d error=%v", count, err)
	}
	if _, err := ReadNodeTraffic(context.Background(), r.Database, start, start.Add(time.Hour), record.NodeID, false); err == nil {
		t.Fatal("overflow hidden by index")
	}
	// Removing the excess sample allows preparation to recover the exact bucket.
	if _, err := r.Database.Exec(`DELETE FROM core_traffic_samples WHERE node_id=? AND observed_at=?`, record.NodeID, timestamp(start.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareNodeTrafficHours(context.Background(), r.Database, 1); err != nil {
		t.Fatal(err)
	}
	got, err := ReadNodeTraffic(context.Background(), r.Database, start, start.Add(time.Hour), record.NodeID, false)
	if err != nil || len(got) != 1 || got[0].Upload != math.MaxInt64 {
		t.Fatalf("overflow recovery: %+v %v", got, err)
	}
}

func TestTrafficHoursExcludedAccountOverflowDoesNotBlockCore(t *testing.T) {
	r, _, record := fixture(t)
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	for _, credential := range []string{"one", "two"} {
		if _, err := r.Database.Exec(`INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,'e',?,?,'u','d',?,0)`, record.NodeID, credential, timestamp(start), int64(math.MaxInt64)); err != nil {
			t.Fatalf("valid individual account rejected: %v", err)
		}
	}
	if _, err := r.Database.Exec(`INSERT INTO core_traffic_samples VALUES(?,'xray','e.node',?,7,11)`, record.NodeID, timestamp(start)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec(`INSERT INTO node_traffic_cursors(node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at) VALUES(?,'e.node','node','xray',1,7,11,?)`, record.NodeID, timestamp(start)); err != nil {
		t.Fatal(err)
	}
	got, err := ReadNodeTraffic(context.Background(), r.Database, start, start.Add(time.Hour), record.NodeID, false)
	if err != nil || len(got) != 1 || got[0].Upload != 7 || got[0].Download != 11 {
		t.Fatalf("excluded account poisoned node total: %+v %v", got, err)
	}
}

func TestTrafficHoursMatchOriginalQueryAndRestoreReset(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	// Different epochs, core sources, fractional timestamps and hour boundaries
	// exercise both full-hour index reads and the two precise edge queries.
	for i := 0; i < 150; i++ {
		stamp := timestamp(start.Add(time.Duration(i)*7*time.Minute + time.Duration(i%3)*time.Nanosecond))
		if _, err := r.Database.Exec(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray',?,?,?,?)`, record.NodeID, []string{"a.node", "b.node"}[i%2], stamp, i+1, (i+1)*3); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Database.Exec(`INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,'a','c',?,'u','d',101,203)`, record.NodeID, timestamp(start.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.Exec(`INSERT INTO node_traffic_cursors(node_id,epoch,credential_id,core,revision,upload_bytes,download_bytes,observed_at) VALUES(?,'a.node','node','xray',1,0,0,?)`, record.NodeID, timestamp(start)); err != nil {
		t.Fatal(err)
	}
	compare := func() {
		t.Helper()
		tx, err := db.BeginRead(ctx, r.Database)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Close()
		for _, from := range []time.Time{start, start.Add(30 * time.Minute), start.Add(time.Nanosecond), start.Add(75 * time.Minute)} {
			for _, to := range []time.Time{start.Add(time.Hour), start.Add(8 * time.Hour), start.Add(10*time.Hour + time.Nanosecond), start.Add(10*time.Hour + 45*time.Minute)} {
				if to.Before(from) {
					continue
				}
				for _, hourly := range []bool{true, false} {
					old, err := readNodeTrafficSnapshot(ctx, tx, from, to, record.NodeID, hourly, false)
					if err != nil {
						t.Fatal(err)
					}
					fast, err := ReadNodeTrafficSnapshot(ctx, tx, from, to, record.NodeID, hourly)
					if err != nil || !reflect.DeepEqual(old, fast) {
						t.Fatalf("%s/%s hourly=%v: original=%+v indexed=%+v err=%v", from, to, hourly, old, fast, err)
					}
				}
			}
		}
	}
	compare()
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := resetTrafficHoursTx(ctx, tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := r.Database.QueryRow(`SELECT count(*) FROM traffic_sample_hours WHERE ready=0`).Scan(&pending); err != nil || pending < 2 {
		t.Fatalf("reset pending=%d err=%v", pending, err)
	}
	compare()
	more, err := PrepareNodeTrafficHours(ctx, r.Database, 1)
	if err != nil || !more {
		t.Fatalf("first bounded batch: more=%v err=%v", more, err)
	}
	// A source update while other buckets are pending must be included by backfill.
	if _, err := r.Database.Exec(`UPDATE core_traffic_samples SET download_bytes=download_bytes+1 WHERE node_id=?`, record.NodeID); err != nil {
		t.Fatal(err)
	}
	for more {
		more, err = PrepareNodeTrafficHours(ctx, r.Database, 4)
		if err != nil {
			t.Fatal(err)
		}
	}
	compare()
}
