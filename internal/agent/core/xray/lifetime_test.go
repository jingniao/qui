package xray

import (
	"errors"
	"os/exec"
	"testing"

	"proxycontrol/internal/agent/core"
)

// TestRunMarkerIsBoundToTheAdapterIdentity proves the shared run-marker material
// lives in this adapter's runtime directory under its own node and core name, and
// that a marker naming another node is never removed nor taken as a clean exit.
func TestRunMarkerIsBoundToTheAdapterIdentity(t *testing.T) {
	directory := t.TempDir()
	adapter := &Adapter{Directory: directory, NodeID: "node-a"}
	if err := adapter.runMarker().Claim("epoch-1", 7); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := core.LoadCoreRun(directory)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.NodeID != "node-a" || claimed.Core != "xray" || claimed.Epoch != "epoch-1" || claimed.Revision != 7 {
		t.Fatalf("claimed marker = %+v", claimed)
	}
	if err := adapter.runMarker().ConfirmExit(); err != nil {
		t.Fatal(err)
	}
	if remaining, _, err := core.LoadCoreRun(directory); err != nil || remaining != nil {
		t.Fatalf("confirmed exit left a marker: %+v %v", remaining, err)
	}
	foreign := &Adapter{Directory: directory, NodeID: "node-b"}
	if err := foreign.runMarker().Claim("epoch-b", 3); err != nil {
		t.Fatal(err)
	}
	if err := adapter.lifecycleReadyLocked(); !errors.Is(err, core.ErrCoreStopUnconfirmed) {
		t.Fatalf("a foreign node marker did not block the data plane: %v", err)
	}
	if remaining, _, err := core.LoadCoreRun(directory); err != nil || remaining == nil {
		t.Fatalf("the foreign marker was removed: %+v %v", remaining, err)
	}
}

// TestRunningChildRequiresTheAccountedEpoch proves a child whose start neve
// settled keeps its process without proving the run of the marker.
func TestRunningChildRequiresTheAccountedEpoch(t *testing.T) {
	adapter := &Adapter{process: &exec.Cmd{}, statsEpoch: "epoch-1"}
	adapter.marker.Epoch = "epoch-1"
	if !adapter.runningChildLocked() {
		t.Fatal("the accounted child was not recognised")
	}
	adapter.statsEpoch = "epoch-0"
	if adapter.runningChildLocked() {
		t.Fatal("a child outside the accounted epoch was proven")
	}
	adapter.statsEpoch = "epoch-1"
	adapter.process = nil
	if adapter.runningChildLocked() {
		t.Fatal("a stopped child was proven")
	}
}
