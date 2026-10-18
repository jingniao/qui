package updater

import (
	"context"
	"strings"
	"testing"
)

// TestBeginInstallationImageReplaysWithoutSignedAcceptance covers the direct-URL
// core path: the target image is bound by the local verification record, not by
// a signed catalogue acceptance, and the transition still replays on reopen.
func TestBeginInstallationImageReplaysWithoutSignedAcceptance(t *testing.T) {
	options, signed, _, _ := historyFixture(t)
	h, err := CreateHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	ctx := context.Background()
	baseline := imageOf(signed.Manifest)
	baseline.Sequence = 0
	if err := h.BootstrapImage(ctx, "xray", baseline); err != nil {
		t.Fatal(err)
	}
	image := Image{SHA256: strings.Repeat("cd", 32), Bytes: 4096, Version: "26.3.27", Sequence: 1}
	s, err := h.BeginInstallationImage(ctx, "xray", "upd_image", 9, image)
	if err != nil || s.Phase != "switching" || s.Target == nil || *s.Target != image {
		t.Fatalf("image installation not reserved: %+v %v", s, err)
	}
	other := image
	other.SHA256 = strings.Repeat("ef", 32)
	if _, err := h.BeginInstallationImage(ctx, "xray", "upd_image2", 9, other); err == nil {
		t.Fatal("pending image installation replaced")
	}
	if _, err := h.BeginInstallationImage(ctx, "xray", "upd_image3", 9, baseline); err == nil {
		t.Fatal("image without an advanced sequence accepted")
	}
	if _, err := h.TransitionInstallation(ctx, "upd_image", "committed"); err != nil {
		t.Fatal(err)
	}
	h.Close()
	h, err = OpenHistory(options)
	if err != nil {
		t.Fatalf("history with a direct image installation did not replay: %v", err)
	}
	defer h.Close()
	state, ok, err := h.Installation("xray")
	if err != nil || !ok || state.Phase != "committed" || state.Current != image || state.Previous == nil || *state.Previous != baseline {
		t.Fatalf("committed image state lost: %+v %v", state, err)
	}
	later := other
	later.Sequence = 2
	if _, err := h.BeginInstallationImage(ctx, "xray", "upd_image4", 9, later); err != nil {
		t.Fatalf("later image installation refused: %v", err)
	}
}
