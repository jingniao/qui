package node

import (
	"context"
	"errors"
	"path/filepath"
	"proxycontrol/internal/platform/db"
	"testing"
	"time"
)

func cancelTestRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return &Repository{Database: database}, ctx
}

func TestCancelEnrollmentRemovesPendingNode(t *testing.T) {
	r, ctx := cancelTestRepository(t)
	n, token, _, err := r.CreateEnrollment(ctx, "Pending", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CancelEnrollment(ctx, n.ID); err != nil {
		t.Fatalf("cancel enrollment failed: %v", err)
	}
	if _, err := r.Get(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
	var tokens int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_enrollment_tokens WHERE node_id=?", n.ID).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatalf("enrollment token survived: %d %v", tokens, err)
	}
	if _, err := r.EnrollmentNode(ctx, Hash(token)); !errors.Is(err, ErrToken) {
		t.Fatalf("cancelled node still enrolls: %v", err)
	}
}

func TestCancelEnrollmentKeepsEnrolledNode(t *testing.T) {
	r, ctx := cancelTestRepository(t)
	n, _, _, err := r.CreateEnrollment(ctx, "Enrolled", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,'pem','fingerprint',?)", n.ID, timestamp(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelEnrollment(ctx, n.ID); !errors.Is(err, ErrNodeEnrolled) {
		t.Fatalf("enrolled node was cancelled: %v", err)
	}
	if _, err := r.Get(ctx, n.ID); err != nil {
		t.Fatalf("enrolled node disappeared: %v", err)
	}
}

func TestCancelEnrollmentKeepsNodeWithDeploymentHistory(t *testing.T) {
	r, ctx := cancelTestRepository(t)
	n, _, _, err := r.CreateEnrollment(ctx, "Deployed", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'a','s','success','x',x'00')"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(1,?,'success','x')", n.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelEnrollment(ctx, n.ID); !errors.Is(err, ErrNodeInUse) {
		t.Fatalf("referenced node was cancelled: %v", err)
	}
	var nodes, revisions, targets int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM nodes WHERE id=?", n.ID).Scan(&nodes); err != nil || nodes != 1 {
		t.Fatalf("referenced node disappeared: %d %v", nodes, err)
	}
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM config_revisions").Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("revision disappeared: %d %v", revisions, err)
	}
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM deployment_targets WHERE node_id=?", n.ID).Scan(&targets); err != nil || targets != 1 {
		t.Fatalf("deployment target disappeared: %d %v", targets, err)
	}
}

func TestCancelEnrollmentKeepsNodeInPolicyScope(t *testing.T) {
	r, ctx := cancelTestRepository(t)
	n, _, _, err := r.CreateEnrollment(ctx, "Routed", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO policies(id,name,priority,enabled,scope_json,match_json,action_json,logging_json,created_at,updated_at) VALUES('p1','Rule',10,1,?,'{}','{}','{}','x','x')", `{"node_ids":["`+n.ID+`"]}`); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelEnrollment(ctx, n.ID); !errors.Is(err, ErrNodeInUse) {
		t.Fatalf("policy-referenced node was cancelled: %v", err)
	}
	if _, err := r.Get(ctx, n.ID); err != nil {
		t.Fatalf("policy-referenced node disappeared: %v", err)
	}
}

func TestCancelEnrollmentRemovesGroupMembership(t *testing.T) {
	r, ctx := cancelTestRepository(t)
	n, _, _, err := r.CreateEnrollment(ctx, "Grouped", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('g1','Group','subscription',1,1,'x')"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_group_members(group_id,node_id) VALUES('g1',?)", n.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelEnrollment(ctx, n.ID); err != nil {
		t.Fatalf("cancel enrollment failed: %v", err)
	}
	var members, groups int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_group_members WHERE node_id=?", n.ID).Scan(&members); err != nil || members != 0 {
		t.Fatalf("group membership survived: %d %v", members, err)
	}
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_groups WHERE id='g1'").Scan(&groups); err != nil || groups != 1 {
		t.Fatalf("node group disappeared: %d %v", groups, err)
	}
}

func TestCancelEnrollmentUnknownNode(t *testing.T) {
	r, ctx := cancelTestRepository(t)
	if err := r.CancelEnrollment(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}
