package access

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/migrations"
)

// Compare only database query work, not browser/network latency. Seven days at
// one sample per five seconds models a recent, uncompacted node traffic series.
func BenchmarkNodeTrafficHourIndex(b *testing.B) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(b.TempDir(), "panel.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO nodes(id,name,status,created_at) VALUES('n','n','online','2026-10-01T00:00:00Z');
 WITH RECURSIVE seq(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM seq WHERE i<120959)
 INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes)
 SELECT 'n','xray','e.node',strftime('%Y-%m-%dT%H:%M:%SZ','2026-10-01',printf('+%d seconds',i*5)),101,203 FROM seq;`); err != nil {
		b.Fatal(err)
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Nanosecond)
	to := from.Add(7*24*time.Hour - time.Second)
	tx, err := db.BeginRead(ctx, database)
	if err != nil {
		b.Fatal(err)
	}
	original, err := readNodeTrafficSnapshot(ctx, tx, from, to, "", false, false)
	if err != nil {
		tx.Close()
		b.Fatal(err)
	}
	prepared, err := ReadNodeTrafficSnapshot(ctx, tx, from, to, "", false)
	tx.Close()
	if err != nil || !reflect.DeepEqual(original, prepared) {
		b.Fatalf("query disagreement: original=%+v prepared=%+v err=%v", original, prepared, err)
	}
	for _, mode := range []struct {
		name  string
		index bool
	}{{"original", false}, {"prepared", true}} {
		b.Run(mode.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				tx, err := db.BeginRead(ctx, database)
				if err != nil {
					b.Fatal(err)
				}
				_, err = readNodeTrafficSnapshot(ctx, tx, from, to, "", false, mode.index)
				tx.Close()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// This separately measures upgrading a populated 0154 database. Query speed
// alone does not measure migration cost or the background writer transaction.
func BenchmarkNodeTrafficExistingDatabasePreparation(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		uri, err := db.URI(filepath.Join(b.TempDir(), "panel.db"))
		if err != nil {
			b.Fatal(err)
		}
		database, err := sql.Open("sqlite", uri)
		if err != nil {
			b.Fatal(err)
		}
		database.SetMaxOpenConns(1)
		prior := fstest.MapFS{}
		for _, name := range []string{"0152_baseline.sql", "0153_drop_one_shot_markers.sql", "0154_traffic_storage_retirement.sql"} {
			data, err := migrations.Files.ReadFile(name)
			if err != nil {
				b.Fatal(err)
			}
			prior[name] = &fstest.MapFile{Data: data}
		}
		if err := db.Migrate(ctx, database, prior); err != nil {
			b.Fatal(err)
		}
		if _, err := database.Exec(`INSERT INTO nodes(id,name,status,created_at) VALUES('n','n','online','2026-10-01T00:00:00Z');
 WITH RECURSIVE seq(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM seq WHERE i<120959)
 INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes)
 SELECT 'n','xray','e.node',strftime('%Y-%m-%dT%H:%M:%SZ','2026-10-01',printf('+%d seconds',i*5)),101,203 FROM seq;`); err != nil {
			b.Fatal(err)
		}
		var before, after, pageSize int64
		if err := database.QueryRow("PRAGMA page_count").Scan(&before); err != nil {
			b.Fatal(err)
		}
		if err := database.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		started := time.Now()
		if err := db.Migrate(ctx, database, migrations.Files); err != nil {
			b.Fatal(err)
		}
		migration := time.Since(started)
		started = time.Now()
		var maxBatch time.Duration
		more := true
		for more {
			batchStart := time.Now()
			more, err = PrepareNodeTrafficHours(ctx, database, 16)
			if err != nil {
				b.Fatal(err)
			}
			if elapsed := time.Since(batchStart); elapsed > maxBatch {
				maxBatch = elapsed
			}
		}
		backfill := time.Since(started)
		b.StopTimer()
		if err := database.QueryRow("PRAGMA page_count").Scan(&after); err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(migration.Microseconds())/1000, "migration-ms")
		b.ReportMetric(float64(backfill.Microseconds())/1000, "backfill-ms")
		b.ReportMetric(float64(maxBatch.Microseconds())/1000, "max-batch-ms")
		b.ReportMetric(float64((after-before)*pageSize), "added-DB-bytes")
		database.Close()
		b.StartTimer()
	}
}
