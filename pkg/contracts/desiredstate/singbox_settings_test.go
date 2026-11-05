package desiredstate

import "testing"

func TestSingboxRealityContractRefusesLossyServerFields(t *testing.T) {
	combination := SettingsCombination{Protocol: "vless", Direction: DirectionInbound, Security: SecurityReality, Transport: "tcp"}
	capability := CoreSettingsCapability{Core: CoreSingBox, Version: SettingsContractVersion, Combinations: []SettingsCombination{combination}}
	settings := ProtocolSettings{Security: SecurityReality, Reality: testReality(t)}
	if err := RequireInboundServerSettings(CoreSingBox, capability, "vless", settings); err != nil {
		t.Fatal(err)
	}
	settings.Reality.ServerNames = append(settings.Reality.ServerNames, "another.example")
	if err := RequireInboundServerSettings(CoreSingBox, capability, "vless", settings); err == nil {
		t.Fatal("capability authorized losing additional server names")
	}
	// Xray keeps its own broader contract; sing-box field constraints are not a
	// global reduction of the shared server model.
	capability.Core = CoreXray
	if err := RequireInboundServerSettings(CoreXray, capability, "vless", settings); err != nil {
		t.Fatal("sing-box restrictions leaked into Xray", err)
	}
}
