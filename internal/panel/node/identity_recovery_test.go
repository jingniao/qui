package node

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func recoveryTestNode(t *testing.T, r *Repository, ctx context.Context) Node {
	t.Helper()
	n, _, _, err := r.CreateEnrollment(ctx, "Recovery", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := strings.Repeat("a", 64)
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,'pem',?,'2999-01-01T00:00:00Z')", n.ID, fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE nodes SET status='offline' WHERE id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIssueRecoveryBindsTokenAndSupersedesPrevious(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := recoveryTestNode(t, r, ctx)
	_, first, _, err := r.IssueRecovery(ctx, n.ID, "owner", 15*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	_, second, _, err := r.IssueRecovery(ctx, n.ID, "owner", 15*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.EnrollmentNodePurpose(ctx, Hash(first)); !errors.Is(err, ErrToken) {
		t.Fatalf("superseded token accepted: %v", err)
	}
	var purpose, bound string
	if err := r.Database.QueryRowContext(ctx, "SELECT purpose,bound_fingerprint FROM node_enrollment_tokens WHERE token_hash=?", Hash(second)).Scan(&purpose, &bound); err != nil {
		t.Fatal(err)
	}
	if purpose != "recover" || bound != strings.Repeat("a", 64) {
		t.Fatalf("binding: %q %q", purpose, bound)
	}
	var revoked int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_identities WHERE node_id=? AND revoked_at IS NOT NULL", n.ID).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("revokeNow: %d %v", revoked, err)
	}
	var status string
	if err := r.Database.QueryRowContext(ctx, "SELECT status FROM node_identity_recoveries WHERE token_hash=?", Hash(first)).Scan(&status); err != nil || status != "superseded" {
		t.Fatalf("superseded record: %q %v", status, err)
	}
}

func TestIssueRecoveryReturnsBlockersAndRejectsDisabledNode(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := recoveryTestNode(t, r, ctx)
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'owner','draft','draft','now',x'00')"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(1,?,'pending','now')", n.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.IssueRecovery(ctx, n.ID, "owner", time.Minute, false); err == nil {
		t.Fatal("active deployment did not block recovery")
	} else {
		var blocked *BlockedError
		if !errors.As(err, &blocked) || blocked.Code != "node_recovery_blocked" {
			t.Fatalf("blocker error: %v", err)
		}
	}
	if _, err := r.Database.ExecContext(ctx, "DELETE FROM deployment_targets WHERE node_id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE nodes SET enabled=0 WHERE id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.IssueRecovery(ctx, n.ID, "owner", time.Minute, false); !errors.Is(err, ErrNodeNotEnrolled) {
		t.Fatalf("disabled node: %v", err)
	}
}

// A claim that never reached a revision is claimable again, so a panel restart
// between the claim and the deploy does not leave the redeploy queued forever.
func TestClaimRecoveryRedeployRetriesAnUnfinishedClaim(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := recoveryTestNode(t, r, ctx)
	if _, _, _, err := r.IssueRecovery(ctx, n.ID, "owner", time.Minute, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE node_identity_recoveries SET redeploy_state='pending' WHERE node_id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	if claimed, err := r.ClaimRecoveryRedeploy(ctx, n.ID); err != nil || !claimed {
		t.Fatalf("first claim: %v %v", claimed, err)
	}
	// The panel stops here: the row is queued without a revision.
	if claimed, err := r.ClaimRecoveryRedeploy(ctx, n.ID); err != nil || !claimed {
		t.Fatalf("unfinished claim was not retried: %v %v", claimed, err)
	}
	if err := r.FinishRecoveryRedeploy(ctx, n.ID, 7, "queued", ""); err != nil {
		t.Fatal(err)
	}
	if claimed, err := r.ClaimRecoveryRedeploy(ctx, n.ID); err != nil || claimed {
		t.Fatalf("a claim with a revision was taken again: %v %v", claimed, err)
	}
}

// The status endpoint reads. It shows an expired code without rewriting the row
// the enrollment path reads.
func TestRecoveryStatusReportsExpiryWithoutWriting(t *testing.T) {
	r, ctx := deletionRepository(t)
	n := recoveryTestNode(t, r, ctx)
	_, token, _, err := r.IssueRecovery(ctx, n.ID, "owner", time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE node_identity_recoveries SET expires_at=? WHERE token_hash=?", timestamp(time.Now().Add(-time.Second)), Hash(token)); err != nil {
		t.Fatal(err)
	}
	status, err := r.RecoveryStatus(ctx, n.ID)
	if err != nil || status.Status != "expired" {
		t.Fatalf("status: %+v %v", status, err)
	}
	var stored string
	if err := r.Database.QueryRowContext(ctx, "SELECT status FROM node_identity_recoveries WHERE token_hash=?", Hash(token)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "issued" {
		t.Fatalf("a read changed the stored status to %q", stored)
	}
}
