package main

import (
	"flag"
	"io"
	"sort"
	"strings"
	"testing"
)

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
		"apply-host-network",
		"check-config",
		"config",
		"control-only-contract",
		"enroll",
		"initialize-agent-history",
		"managed-worker",
		"renew-node-certificate",
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
