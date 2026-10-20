package node

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
	"time"
)

// testAudit writes the same row system.Repository.RecordTx writes, so the test
// proves the entry commits with the deletion.
type testAudit struct{ database *sql.DB }

func (a testAudit) RecordTx(ctx context.Context, tx *sql.Tx, actor, action, target, result, ip string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,?,?,?,?)", time.Now().UTC().Format(time.RFC3339Nano), actor, action, target, result, ip)
	return err
}

type testTasks struct {
	database *sql.DB
	called   []string
}

func (t *testTasks) CloseNodeTasks(ctx context.Context, tx *sql.Tx, nodeID string) error {
	t.called = append(t.called, nodeID)
	return nil
}

func deletionRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return &Repository{Database: database}, ctx
}

func enrolledNode(t *testing.T, r *Repository, ctx context.Context, name string) Node {
	t.Helper()
	n, _, _, err := r.CreateEnrollment(ctx, name, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,'pem',?,'x')", n.ID, "fingerprint-"+n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE nodes SET status='offline' WHERE id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	return n
}

func auditRows(t *testing.T, r *Repository, ctx context.Context, action string) int {
	t.Helper()
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM admin_operations WHERE action=?", action).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestDeleteNodeKeepsHistoryAndRevokesIdentity(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "Enrolled")
	tasks := &testTasks{database: r.Database}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_enrollment_tokens(token_hash,node_id,expires_at) VALUES('hash',?,'x')", n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_certificate_renewals(node_id,previous_fingerprint,certificate_pem,fingerprint,expires_at,created_at) VALUES(?,'old','pem','next','x','x')", n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('g1','Group','subscription',1,1,'x')"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_group_members(group_id,node_id) VALUES('g1',?)", n.ID); err != nil {
		t.Fatal(err)
	}
	// History the deletion must not touch.
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'a','s','success','x',x'00')"); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(1,'" + n.ID + "','success','x')",
		"INSERT INTO access_rollups(id,bucket,node_id,user_id,device_id,domain,target_ip,allowed,dimensions_json,connections,upload_bytes,download_bytes,traffic_known) VALUES('a1','2026-01-01T00:00:00Z','" + n.ID + "','u','d','example.com','203.0.113.1',1,'{}',3,NULL,NULL,0)",
		"INSERT INTO traffic_batches(node_id,batch_id,digest,received_at) VALUES('" + n.ID + "','b1','d','x')",
		"INSERT INTO node_accepted_messages(node_id,sequence,digest) VALUES('" + n.ID + "',7,'d')",
		"INSERT INTO node_health_checks(id,node_id,revision,status,created_at,deadline,created_by) VALUES('h1','" + n.ID + "',1,'queued','x','y','owner')",
	} {
		if _, err := r.Database.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Delete(ctx, n.ID, tasks, testAudit{database: r.Database}, "owner", "192.0.2.1:1"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	deleted, err := r.Get(ctx, n.ID)
	if err != nil || deleted.DeletedAt == "" || deleted.Enabled || deleted.AllowSubscription || deleted.Status != "disabled" {
		t.Fatalf("node not deleted: %+v %v", deleted, err)
	}
	if len(tasks.called) != 1 || tasks.called[0] != n.ID {
		t.Fatalf("deployment tasks were not closed: %v", tasks.called)
	}
	var revoked, tokens, renewals, members int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_identities WHERE node_id=? AND revoked_at IS NOT NULL", n.ID).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("identity not revoked: %d %v", revoked, err)
	}
	for _, item := range []struct {
		query string
		count *int
	}{
		{"SELECT count(*) FROM node_enrollment_tokens WHERE node_id=?", &tokens},
		{"SELECT count(*) FROM node_certificate_renewals WHERE node_id=?", &renewals},
		{"SELECT count(*) FROM node_group_members WHERE node_id=?", &members},
	} {
		if err := r.Database.QueryRowContext(ctx, item.query, n.ID).Scan(item.count); err != nil || *item.count != 0 {
			t.Fatalf("%s survived: %d %v", item.query, *item.count, err)
		}
	}
	var targets, rollups, batches, accepted, checks int
	for _, item := range []struct {
		query string
		count *int
	}{
		{"SELECT count(*) FROM deployment_targets WHERE node_id=?", &targets},
		{"SELECT count(*) FROM access_rollups WHERE node_id=?", &rollups},
		{"SELECT count(*) FROM traffic_batches WHERE node_id=?", &batches},
		{"SELECT count(*) FROM node_accepted_messages WHERE node_id=?", &accepted},
		{"SELECT count(*) FROM node_health_checks WHERE node_id=?", &checks},
	} {
		if err := r.Database.QueryRowContext(ctx, item.query, n.ID).Scan(item.count); err != nil || *item.count != 1 {
			t.Fatalf("history lost: %s %d %v", item.query, *item.count, err)
		}
	}
	if auditRows(t, r, ctx, "删除节点") != 1 {
		t.Fatal("deletion was not audited")
	}
	items, total, err := r.List(ctx, "", 1, 10, false)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("deleted node is still listed: %d %v", total, err)
	}
	items, total, err = r.List(ctx, "", 1, 10, true)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != n.ID {
		t.Fatalf("deleted node missing from the deleted list: %d %v", total, err)
	}
}

