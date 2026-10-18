package main

import (
	"flag"
	"io"
	"strings"
	"testing"

	"proxycontrol/internal/agent/hostnet"
)

// TestApplyHostNetworkModeIsAFixedExclusiveOperation pins the helper entry point:
// it takes only its directory, never a configuration, and it reports the outcome
// of the desired file through the status file the Agent waits for.
func TestApplyHostNetworkModeIsAFixedExclusiveOperation(t *testing.T) {
	directory := t.TempDir()
	run := func(args ...string) error {
		set := flag.NewFlagSet("proxy-agent", flag.ContinueOnError)
		set.SetOutput(io.Discard)
		f := registerFlags(set)
		if err := set.Parse(args); err != nil {
			return err
		}
		return execute(set, f)
	}
	// The unit can start at boot before the first publish: an absent desired file
	// is a successful no-op.
	if err := run("--apply-host-network", directory); err != nil {
		t.Fatalf("an empty helper directory failed: %v", err)
	}
	if err := run("--apply-host-network", directory, "--config", "x"); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("the helper mode accepted another flag: %v", err)
	}
	// An address that is not assigned locally fails before nft runs, and the
	// failure is reported where the Agent waits for it.
	if err := hostnet.WriteDesired(directory, hostnet.Desired{ID: "helper-test", IPv4: "203.0.113.7"}); err != nil {
		t.Fatal(err)
	}
	if err := run("--apply-host-network", directory); err == nil {
		t.Fatal("the helper mode accepted an address that is not assigned locally")
	}
	status, err := hostnet.ReadStatus(directory)
	if err != nil {
		t.Fatal(err)
	}
	if status.ID != "helper-test" || status.OK || !strings.Contains(status.Error, "not assigned") {
		t.Fatalf("the helper reported %+v", status)
	}
}
