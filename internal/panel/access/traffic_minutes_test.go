package access

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestReadNodeTrafficRejectsPartialMinuteBoundary(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	if _, err := r.Database.ExecContext(ctx, `INSERT INTO core_traffic_minutes(node_id,core,epoch,bucket,upload_bytes,download_bytes,samples) VALUES(?,'xray','old',?,11,17,1)`, record.NodeID, timestamp(start)); err != nil {
		t.Fatal(err)
	}
	_, err := ReadNodeTraffic(ctx, r.Database, start.Add(30*time.Second), start.Add(time.Minute), record.NodeID, false)
	if !errors.Is(err, ErrTrafficResolution) {
		t.Fatalf("partial minute boundary error=%v", err)
	}
}

func TestTrafficMinutesMergeLateSamplesAndPreserveFallback(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	now := start.Add(25 * time.Hour)
	insert := func(query string, args ...any) {
		t.Helper()
		if _, err := r.Database.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	insert(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','old',?,?,?),(?,'xray','old',?,?,?)`, record.NodeID, timestamp(start), 11, 17, record.NodeID, timestamp(start.Add(30*time.Second)), 13, 19)
	insert(`INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,'acct','cred',?,'u','d',23,29)`, record.NodeID, timestamp(start.Add(10*time.Second)))
	more, err := r.compactTrafficMinutes(ctx, now)
	if err != nil || more {
		t.Fatalf("first compaction more=%v err=%v", more, err)
	}
	var up, down, samples int64
	if err := r.Database.QueryRow(`SELECT upload_bytes,download_bytes,samples FROM core_traffic_minutes WHERE node_id=?`, record.NodeID).Scan(&up, &down, &samples); err != nil {
		t.Fatal(err)
	}
	if up != 24 || down != 36 || samples != 2 {
		t.Fatalf("minute core=%d/%d/%d", up, down, samples)
	}
	got, err := ReadNodeTraffic(ctx, r.Database, start, start.Add(time.Minute), record.NodeID, false)
	want := []NodeTrafficTotal{{NodeID: record.NodeID, Upload: 47, Download: 65, Samples: 3}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("merged traffic got=%+v err=%v want=%+v", got, err, want)
	}
	insert(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','old',?,?,?)`, record.NodeID, timestamp(start.Add(45*time.Second)), 5, 7)
	if more, err := r.compactTrafficMinutes(ctx, now); err != nil || more {
		t.Fatalf("late compaction more=%v err=%v", more, err)
	}
	if err := r.Database.QueryRow(`SELECT upload_bytes,download_bytes,samples FROM core_traffic_minutes WHERE node_id=?`, record.NodeID).Scan(&up, &down, &samples); err != nil {
		t.Fatal(err)
	}
	if up != 29 || down != 43 || samples != 3 {
		t.Fatalf("late minute core=%d/%d/%d", up, down, samples)
	}
	if err := r.Database.QueryRow(`SELECT count(*) FROM core_traffic_samples WHERE node_id=?`, record.NodeID).Scan(&samples); err != nil || samples != 0 {
		t.Fatalf("late raw rows=%d err=%v", samples, err)
	}
}
