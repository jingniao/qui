package access

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTrafficMinuteMixedPrecisionBoundaries(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	cutoff := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	stamps := []string{"2026-11-30T22:33:59.999999999Z", "2026-11-30T22:34:00.834410219Z", "2026-11-30T22:59:59.999999999Z", "2026-11-30T23:00:00.000000000Z", "2026-11-30T23:59:59.999999999Z", "2026-12-01T00:00:00.000000000Z", "2026-12-01T00:00:00.834410219Z", "2026-12-01T00:00:01Z"}
	for i, stamp := range stamps {
		at, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Database.Exec(`INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,'a','c',?,'u','d',?,?)`, record.NodeID, stamp, i+1, (i+1)*2); err != nil {
			t.Fatal(err)
		}
		if i >= 5 {
			at = cutoff.Add(time.Duration(i-5) * time.Second)
		}
		if _, err := r.Database.Exec(`INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','c',?,?,?)`, record.NodeID, timestamp(at), i+1, (i+1)*2); err != nil {
			t.Fatal(err)
		}
	}
	if more, err := r.compactTrafficMinutes(ctx, cutoff.Add(24*time.Hour+30*time.Second)); err != nil || more {
		t.Fatalf("compact: more=%v err=%v", more, err)
	}
	for _, source := range []string{"core", "account"} {
		for i, stamp := range stamps[:5] {
			bucket := stamp[:16] + ":00.000000000Z"
			var up, down, samples int
			if err := r.Database.QueryRow(`SELECT upload_bytes,download_bytes,samples FROM `+source+`_traffic_minutes WHERE bucket=?`, bucket).Scan(&up, &down, &samples); err != nil {
				t.Fatal(err)
			}
			if up != i+1 || down != (i+1)*2 || samples != 1 {
				t.Fatalf("%s %s: %d/%d/%d", source, bucket, up, down, samples)
			}
		}
		var raw, total int
		if err := r.Database.QueryRow(`SELECT count(*) FROM ` + source + `_traffic_samples`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if err := r.Database.QueryRow(`SELECT (SELECT sum(upload_bytes) FROM ` + source + `_traffic_samples)+(SELECT sum(upload_bytes) FROM ` + source + `_traffic_minutes)`).Scan(&total); err != nil {
			t.Fatal(err)
		}
		if raw != 3 || total != 36 {
			t.Fatalf("%s raw=%d total=%d", source, raw, total)
		}
	}
}

func TestTrafficMinuteBacklogBounded(t *testing.T) {
	r, _, record := fixture(t)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	tx, err := r.Database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 501; i++ {
		if _, err := tx.Exec(`INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,'a','c',?,'u','d',1,2)`, record.NodeID, start.Add(time.Duration(i)*time.Minute+834410219*time.Nanosecond).Format("2006-01-02T15:04:05.000000000Z")); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for pass, want := range []int{1, 0} {
		more, err := r.compactTrafficMinutes(context.Background(), start.Add(48*time.Hour))
		if err != nil || more != (pass == 0) {
			t.Fatalf("pass %d: %v %v", pass, more, err)
		}
		var n int
		if err := r.Database.QueryRow("SELECT count(*) FROM account_traffic_samples").Scan(&n); err != nil || n != want {
			t.Fatalf("raw=%d err=%v", n, err)
		}
	}
}

func TestTrafficMinuteHourCutoffAndResolution(t *testing.T) {
	r, _, record := fixture(t)
	cutoff := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	for _, source := range []string{"core", "account"} {
		query := `INSERT INTO core_traffic_minutes(node_id,core,epoch,bucket,upload_bytes,download_bytes,samples) VALUES(?,'xray','c',?,1,2,1)`
		if source == "account" {
			query = `INSERT INTO account_traffic_minutes(node_id,epoch,credential_id,user_id,device_id,bucket,upload_bytes,download_bytes,samples) VALUES(?,'a','c','u','d',?,1,2,1)`
		}
		for _, at := range []time.Time{cutoff.Add(-time.Minute), cutoff} {
			if _, err := r.Database.Exec(query, record.NodeID, at.Format("2006-01-02T15:04:05.000000000Z")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := r.compactCoreTraffic(context.Background(), cutoff.Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := r.Database.QueryRow("SELECT count(*) FROM core_traffic_minutes").Scan(&n); err != nil || n != 1 {
		t.Fatalf("cutoff minute=%d err=%v", n, err)
	}
	if _, err := ReadNodeTraffic(context.Background(), r.Database, cutoff.Add(30*time.Second), cutoff.Add(time.Minute), record.NodeID, false); err == nil {
		t.Fatal("partial core minute accepted")
	}
	if _, err := r.AccountSummary(context.Background(), Filter{From: cutoff.Add(30 * time.Second), To: cutoff.Add(time.Minute), UserID: "u"}); err == nil {
		t.Fatal("partial account minute accepted")
	}
}

func TestAccountMinuteRetentionExactCutoff(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(fmt.Sprint(batched), func(t *testing.T) {
			r, _, record := fixture(t)
			ctx := context.Background()
			now := time.Now().UTC().Truncate(time.Minute)
			cutoff := now.AddDate(0, 0, -DefaultRetention.HourlyDays)
			for _, at := range []time.Time{cutoff.Add(-time.Minute), cutoff} {
				if _, err := r.Database.Exec(`INSERT INTO account_traffic_minutes(node_id,epoch,credential_id,user_id,device_id,bucket,upload_bytes,download_bytes,samples) VALUES(?,'a','c','u','d',?,1,2,1)`, record.NodeID, at.Format("2006-01-02T15:04:05.000000000Z")); err != nil {
					t.Fatal(err)
				}
			}
			if batched {
				if err := r.PruneConfigured(ctx, now); err != nil {
					t.Fatal(err)
				}
			} else {
				tx, err := r.Database.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if err := pruneInTx(ctx, tx, now, DefaultRetention); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			var n int
			if err := r.Database.QueryRow("SELECT count(*) FROM account_traffic_minutes").Scan(&n); err != nil || n != 1 {
				t.Fatalf("cutoff minute count=%d err=%v", n, err)
			}
		})
	}
}

func TestLegacyReceiptRetirementWindowAndBatch(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.Add(-7 * 24 * time.Hour)
	tx, err := r.Database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	digest := strings.Repeat("a", 64)
	for i := 0; i < 501; i++ {
		if _, err := tx.Exec(`INSERT INTO traffic_batches(node_id,batch_id,digest,received_at,accounting_completed_at,reset_id) VALUES(?,?,?,?,?,'reset')`, record.NodeID, fmt.Sprintf("old-%04d", i), digest, timestamp(cutoff.Add(-time.Second)), timestamp(now)); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"recent", "exact", "pending", "incomplete", "numbered"} {
		received, completed, queue, seq := timestamp(cutoff.Add(-time.Second)), timestamp(now), "", 0
		if id == "recent" {
			received = timestamp(cutoff.Add(time.Second))
		}
		if id == "exact" {
			received = timestamp(cutoff)
		}
		if id == "incomplete" {
			completed = ""
		}
		if id == "numbered" {
			queue = "queue"
			seq = 1
		}
		if _, err := tx.Exec(`INSERT INTO traffic_batches(node_id,batch_id,digest,received_at,accounting_completed_at,queue_id,delivery_seq) VALUES(?,?,?,?,?,?,?)`, record.NodeID, id, digest, received, completed, queue, seq); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO traffic_pending_batches(node_id,batch_id,digest,encrypted_batch,created_at) VALUES(?,'pending',?,x'01',?)`, record.NodeID, digest, timestamp(now)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for pass, want := range []int{6, 5} {
		more, err := r.AdvanceTrafficStorage(ctx, now)
		if err != nil || more != (pass == 0) {
			t.Fatalf("pass %d more=%v err=%v", pass, more, err)
		}
		var n, retired int
		if err := r.Database.QueryRow("SELECT count(*) FROM traffic_batches").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if err := r.Database.QueryRow("SELECT retired_receipts FROM traffic_storage_job").Scan(&retired); err != nil {
			t.Fatal(err)
		}
		if n != want || retired != 506-want {
			t.Fatalf("pass %d rows=%d retired=%d", pass, n, retired)
		}
		for _, id := range []string{"recent", "exact", "pending", "incomplete", "numbered"} {
			var present int
			if err := r.Database.QueryRow("SELECT count(*) FROM traffic_batches WHERE node_id=? AND batch_id=?", record.NodeID, id).Scan(&present); err != nil || present != 1 {
				t.Fatalf("protected receipt %s present=%d err=%v", id, present, err)
			}
		}
	}
}
