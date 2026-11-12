package control

import (
	"testing"

	"proxycontrol/internal/agent/capability"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// A legacy single-core Agent reports the settings contract in the scalar fields.
// Decorating that report into one core entry must carry the contract instead of
// dropping it, or the panel would gate xhttp/Reality on a report it never sees.
func TestReportCapabilitiesKeepsTheScalarSettingsContract(t *testing.T) {
	t.Parallel()
	state := newCapabilityState(&pb.Capabilities{
		CoreName: "xray", CoreVersion: "26.3.27",
		ServerSettingsContractVersion: 1,
		ServerSettingsCombinations:    []*pb.SettingsCombination{{Protocol: "vless", Direction: "inbound", Security: "reality", Transport: "tcp"}},
	})
	report := reportCapabilities(capability.NewSampler(t.TempDir()), state)
	if len(report.Cores) != 1 || report.Cores[0].Name != "xray" {
		t.Fatalf("capabilities: %+v", report.Cores)
	}
	if report.Cores[0].ServerSettingsContractVersion != 1 || len(report.Cores[0].ServerSettingsCombinations) != 1 {
		t.Fatalf("the scalar settings contract was dropped: %+v", report.Cores[0])
	}
	if report.Cores[0].ServerSettingsCombinations[0].GetSecurity() != "reality" {
		t.Fatalf("the combination was changed: %+v", report.Cores[0].ServerSettingsCombinations[0])
	}
	empty := reportCapabilities(capability.NewSampler(t.TempDir()), newCapabilityState(&pb.Capabilities{CoreName: "xray", CoreVersion: "26.3.27"}))
	if len(empty.Cores) != 1 || empty.Cores[0].ServerSettingsContractVersion != 0 || len(empty.Cores[0].ServerSettingsCombinations) != 0 {
		t.Fatalf("a report without a contract gained one: %+v", empty.Cores)
	}
}
