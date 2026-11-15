package node

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestNodeRelayCleanupFieldsInListAndDetail(t *testing.T) {
	r, id := machineFixture(t)
	ctx := context.Background()
	for _, pending := range []bool{false, true, false} {
		if pending {
			stamp := time.Now().UTC().Format(time.RFC3339Nano)
			if _, err := r.Database.ExecContext(ctx, "INSERT INTO relay_cleanup_nodes(node_id,reason,last_code,next_attempt_at,created_at,updated_at) VALUES(?,'relay cleanup','health_failed',?,?,?)", id, stamp, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		} else if _, err := r.Database.ExecContext(ctx, "DELETE FROM relay_cleanup_nodes WHERE node_id=?", id); err != nil {
			t.Fatal(err)
		}
		detail, err := r.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		items, _, err := r.List(ctx, "", 1, 100, false)
		if err != nil || len(items) != 1 {
			t.Fatalf("list %v: %v", items, err)
		}
		for _, n := range []Node{detail, items[0]} {
			code := ""
			if pending {
				code = "health_failed"
			}
			if n.RelayCleanupPending != pending || n.RelayCleanupCode != code {
				t.Fatalf("cleanup fields: %+v", n)
			}
			data, err := json.Marshal(n)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				t.Fatal(err)
			}
			if body["relayCleanupPending"] != pending || body["relayCleanupCode"] != code {
				t.Fatalf("JSON fields: %s", data)
			}
		}
	}
}
