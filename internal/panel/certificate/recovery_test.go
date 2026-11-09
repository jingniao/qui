package certificate

import (
	"context"
	"testing"
	"time"
)

// TestRestoreTurnsInterruptedRunsIntoFailures walks the coordinator's step for this
// module: the archive's rows are kept as stored, and a run the snapshot caught in
// flight becomes a failed row the normal backoff retries. A certificate that was
// valid stays valid and renews on its own schedule, and nothing is queued.
func TestRestoreTurnsInterruptedRunsIntoFailures(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	interrupted := fixture.store(t, []string{"edge.example.com"}, false)
	if err := fixture.repository.MarkIssuing(ctx, interrupted.ID, fixture.now); err != nil {
		t.Fatal(err)
	}
	valid := fixture.store(t, []string{"other.example.com"}, false)
	if err := fixture.repository.MarkIssuing(ctx, valid.ID, fixture.now); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.SaveIssued(ctx, valid.ID, Material{CertificatePEM: "cert", PrivateKeyPEM: "key"}, Stamp(fixture.now), Stamp(fixture.now.Add(90*24*time.Hour)), fixture.now); err != nil {
		t.Fatal(err)
	}
	tx, err := fixture.database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := (&Repository{}).PrepareRestoreTx(ctx, tx, Stamp(fixture.now)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	saved := fixture.reload(t, interrupted.ID)
	if saved.Status != StatusFailed || saved.LastErrorCode != CodeInterrupted || saved.Attempts != 1 {
		t.Fatalf("interrupted row after restore: %+v", saved)
	}
	next, ok := parseStamp(saved.NextAttemptAt)
	if !ok || next.Sub(fixture.now) != time.Hour {
		t.Fatalf("restored retry: %q", saved.NextAttemptAt)
	}
	kept := fixture.reload(t, valid.ID)
	if kept.Status != StatusValid || kept.LastErrorCode != "" || !kept.HasMaterial {
		t.Fatalf("valid row after restore: %+v", kept)
	}
	// A restore never starts an issuance by itself.
	if len(fixture.service.jobs) != 0 {
		t.Fatalf("restore queued %d jobs", len(fixture.service.jobs))
	}
}
