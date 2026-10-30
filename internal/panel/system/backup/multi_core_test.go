package backup

import (
	"context"
	"encoding/json"
	"path/filepath"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestMultiCoreArchiveInventoryAndResultValidation(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	db := source.Database
	var node string
	var rev int64
	if err := db.QueryRow("SELECT n.id,r.id FROM nodes n CROSS JOIN config_revisions r LIMIT 1").Scan(&node, &rev); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	result, _ := json.Marshal(&pb.RemoveCoreResult{Id: "remove", Component: "sing-box", Revision: rev, Success: true, Code: "success"})
	for _, q := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO core_remove_tasks(id,node_id,component,revision,status,code,result_json,created_by,created_at,updated_at) VALUES('remove',?,'sing-box',?,'success','success',?,'owner',?,?)", []any{node, rev, result, stamp, stamp}},
		{"INSERT INTO node_traffic_cursors VALUES(?,'core.node','node','sing-box',?,12,34,?)", []any{node, rev, stamp}},
		{"INSERT INTO core_traffic_samples VALUES(?,'sing-box','core.node',?,12,34)", []any{node, stamp}},
	} {
		if _, err := db.Exec(q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateMultiCore(ctx, db); err != nil {
		t.Fatal(err)
	}
	archive, err := source.Create(ctx, directory, "multi_core_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, archive.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		copy, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer copy.Close()
		inventory, err := ReadInventory(ctx, copy)
		if err != nil {
			return err
		}
		if inventory.Totals.CoreRemoveTasks != 1 || inventory.Totals.NodeTrafficCursors != 1 || inventory.Totals.CoreTrafficSamples != 1 {
			t.Fatal(inventory.Totals)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE core_remove_tasks SET component='xray'"); err != nil {
		t.Fatal(err)
	}
	if validateMultiCore(ctx, db) == nil {
		t.Fatal("mismatched removal result accepted")
	}
}
