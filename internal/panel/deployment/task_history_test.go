package deployment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTaskHistoryPaginationFiltersAndRecovery(t *testing.T) {
	ctx := context.Background()
	r, catalogue, nodes, nodeID, revision, entry, keys := agentTaskFixture(t)
	task, err := r.QueueAgentUpdate(ctx, catalogue, keys, nodeID, entry.ID, "owner", "local", revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Database.Exec(`INSERT INTO core_update_tasks(id,node_id,baseline_revision,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at) VALUES('history-core',?,?,'xray','26.3.27','https://example.test/core',?,1,'rejected','history-command','owner','2020-01-01T00:00:00Z','2020-01-01T00:00:00Z')`, nodeID, revision.ID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	items, total, err := r.TaskHistory(ctx, "all", "all", "", 1, 1)
	if err != nil || total != 2 || len(items) != 1 {
		t.Fatalf("first page: total=%d items=%v err=%v", total, items, err)
	}
	if items[0].ID != task.ID || items[0].Recovery == nil || items[0].Recovery.CommandID != task.CommandID || items[0].Recovery.Generation != task.Generation {
		t.Fatalf("lost recovery identity: %#v", items[0])
	}
	second, total, err := r.TaskHistory(ctx, "all", "all", "", 2, 1)
	if err != nil || total != 2 || len(second) != 1 || second[0].ID != "history-core" || second[0].Recovery != nil {
		t.Fatalf("second page: %+v %d %v", second, total, err)
	}
	filtered, total, err := r.TaskHistory(ctx, "xray", "failed", "节点拒绝", 1, 20)
	if err != nil || total != 1 || len(filtered) != 1 || filtered[0].ID != "history-core" {
		t.Fatalf("filtered: %+v %d %v", filtered, total, err)
	}
	if _, _, err = r.TaskHistory(ctx, "unknown", "all", "", 1, 20); !errors.Is(err, errTaskHistoryFilter) {
		t.Fatalf("invalid filter: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err = r.TaskHistory(canceled, "all", "all", "", 1, 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err = r.Database.Exec("UPDATE core_update_tasks SET adding=1 WHERE id='history-core'"); err != nil {
		t.Fatal(err)
	}
	choices, _, err := nodes.Choices(ctx, "", 1, 20)
	if err != nil || len(choices) != 1 || len(choices[0].CoreSelectionBlocked) != 1 || choices[0].CoreSelectionBlocked[0] != "xray" {
		t.Fatalf("node choice lost unconfirmed core restriction: %+v %v", choices, err)
	}
	rows, err := r.Database.QueryContext(ctx, "EXPLAIN QUERY PLAN "+historyCTE+"SELECT id FROM records ORDER BY updated_at DESC,source,id DESC LIMIT 20")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
}
