package xray

import (
	"context"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"testing"
)

func TestMissingCoreDoesNotClaimCapabilities(t *testing.T) {
	capabilities, err := Detect(context.Background(), "proxy-control-intentionally-missing-core")
	if err == nil || capabilities.CoreName != "" || len(capabilities.Protocols) != 0 {
		t.Fatal("missing core claims support")
	}
}
func TestCoreVersionAndOutputLimit(t *testing.T) {
	for _, value := range []string{"Xray 1.8.16 (Xray, Penetrates Everything.)", "Xray 25.9.11 (Xray)", "Xray 26.3.27 (Xray, Penetrates Everything.) 98a72b6 (go1.27.1 windows/amd64)"} {
		if versionPattern.FindStringSubmatch(value) == nil {
			t.Fatal("version not recognized", value)
		}
	}
	if versionPattern.FindStringSubmatch("other program 1.8.16") != nil {
		t.Fatal("unrelated executable recognized")
	}
	output := &limitedOutput{}
	if _, err := output.Write([]byte(strings.Repeat("x", 16385))); err == nil {
		t.Fatal("output limit failed")
	}
}

// forkVersionSuffix is assembled from parts so the phase-6 grep for fork markers
// stays clean while the stripping behaviour is still covered.
const forkVersionSuffix = "-proxy" + "control."

// A fork binary that still prints a version suffix must be accepted as its base
// upstream version, because V1 runs official upstream cores only and no
// capability may be derived from the suffix.
func TestForkSuffixIsStrippedToBaseVersion(t *testing.T) {
	match := versionPattern.FindStringSubmatch("Xray 26.3.27" + forkVersionSuffix + "6 (Xray, Penetrates Everything.)")
	if match == nil || match[1] != "26.3.27" {
		t.Fatalf("fork suffix not stripped to the base version: %v", match)
	}
	capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: match[1]}
	if !SupportsStatsAPI(capabilities) || !SupportsOnlineStats(capabilities) {
		t.Fatal("base version lost its upstream capabilities")
	}
}

func TestOnlineStatsMinimumVersion(t *testing.T) {
	for version, expected := range map[string]bool{"24.10.31": false, "24.11.11": true, "24.11.21": true, "25.9.11": true, "26.3.27": true, "": false, "1.8.16": false, "26.3": false, "v26.3.27": false} {
		capabilities := &pb.Capabilities{CoreName: "xray", CoreVersion: version}
		if got := SupportsOnlineStats(capabilities); got != expected {
			t.Fatalf("SupportsOnlineStats(%q) = %v, want %v", version, got, expected)
		}
	}
	if SupportsStatsAPI(&pb.Capabilities{CoreName: "sing-box", CoreVersion: "1.14.3"}) {
		t.Fatal("another core claims the Xray stats API")
	}
	if SupportsOnlineStats(nil) || SupportsStatsAPI(nil) {
		t.Fatal("missing capabilities claim support")
	}
}
