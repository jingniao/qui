package recovery

import (
	"context"
	"path/filepath"

	"proxycontrol/internal/testutil/dbtest"

	"testing"
	"time"
)

func TestK2bRestorePreservesCurrentTuningWithoutResend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	f := listeningFixture(t, func(f *fixture) {
		if _, err := f.source.Database.Exec(`INSERT INTO node_tuning(node_id,desired_json,request_id,status,requested_at,updated_at) SELECT id,'{"net.core.somaxconn":"128"}','snapshot','pending',?,? FROM nodes`, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec(`UPDATE node_tuning SET desired_json='{"net.core.default_qdisc":"fq"}',install_missing_modules=1,request_id='current'; INSERT INTO node_metric_samples(node_id,at,cpu_percent) SELECT id,?,0 FROM nodes`, stamp); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT count(*) FROM node_tuning WHERE request_id='current' AND install_missing_modules=1 AND status='unconfirmed' AND desired_json=?", 1, `{"net.core.default_qdisc":"fq"}`)
	assertInt(t, restored, "SELECT count(*) FROM node_metric_samples", 0)
}

func TestK2bRestoreDoesNotReviveSnapshotOnlyTuning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := f.source.Database.Exec(`INSERT INTO node_tuning(node_id,desired_json,request_id,status,requested_at,updated_at) SELECT id,'{}','snapshot','pending',?,? FROM nodes`, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := f.source.Database.Exec("DELETE FROM node_tuning"); err != nil {
		t.Fatal(err)
	}
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := dbtest.Open(ctx, filepath.Join(candidate.Directory, "database", "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	assertInt(t, restored, "SELECT count(*) FROM node_tuning", 0)
}
