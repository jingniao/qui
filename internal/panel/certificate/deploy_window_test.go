package certificate

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A renewal that failed keeps the certificate valid and records its backoff; the
// sweep must not retry it before that time, or the CA's failed-validation limit
// is hit by an hourly retry.
func TestFailedRenewalIsNotRetriedBeforeItsBackoff(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	item := fixture.store(t, []string{"edge.example.com"}, false)
	fixture.service.run(ctx, fixture.reload(t, item.ID))
	if _, err := fixture.database.Exec("UPDATE certificates SET not_after=? WHERE id=?", Stamp(fixture.now.Add(20*24*time.Hour)), item.ID); err != nil {
		t.Fatal(err)
	}
	if !due(fixture.reload(t, item.ID), fixture.now) {
		t.Fatal("a certificate inside the renewal window is due")
	}
	fixture.issuer.order.failFinalize = fail(CodeACMERateLimited)
	fixture.service.run(ctx, fixture.reload(t, item.ID))
	failed := fixture.reload(t, item.ID)
	if failed.Status != StatusValid || failed.NextAttemptAt == "" {
		t.Fatalf("failed renewal: %+v", failed)
	}
	for _, wait := range []time.Duration{0, 30 * time.Minute, 59 * time.Minute} {
		if due(failed, fixture.now.Add(wait)) {
			t.Fatalf("retried %v after the failure, inside the 1 hour backoff", wait)
		}
	}
	fixture.service.sweep(ctx)
	if len(fixture.service.jobs) != 0 {
		t.Fatal("the sweep queued a renewal inside its backoff")
	}
	if !due(failed, fixture.now.Add(time.Hour+time.Minute)) {
		t.Fatal("the renewal is not retried after its backoff")
	}
	fixture.now = fixture.now.Add(time.Hour + time.Minute)
	fixture.service.sweep(ctx)
	if len(fixture.service.jobs) != 1 {
		t.Fatalf("queued %d jobs after the backoff", len(fixture.service.jobs))
	}
}

func TestDeployWindowSettings(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	settings, err := fixture.repository.ReadSettings(ctx)
	if err != nil || settings.DeployStartHour != 3 || settings.DeployEndHour != 6 {
		t.Fatalf("defaults: %+v %v", settings, err)
	}
	// A value stored before the window existed has no hour fields and reads as the
	// defaults.
	if _, err := fixture.database.Exec(`UPDATE system_settings SET value_json=? WHERE key=?`, `{"email":"owner@example.com","directory":"staging","revision":1,"updatedAt":"`+Stamp(fixture.now)+`","updatedBy":"owner"}`, settingsKey); err != nil {
		t.Fatal(err)
	}
	if settings, err = fixture.repository.ReadSettings(ctx); err != nil || settings.DeployStartHour != 3 || settings.DeployEndHour != 6 {
		t.Fatalf("older value: %+v %v", settings, err)
	}
	hour := func(value int) *int { return &value }
	save := func(start, end *int, revision int64) error {
		_, _, err := fixture.repository.SaveSettings(ctx, SaveSettingsInput{Email: "owner@example.com", Directory: DirectoryStaging, DeployStartHour: start, DeployEndHour: end, ExpectedRevision: revision, Actor: "owner"})
		return err
	}
	for _, invalid := range [][2]*int{{hour(24), hour(5)}, {hour(-1), hour(5)}, {hour(2), hour(24)}, {hour(4), hour(4)}, {hour(6), nil}} {
		if err := save(invalid[0], invalid[1], 1); !errors.Is(err, ErrSettingsInvalid) {
			t.Fatalf("window %v %v accepted: %v", invalid[0], invalid[1], err)
		}
	}
	// A window that wraps midnight is valid; omitting the hours keeps them.
	if err := save(hour(22), hour(2), 1); err != nil {
		t.Fatal(err)
	}
	if err := save(nil, nil, 2); err != nil {
		t.Fatal(err)
	}
	if settings, err = fixture.repository.ReadSettings(ctx); err != nil || settings.DeployStartHour != 22 || settings.DeployEndHour != 2 || settings.Revision != 2 {
		t.Fatalf("saved: %+v %v", settings, err)
	}
	view, err := fixture.service.SettingsView(ctx)
	if err != nil || view.DeployStartHour != 22 || view.DeployEndHour != 2 {
		t.Fatalf("view: %+v %v", view, err)
	}
}
