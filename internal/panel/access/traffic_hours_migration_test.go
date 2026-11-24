package access

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"proxycontrol/internal/platform/db"
	"proxycontrol/migrations"
)

func TestTrafficHoursMigrationRebuildsPreparedMinuteCounts(t *testing.T) {
	ctx := context.Background()
	oldFiles := fstest.MapFS{}
	files, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() == "0164_traffic_hour_sample_counts.sql" {
			continue
		}
		data, err := fs.ReadFile(migrations.Files, file.Name())
		if err != nil {
			t.Fatal(err)
		}
		oldFiles[file.Name()] = &fstest.MapFile{Data: data}
	}
	uri, err := db.URI(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", uri)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	if err := db.Migrate(ctx, database, oldFiles); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO nodes(id,name,status,created_at) VALUES('n','node','offline','2026-11-30T00:00:00Z');
 INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES('n','xray','raw','2026-11-30T22:00:00Z',2,3);
 INSERT INTO core_traffic_minutes(node_id,core,epoch,bucket,upload_bytes,download_bytes,samples) VALUES('n','xray','c','2026-11-30T23:34:00.000000000Z',11,13,3);
 INSERT INTO account_traffic_minutes(node_id,epoch,credential_id,user_id,device_id,bucket,upload_bytes,download_bytes,samples) VALUES('n','a','cred','u','d','2026-11-30T23:34:00.000000000Z',17,19,5);
 UPDATE traffic_sample_hours SET ready=1,upload_bytes=11,download_bytes=13,samples=1 WHERE source='core' AND epoch='c';
 UPDATE traffic_sample_hours SET ready=1,upload_bytes=17,download_bytes=19,samples=1 WHERE source='account'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, database, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var pending, cleanReady int
	if err := database.QueryRow("SELECT count(*) FROM traffic_sample_hours WHERE ready=0").Scan(&pending); err != nil || pending != 2 {
		t.Fatalf("invalidated minute hours=%d err=%v", pending, err)
	}
	if err := database.QueryRow("SELECT ready FROM traffic_sample_hours WHERE epoch='raw'").Scan(&cleanReady); err != nil || cleanReady != 1 {
		t.Fatalf("raw-only hour ready=%d err=%v", cleanReady, err)
	}
	if more, err := PrepareNodeTrafficHours(ctx, database, 1); err != nil || !more {
		t.Fatalf("first bounded preparation more=%v err=%v", more, err)
	}
	if more, err := PrepareNodeTrafficHours(ctx, database, 1); err != nil || more {
		t.Fatalf("second bounded preparation more=%v err=%v", more, err)
	}
	start := time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC)
	got, err := ReadNodeTraffic(ctx, database, start, start.Add(time.Hour), "n", false)
	if err != nil || len(got) != 1 || got[0].Upload != 28 || got[0].Download != 32 || got[0].Samples != 8 {
		t.Fatalf("rebuilt traffic=%+v err=%v", got, err)
	}
	var samples int
	if err := database.QueryRow("SELECT (SELECT sum(samples) FROM core_traffic_minutes)+(SELECT sum(samples) FROM account_traffic_minutes)").Scan(&samples); err != nil || samples != 8 {
		t.Fatalf("source minute samples=%d err=%v", samples, err)
	}
}
