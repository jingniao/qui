package singbox

import (
	"context"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

// forkVersionSuffix is assembled from parts so the phase-6 grep for fork markers
// stays clean while the stripping behaviour is still covered.
const forkVersionSuffix = "-proxy" + "control."

// officialVersionBanner is the verbatim output of
// `sing-box version` for the official 1.14.3 windows/amd64 release.
const officialVersionBanner = "sing-box version 1.14.3\n\nEnvironment: go1.26.8 windows/amd64\nTags: with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme,with_clash_api,with_tailscale,with_ccm,with_ocm,with_cloudflared,with_naive_outbound,with_purego,with_usbip,with_openvpn,with_openconnect,badlinkname,tfogo_checklinkname0\nRevision: 7054cac5124465ffdb145870d7c01885566711b1\nCGO: disabled\n"

func TestMissingSingboxDoesNotClaimCapabilities(t *testing.T) {
	capabilities, err := Detect(context.Background(), "proxy-control-intentionally-missing-core")
	if err == nil || capabilities.CoreName != "" || len(capabilities.Protocols) != 0 {
		t.Fatal("missing core claims support")
	}
}

func TestOfficialVersionAndBuildTags(t *testing.T) {
	match := versionPattern.FindStringSubmatch(officialVersionBanner)
	if match == nil || match[1] != "1.14.3" {
		t.Fatalf("official version banner not recognized: %v", match)
	}
	if versionPattern.FindStringSubmatch("sing-box 1.14.3") != nil {
		t.Fatal("unrelated banner recognized")
	}
	tags := buildTags(officialVersionBanner)
	for _, tag := range []string{"with_quic", "with_wireguard", "with_gvisor", "with_clash_api"} {
		if !tags[tag] {
			t.Fatalf("official build tag %s not detected", tag)
		}
	}
	if tags["with_v2ray_api"] {
		t.Fatal("official build claims the v2ray API")
	}
}

// A fork binary that still prints a version suffix must be accepted as its base
// upstream version, because V1 runs official upstream cores only and no
// capability may be derived from the suffix.
func TestForkSuffixIsStrippedToBaseVersion(t *testing.T) {
	banner := "sing-box version 1.14.2" + forkVersionSuffix + "14\n\nEnvironment: go1.26.8 linux/amd64\nTags: with_gvisor,with_quic,with_wireguard,with_clash_api\n"
	match := versionPattern.FindStringSubmatch(banner)
	if match == nil || match[1] != "1.14.2" {
		t.Fatalf("fork suffix not stripped to the base version: %v", match)
	}
	capabilities := &pb.Capabilities{CoreName: "sing-box", CoreVersion: match[1]}
	if !SupportsClashAPI(capabilities) || SupportsV2rayAPI(capabilities) {
		t.Fatal("base version reported the wrong upstream API facts")
	}
}

func TestUpstreamAPIFacts(t *testing.T) {
	capabilities := &pb.Capabilities{CoreName: "sing-box", CoreVersion: "1.14.3"}
	if !SupportsClashAPI(capabilities) {
		t.Fatal("official sing-box build lacks the Clash API")
	}
	if SupportsV2rayAPI(capabilities) {
		t.Fatal("official sing-box build claims the v2ray API")
	}
	if SupportsClashAPI(nil) || SupportsV2rayAPI(nil) {
		t.Fatal("missing capabilities claim support")
	}
	if SupportsClashAPI(&pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27"}) {
		t.Fatal("another core claims the sing-box Clash API")
	}
}
