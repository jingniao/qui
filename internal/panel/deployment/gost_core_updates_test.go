package deployment

import (
	"context"
	"strings"
	"testing"

	"proxycontrol/pkg/contracts/release"
)

// gostPin is the one reviewed GOST target: version, official asset URL and
// archive digest come from pkg/contracts/release, never from the caller.
func gostPin() CoreUpdateInput {
	return CoreUpdateInput{Component: "gost", Version: release.GostVersion, URL: "https://github.com/go-gost/gost/releases/download/" + release.GostTag + "/" + release.GostAsset, SHA256: release.GostArchiveSHA256}
}

// An absent GOST core is installed through the ordinary component task, but only
// for an Agent that reported the Hello flag and only with the reviewed pin.
func TestGostCoreUpdateRequiresTheHelloFlagAndTheReviewedPin(t *testing.T) {
	ctx := context.Background()
	// A multi-core Agent that reports Xray: GOST is absent, not unsupported.
	r, catalogue, nodeID, revision := coreNodeFixture(t, "linux/amd64", "xray")
	caps := `{"coreName":"xray","coreVersion":"26.3.27","multiCoreSupported":true,"cores":[{"name":"xray","version":"26.3.27"}]}`
	if _, err := r.Database.Exec("UPDATE nodes SET capabilities_json=?,multi_core_supported=1 WHERE id=?", caps, nodeID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, gostPin()); err == nil {
		t.Fatal("GOST install accepted without the Hello flag")
	}
	if _, err := r.Database.Exec("UPDATE nodes SET gost_core_supported=1 WHERE id=?", nodeID); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*CoreUpdateInput){
		"version": func(in *CoreUpdateInput) { in.Version = "3.3.3" },
		"digest":  func(in *CoreUpdateInput) { in.SHA256 = strings.Repeat("ab", 32) },
		"tag":     func(in *CoreUpdateInput) { in.URL = strings.Replace(in.URL, release.GostTag, "v3.3.3", 1) },
		"asset": func(in *CoreUpdateInput) {
			in.URL = strings.Replace(in.URL, release.GostAsset, "gost_3.3.2_linux_arm64.tar.gz", 1)
		},
	} {
		changed := gostPin()
		change(&changed)
		if _, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, changed); err == nil {
			t.Fatalf("%s outside the reviewed pin accepted", name)
		}
	}
	task, err := r.QueueCoreUpdate(ctx, catalogue, nodeID, "owner", "local", revision.ID, gostPin())
	if err != nil || !task.Adding || task.Component != "gost" || task.Version != release.GostVersion || task.SHA256 != release.GostArchiveSHA256 {
		t.Fatalf("reviewed pin refused: %+v %v", task, err)
	}
	// The pinned asset exists for linux/amd64 only.
	other, otherCatalogue, otherNode, otherRevision := coreNodeFixture(t, "windows/amd64", "xray")
	if _, err := other.Database.Exec("UPDATE nodes SET gost_core_supported=1 WHERE id=?", otherNode); err != nil {
		t.Fatal(err)
	}
	if _, err := other.QueueCoreUpdate(ctx, otherCatalogue, otherNode, "owner", "local", otherRevision.ID, gostPin()); err == nil {
		t.Fatal("GOST install accepted on an unsupported platform")
	}
}
