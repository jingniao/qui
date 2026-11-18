package recovery

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/testutil/dbtest"
	"testing"
)

func TestRestoreLatencyTargetsCurrentSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := listeningFixture(t, func(f *fixture) {
		if _, err := f.source.Database.Exec("INSERT INTO probe_targets(id,label,host,port,scheduled) VALUES('snapshot','snapshot','1.1.1.1',443,1)"); err != nil {
			t.Fatal(err)
		}
	})
	for _, q := range []string{
		"INSERT INTO probe_targets(id,label,host,port) VALUES('new','new','1.0.0.1',443)",
		"UPDATE nodes SET latency_targets_custom=1",
		"INSERT INTO node_latency_targets SELECT id,'new' FROM nodes",
	} {
		if _, err := f.source.Database.Exec(q); err != nil {
			t.Fatal(err)
		}
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
	assertInt(t, restored, "SELECT latency_targets_custom FROM nodes WHERE id=?", 1, f.node.ID)
	assertInt(t, restored, "SELECT count(*) FROM node_latency_targets WHERE node_id=? AND target_id='new'", 1, f.node.ID)
	assertInt(t, restored, "SELECT scheduled FROM probe_targets WHERE id='new'", 0)
	assertInt(t, restored, "SELECT scheduled FROM probe_targets WHERE id='snapshot'", 1)
}
