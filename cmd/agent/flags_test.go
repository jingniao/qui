package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestUnenrolledRuntimeLeavesEnrollmentDirectoryUntouched(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent.yaml")
	data := filepath.Join(root, "data")
	if err := os.WriteFile(path, []byte("development: true\ndata_directory: "+filepath.ToSlash(data)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	set := flag.NewFlagSet("agent", flag.ContinueOnError)
	f := registerFlags(set)
	if err := set.Parse([]string{"--config", path}); err != nil {
		t.Fatal(err)
	}
	if err := execute(set, f); err == nil || !strings.Contains(err.Error(), "not enrolled") {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "logs")); !os.IsNotExist(err) {
		t.Fatalf("unenrolled runtime created logs: %v", err)
	}
}

// The Agent command line is a fixed, small surface. This test fails whenever a
// flag is added, removed or renamed without updating the list, so the phase
// report and the deploy scripts cannot drift away from the binary.
func TestAgentFlagsAreExactlyTheSupportedSurface(t *testing.T) {
	set := flag.NewFlagSet("proxy-agent", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	registerFlags(set)
	var got []string
	set.VisitAll(func(f *flag.Flag) { got = append(got, f.Name) })
	sort.Strings(got)
	want := []string{
		"agent-binary",
		"agent-manifest",
		"agent-version",
		"apply-host-network",
		"apply-tuning",
		"check-agent-release",
		"check-config",
		"config",
		"control-only-contract",
		"drain-telemetry",
		"enroll",
		"initialize-agent-history",
		"install-agent-release",
		"local-agent-contract",
		"local-agent-status",
		"local-agent-stopped",
		"managed-worker",
		"renew-node-certificate",
		"reserve-agent-bootstrap",
		"rotate-node-key",
		"storage-contract",
		"storage-contract-version",
		"supervise",
		"token-file",
		"version",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Agent flags changed:\n got %v\nwant %v", got, want)
	}
}

// Removed modes must stay removed: the offline inspection, restore and
// legacy job-migration procedures are no longer part of the Agent.
func TestAgentRejectsRemovedModes(t *testing.T) {
	for _, name := range []string{
		"enroll-only",
		"enable-agent-jobs",
		"confirm-node-certificate",
		"inspect-agent-supervisor",
		"restore-confirmed-agent-image",
		"confirmed-agent-sha256",
		"inspect-enrollment",
		"restore-original-enrollment-result",
		"original-enrollment-result-sha256",
		"finish-original-enrollment-result-publication",
	} {
		set := flag.NewFlagSet("proxy-agent", flag.ContinueOnError)
		set.SetOutput(io.Discard)
		registerFlags(set)
		if err := set.Parse([]string{"--" + name}); err == nil {
			t.Fatalf("removed mode %q still parses", name)
		}
	}
}

func TestInitializeHistoryIsExclusiveOneShot(t *testing.T) {
	for _, extra := range []string{"--supervise", "--version", "--managed-worker", "--enroll=false", "--token-file=", "unexpected"} {
		set := flag.NewFlagSet("agent", flag.ContinueOnError)
		f := registerFlags(set)
		if err := set.Parse([]string{"--config", "missing.yaml", "--initialize-agent-history", extra}); err != nil {
			t.Fatal(err)
		}
		if err := execute(set, f); err == nil {
			t.Fatalf("accepted %s", extra)
		}
	}
	set := flag.NewFlagSet("agent", flag.ContinueOnError)
	f := registerFlags(set)
	if err := set.Parse([]string{"--config", "missing.yaml", "--initialize-agent-history"}); err != nil {
		t.Fatal(err)
	}
	if err := execute(set, f); err == nil || strings.Contains(err.Error(), "supervision requires") {
		t.Fatalf("one-shot did not reach configuration: %v", err)
	}
}