func TestDeleteNodeIsIdempotent(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "Twice")
	if err := r.Delete(ctx, n.ID, nil, testAudit{database: r.Database}, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, n.ID, nil, testAudit{database: r.Database}, "owner", "ip"); err != nil {
		t.Fatalf("second delete failed: %v", err)
	}
	if auditRows(t, r, ctx, "删除节点") != 1 {
		t.Fatal("second delete wrote another audit row")
	}
}

func TestDeleteNodeUnknownNode(t *testing.T) {
	r, ctx := deletionRepository(t)
	if err := r.Delete(ctx, "missing", nil, nil, "owner", "ip"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func insert(t *testing.T, r *Repository, ctx context.Context, query string, args ...any) {
	t.Helper()
	if _, err := r.Database.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

// Each blocker kind stops the deletion and leaves the node untouched.
func TestDeleteNodeBlockers(t *testing.T) {
	for _, item := range []struct {
		name  string
		kind  string
		setup func(t *testing.T, r *Repository, ctx context.Context, id string)
	}{
		{"inbound", "inbound", func(t *testing.T, r *Repository, ctx context.Context, id string) {
			insert(t, r, ctx, "INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('i1','In',?,'socks5','127.0.0.1',1080,1,1,0,'direct','[]',x'00','x')", id)
		}},
		{"outbound", "outbound", func(t *testing.T, r *Repository, ctx context.Context, id string) {
			insert(t, r, ctx, "INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,credential_id,encrypted_settings,support_tcp,support_udp,failure_policy,backup_outbound_id,created_at) VALUES('o1','Out',?,'direct','',0,1,'',x'00',1,1,'Block','','x')", id)
		}},
		{"outbound_target", "outbound_target", func(t *testing.T, r *Repository, ctx context.Context, id string) {
			other := enrolledNode(t, r, ctx, "Other")
			insert(t, r, ctx, "INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,credential_id,encrypted_settings,support_tcp,support_udp,failure_policy,backup_outbound_id,target_node_id,created_at) VALUES('o2','Relay',?,'trojan','203.0.113.5',443,1,'',x'00',1,1,'Block','',?,'x')", other.ID, id)
		}},
		{"outbound_group", "outbound_group", func(t *testing.T, r *Repository, ctx context.Context, id string) {
			insert(t, r, ctx, "INSERT INTO outbound_groups(id,name,node_id,strategy,enabled,probe_url,probe_interval,failure_policy,backup_outbound_id,created_at) VALUES('g1','Exit',?,'primary',1,'https://www.gstatic.com/generate_204',30,'Block','','x')", id)
		}},
		{"proxy_chain_node_ids", "proxy_chain", func(t *testing.T, r *Repository, ctx context.Context, id string) {
			insert(t, r, ctx, "INSERT INTO proxy_chains(id,name,node_ids_json,final_outbound_id,support_tcp,support_udp,enabled,failure_policy,backup_target_id,created_at) VALUES('c1','Chain','[\""+id+"\"]','direct',1,1,1,'Block','','x')")
		}},
		{"policy", "policy", func(t *testing.T, r *Repository, ctx context.Context, id string) {
			insert(t, r, ctx, "INSERT INTO policies(id,name,priority,enabled,scope_json,match_json,action_json,logging_json,created_at,updated_at) VALUES('p1','Rule',10,1,?,'{}','{}','{}','x','x')", `{"node_ids":["`+id+`"]}`)
		}},
		{"deployment_batch", "deployment_batch", func(t *testing.T, r *Repository, ctx context.Context, id string) {
			insert(t, r, ctx, "INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'a','s','pending','x',x'00')")
			insert(t, r, ctx, "INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at) VALUES('b1','c','Batch','owner','deploying','x')")
			insert(t, r, ctx, "INSERT INTO deployment_batch_items(batch_id,position,node_id,revision,status) VALUES('b1',0,?,1,'waiting')", id)
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			r, ctx := deletionRepository(t)
			n := enrolledNode(t, r, ctx, "Blocked")
			item.setup(t, r, ctx, n.ID)
			err := r.Delete(ctx, n.ID, nil, testAudit{database: r.Database}, "owner", "ip")
			var blocked *BlockedError
			if !errors.As(err, &blocked) {
				t.Fatalf("expected a blocker, got %v", err)
			}
			found := false
			for _, blocker := range blocked.Items {
				if blocker.Kind == item.kind {
					found = true
				}
			}
			if !found {
				t.Fatalf("blocker kind %q missing from %+v", item.kind, blocked.Items)
			}
			if !errors.Is(err, ErrNodeInUse) {
				t.Fatalf("blocker is not reported as a node in use: %v", err)
			}
			current, err := r.Get(ctx, n.ID)
			if err != nil || current.DeletedAt != "" || !current.Enabled {
				t.Fatalf("blocked deletion changed the node: %+v %v", current, err)
			}
			if auditRows(t, r, ctx, "删除节点") != 0 {
				t.Fatal("blocked deletion was audited")
			}
		})
	}
}

// A finished batch is history, not a blocker.
func TestDeleteNodeIgnoresFinishedBatch(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "Finished")
	insert(t, r, ctx, "INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'a','s','success','x',x'00')")
	insert(t, r, ctx, "INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at) VALUES('b1','c','Batch','owner','success','x')")
	insert(t, r, ctx, "INSERT INTO deployment_batch_items(batch_id,position,node_id,revision,status) VALUES('b1',0,?,1,'success')", n.ID)
	if err := r.Delete(ctx, n.ID, nil, nil, "owner", "ip"); err != nil {
		t.Fatalf("finished batch blocked the deletion: %v", err)
	}
}

func TestPreviewDeletionModes(t *testing.T) {
	r, ctx := deletionRepository(t)
	pending, _, _, err := r.CreateEnrollment(ctx, "Pending", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := r.Preview(ctx, pending.ID)
	if err != nil || preview.Mode != "cancel" || len(preview.Blockers) != 0 || preview.Online {
		t.Fatalf("pending preview: %+v %v", preview, err)
	}
	enrolled := enrolledNode(t, r, ctx, "Enrolled")
	insert(t, r, ctx, "INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('g1','Group','subscription',1,1,'x')")
	insert(t, r, ctx, "INSERT INTO node_group_members(group_id,node_id) VALUES('g1',?)", enrolled.ID)
	insert(t, r, ctx, "INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,credential_id,encrypted_settings,support_tcp,support_udp,failure_policy,backup_outbound_id,created_at) VALUES('o1','Out',?,'direct','',0,1,'',x'00',1,1,'Block','','x')", enrolled.ID)
	preview, err = r.Preview(ctx, enrolled.ID)
	if err != nil || preview.Mode != "delete" || len(preview.Groups) != 1 || preview.Groups[0] != "Group" || len(preview.Blockers) != 1 || preview.Blockers[0].Kind != "outbound" || preview.Blockers[0].Name != "Out" {
		t.Fatalf("enrolled preview: %+v %v", preview, err)
	}
	insert(t, r, ctx, "DELETE FROM outbounds WHERE id='o1'")
	if err := r.Delete(ctx, enrolled.ID, nil, nil, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	preview, err = r.Preview(ctx, enrolled.ID)
	if err != nil || preview.Mode != "deleted" || len(preview.Groups) != 0 {
		t.Fatalf("deleted preview: %+v %v", preview, err)
	}
	if _, err := r.Preview(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeletedNodeRefusesEditsAndEnrollment(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := enrolledNode(t, r, ctx, "Locked")
	insert(t, r, ctx, "INSERT INTO node_enrollment_tokens(token_hash,node_id,expires_at) VALUES(?,?,'2999-01-01T00:00:00.000000000Z')", Hash("token"), n.ID)
	if err := r.Delete(ctx, n.ID, nil, nil, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateSettings(ctx, n.ID, SettingsInput{Name: "Renamed", Enabled: true}); !errors.Is(err, ErrNodeDeleted) {
		t.Fatalf("deleted node accepted settings: %v", err)
	}
	if err := r.SubscriptionFlags(ctx, n.ID, true, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted node accepted subscription flags: %v", err)
	}
	if err := r.ValidateIdentity(ctx, n.ID, "fingerprint-"+n.ID); err == nil {
		t.Fatal("deleted node accepted its revoked identity")
	}
	if _, err := r.EnrollmentNode(ctx, Hash("token")); !errors.Is(err, ErrToken) {
		t.Fatalf("deleted node accepted an enrollment token: %v", err)
	}
	if _, err := r.SaveGroup(ctx, "", GroupInput{Name: "Group", Purpose: "subscription", Enabled: true, NodeIDs: []string{n.ID}}); err == nil {
		t.Fatal("deleted node joined a node group")
	}
	var groups int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_groups").Scan(&groups); err != nil || groups != 0 {
		t.Fatalf("refused group was created: %d %v", groups, err)
	}
}

// A restore keeps a deletion from either side: a node the restored snapshot has
// deleted stays deleted, a node the running panel deleted stays deleted, and a
// node only the running panel knows is inserted as a disabled tombstone that
// keeps its deleted_at.
func TestRestoreKeepsDeletedNodesDeleted(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	candidatePath := filepath.Join(directory, "candidate.db")
	candidate, err := db.Open(ctx, candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.Close()
	r := &Repository{Database: candidate}
	kept := enrolledNode(t, r, ctx, "Kept")
	inserted := enrolledNode(t, r, ctx, "Inserted")
	// The running panel: same nodes, not deleted yet.
	sourcePath := filepath.Join(directory, "source.db")
	source, err := db.Open(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.ExecContext(ctx, "ATTACH DATABASE ? AS origin", candidatePath); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"nodes", "node_identities", "node_certificate_renewals"} {
		if _, err := source.ExecContext(ctx, "INSERT INTO "+table+" SELECT * FROM origin."+table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.ExecContext(ctx, "DETACH DATABASE origin"); err != nil {
		t.Fatal(err)
	}
	// The snapshot has the node deleted; the running panel deleted the other one
	// after the snapshot was taken and no longer has it at all.
	if err := r.Delete(ctx, kept.ID, nil, nil, "owner", "ip"); err != nil {
		t.Fatal(err)
	}
	if _, err := source.ExecContext(ctx, "UPDATE nodes SET deleted_at='2026-01-01T00:00:00.000000000Z',enabled=0,allow_subscription=0 WHERE id=?", inserted.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.ExecContext(ctx, "DELETE FROM node_identities WHERE node_id=?", inserted.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.ExecContext(ctx, "DELETE FROM nodes WHERE id=?", inserted.ID); err != nil {
		t.Fatal(err)
	}
	// The attached evidence database is opened read-only and immutable, so the
	// source WAL must be folded into the main file first.
	if _, err := source.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	if err := db.AttachRecoverySource(ctx, candidate, sourcePath); err != nil {
		t.Fatal(err)
	}
	tx, err := candidate.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := r.PrepareRestoreTx(ctx, tx, "2026-01-02T00:00:00.000000000Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id, name string
	}{{kept.ID, "kept"}, {inserted.ID, "inserted"}} {
		var deleted, enabled, allow, status string
		if err := candidate.QueryRowContext(ctx, "SELECT coalesce(deleted_at,''),enabled,allow_subscription,status FROM nodes WHERE id=?", item.id).Scan(&deleted, &enabled, &allow, &status); err != nil {
			t.Fatalf("%s node missing: %v", item.name, err)
		}
		if deleted == "" || enabled != "0" || allow != "0" || status != "disabled" {
			t.Fatalf("%s node is no longer deleted: %s %s %s %s", item.name, deleted, enabled, allow, status)
		}
	}
}
