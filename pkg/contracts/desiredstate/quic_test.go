package desiredstate

import "testing"

func TestQUICSettingsEnforceNativeTransportTLSAndProtocolBoundOptions(t *testing.T) {
	for _, protocol := range []string{"hysteria2", "tuic"} {
		valid := ProtocolSettings{Transport: "quic", Security: "tls"}
		if err := ValidateSettings(protocol, valid, false); err != nil {
			t.Fatal(protocol, err)
		}
		for _, fault := range []string{"plaintext", "tcp", "foreign-option", "invalid-mode"} {
			settings := valid
			switch fault {
			case "plaintext":
				settings.Security = "none"
			case "tcp":
				settings.Transport = "tcp"
			case "foreign-option":
				settings.QUIC = &QUICSettings{}
				if protocol == "tuic" {
					settings.QUIC.UpMbps = 100
				} else {
					settings.QUIC.CongestionControl = "bbr"
				}
			case "invalid-mode":
				if protocol == "tuic" {
					settings.QUIC = &QUICSettings{UDPRelayMode: "untyped"}
				} else {
					settings.QUIC = &QUICSettings{UpMbps: -1}
				}
			}
			if err := ValidateSettings(protocol, settings, false); err == nil {
				t.Fatal("invalid QUIC contract accepted", protocol, fault)
			}
		}
	}
	if err := ValidateSettings("http", ProtocolSettings{QUIC: &QUICSettings{}}, false); err == nil {
		t.Fatal("QUIC settings silently ignored by another protocol")
	}
	if err := ValidateUUID("not-a-uuid"); err == nil {
		t.Fatal("invalid TUIC identity accepted")
	}
}
