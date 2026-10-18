package agentjobs

import (
	"errors"
	"proxycontrol/pkg/contracts/release"
	"testing"
)

func TestFailedAgentJobReservesOriginalUntilConfirmedRecovery(t *testing.T) {
	o, install := fixture(t)
	s := openTest(t, o)
	if err := s.Queue(install); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkStarted(install); err != nil {
		t.Fatal(err)
	}
	failed := release.UpdateResult{ID: install.ID, NodeID: install.NodeID, Revision: install.Revision, TargetDigest: install.Release.Manifest.SHA256, Status: "failed_closed", Code: "installation_failed"}
	if err := s.Finish(install, failed); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, o)
	if !s.NeedsRecovery() {
		t.Fatal("failed job lost recovery requirement")
	}
	next := install
	next.ID = "new-install"
	if err := s.Queue(next); !errors.Is(err, ErrBusy) {
		t.Fatal("failure reservation bypassed", err)
	}
	if err := s.Queue(install); err != nil {
		t.Fatal("original idempotent request rejected", err)
	}
	recover := install
	recover.ID = "recover-one"
	recover.Operation = "recover"
	recover.OriginalID = install.ID
	altered := recover
	altered.Revision++
	if err := s.Queue(altered); err == nil {
		t.Fatal("changed recovery scope accepted")
	}
	if err := s.Queue(recover); err != nil {
		t.Fatal(err)
	}
	retryFailure := failed
	retryFailure.ID = recover.ID
	retryFailure.Code = "recovery_failed"
	if err := s.MarkStarted(recover); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(recover, retryFailure); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, o)
	if err := s.Queue(next); !errors.Is(err, ErrBusy) {
		t.Fatal("failed recovery released reservation", err)
	}
	retry := recover
	retry.ID = "recover-two"
	if err := s.Queue(retry); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkStarted(retry); err != nil {
		t.Fatal(err)
	}
	restored := success(retry)
	restored.Status = "rolled_back"
	restored.Code = "rolled_back"
	if err := s.Finish(retry, restored); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = openTest(t, o)
	defer s.Close()
	original, ok := s.Get(install.ID)
	if !ok || original.Result == nil || *original.Result != failed {
		t.Fatal("original failure changed")
	}
	if s.NeedsRecovery() {
		t.Fatal("confirmed recovery did not clear runtime restriction")
	}
	if err := s.Queue(next); err != nil {
		t.Fatal("confirmed recovery retained reservation", err)
	}
}
