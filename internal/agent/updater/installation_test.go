package updater

import (
	"context"
	"errors"
	"testing"
)

func TestInstallationJournalKeepsAcceptanceSeparateAndReplaysFullTransitions(t *testing.T) {
	options, signed, policy, _ := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := h.Accept(ctx, signed, policy); err != nil {
		t.Fatal(err)
	}
	baseline := imageOf(signed.Manifest)
	baseline.Sequence = 0
	if err := h.BootstrapImage(ctx, "proxy-agent", baseline); err != nil {
		t.Fatal(err)
	}
	s, err := h.BeginInstallation(ctx, "install-1", 12, signed, policy)
	if err != nil || s.Phase != "switching" || s.Current.Sequence != 0 {
		t.Fatal(s, err)
	}
	if _, err := h.BeginInstallation(ctx, "install-2", 12, signed, policy); err == nil {
		t.Fatal("pending install replaced")
	}
	if _, err := h.TransitionInstallation(ctx, "unknown", "committed"); err == nil {
		t.Fatal("unrelated result committed")
	}
	if _, err := h.TransitionInstallation(ctx, "install-1", "rolled_back"); err == nil {
		t.Fatal("rollback success without rollback phase")
	}
	s, err = h.TransitionInstallation(ctx, "install-1", "committed")
	if err != nil || s.Current.Sequence != 1 || s.Previous == nil || s.Previous.Sequence != 0 {
		t.Fatal(s, err)
	}
	s.Previous.SHA256 = "caller-mutated"
	if _, err := h.BeginInstallation(ctx, "install-1", 12, signed, policy); err == nil {
		t.Fatal("request id reused for new execution")
	}
	h.Close()
	h, err = OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s, ok, err := h.Installation("proxy-agent")
	if err != nil || !ok || s.Phase != "committed" || s.Previous.SHA256 != baseline.SHA256 || h.highest["proxy-agent"].Sequence != 1 {
		t.Fatal(s, err)
	}
	if _, ok, err := h.InstallationRequest("install-1"); err != nil || !ok {
		t.Fatal("durable request receipt missing", err)
	}
}
func TestInstallationFailureStaysClosedAndRequiresExplicitRecovery(t *testing.T) {
	options, signed, policy, _ := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := h.Accept(ctx, signed, policy); err != nil {
		t.Fatal(err)
	}
	baseline := imageOf(signed.Manifest)
	baseline.Sequence = 0
	if err := h.BootstrapImage(ctx, "proxy-agent", baseline); err != nil {
		t.Fatal(err)
	}
	if _, err := h.BeginInstallation(ctx, "install-failure", 4, signed, policy); err != nil {
		t.Fatal(err)
	}
	if _, err := h.TransitionInstallation(ctx, "install-failure", "rolling_back"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.TransitionInstallation(ctx, "install-failure", "failed_closed"); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h, err = OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	s, _, _ := h.Installation("proxy-agent")
	if !pending(s) || s.Current != baseline {
		t.Fatal("unknown runtime promoted to success", s)
	}
	if _, err := h.TransitionInstallation(ctx, "install-failure", "committed"); !errors.Is(err, ErrInstallation) {
		t.Fatal("failed closed install promoted without new checks", err)
	}
	if _, err := h.TransitionInstallation(ctx, "install-failure", "rolling_back"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.TransitionInstallation(ctx, "install-failure", "rolled_back"); err != nil {
		t.Fatal(err)
	}
	if h.highest["proxy-agent"].Sequence != 1 {
		t.Fatal("failed install erased sequence reservation")
	}
}
