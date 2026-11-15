package proxy

import (
	"database/sql"
	"testing"
	"time"
)

func TestRegisterRelayCleanupResetsBackoffAndPreservesPendingRevision(t *testing.T) {
	for _, status := range []string{"failed", "pending", "success"} {
		t.Run(status, func(t *testing.T) {
			p := newPublishPanel(t)
			if _, err := p.database.Exec("INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(999,'owner','cleanup',?,'now',x'00')", status); err != nil {
				t.Fatal(err)
			}
			if _, err := p.database.Exec("INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(999,?,?,'now')", p.nodeA, status); err != nil {
				t.Fatal(err)
			}
			future := time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339Nano)
			if _, err := p.database.Exec("INSERT INTO relay_cleanup_nodes(node_id,reason,attempts,last_code,next_attempt_at,created_at,updated_at,revision_id) VALUES(?,'relay cleanup',8,'apply_failed',?,'old','old',999)", p.nodeA, future); err != nil {
				t.Fatal(err)
			}
			tx, err := p.database.BeginTx(p.ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = registerRelayCleanup(p.ctx, tx, p.nodeA); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var attempts int
			var code, next, updated string
			var revision sql.NullInt64
			if err = p.database.QueryRow("SELECT attempts,last_code,next_attempt_at,updated_at,revision_id FROM relay_cleanup_nodes WHERE node_id=?", p.nodeA).Scan(&attempts, &code, &next, &updated, &revision); err != nil {
				t.Fatal(err)
			}
			due, err := time.Parse(time.RFC3339Nano, next)
			if err != nil {
				t.Fatal(err)
			}
			if attempts != 0 || code != "" || due.After(time.Now()) || updated == "old" {
				t.Fatalf("did not reset: %d %q %s %s", attempts, code, next, updated)
			}
			if revision.Valid != (status == "pending") || revision.Valid && revision.Int64 != 999 {
				t.Fatalf("revision %+v status %s", revision, status)
			}
		})
	}
}
