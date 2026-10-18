package updater

import (
	"context"
	"proxycontrol/pkg/contracts/release"
	"testing"
)

func TestAgentRefusalRecoveryRetainsBaselineAndClosesOriginalInstall(t *testing.T) {
	options, h, signed, policy, record := baselineJournalFixture(t, "proxy-agent")
	ctx := context.Background()
	if record.Request.Validate() == nil {
		t.Fatal("core decoder now accepts Agent operation")
	}
	if release.AgentUpdateRequest(record.Request).Validate() != nil {
		t.Fatal("invalid Agent fixture")
	}
	if _, err := h.BeginBaselineRecovery(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := h.BeginInstallation(ctx, "competing-agent-install", record.Request.Revision, signed, policy); err == nil {
		t.Fatal("pending baseline allowed new installation")
	}
	if _, err := h.finishBaselineRecovery(ctx, record.Request.ID, "recovered"); err != nil {
		t.Fatal(err)
	}
	h.Close()
	reopened, err := OpenHistory(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, found, err := reopened.BaselineRecovery(record.Request.ID)
	if err != nil || !found || restored.Rejection != record.Rejection || restored.Baseline != record.Baseline || restored.Phase != "recovered" {
		t.Fatal(restored, err)
	}
	if _, err := reopened.BeginInstallation(ctx, record.Original.ID, record.Request.Revision, signed, policy); err == nil {
		t.Fatal("refused original install reopened")
	}
	if _, err := reopened.BeginInstallation(ctx, "new-agent-install", record.Request.Revision, signed, policy); err != nil {
		t.Fatal("distinct authorized install blocked", err)
	}
}
