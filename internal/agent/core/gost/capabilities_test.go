package gost

import "testing"

// TestVersionParsing pins the banner the pinned release prints. `gost version` is
// not a subcommand, so the first line of `-V` with its second field is the only
// accepted source, and anything else must be refused instead of guessed.
func TestVersionParsing(t *testing.T) {
	for _, tc := range []struct {
		name, output, version string
	}{
		{"measured", "gost 3.3.2 (go1.27.3 linux/amd64)\n", "3.3.2"},
		{"no trailing newline", "gost 3.3.2 (go1.27.3 linux/amd64)", "3.3.2"},
		{"other patch", "gost 3.4.0 (go1.27.4 linux/amd64)\n", "3.4.0"},
		{"pre-release is not a plain version", "gost 3.3.2-rc.1 (go1.27.3 linux/amd64)\n", ""},
		{"another program", "sing-box version 1.14.3\n", ""},
		{"the wrong command", "{\"level\":\"fatal\",\"msg\":\"Config File \\\"gost\\\" Not Found\"}\n", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			match := versionPattern.FindStringSubmatch(tc.output)
			version := ""
			if match != nil {
				version = match[1]
			}
			if version != tc.version {
				t.Fatalf("parsed %q, want %q", version, tc.version)
			}
		})
	}
}

// TestCapabilitiesAdvertiseOnlyForward proves the capability row the panel gates
// the editor on: one inbound forward protocol over TCP and UDP, no outbound, and
// none of the account, gate or observation flags.
func TestCapabilitiesAdvertiseOnlyForward(t *testing.T) {
	capabilities := capabilitiesFor("3.3.2")
	if capabilities.OutboundSourceSupported {
		t.Fatal("gost advertised outbound source support")
	}
	if capabilities.CoreName != CoreName || capabilities.CoreVersion != "3.3.2" || !capabilities.ForwardInboundSupported {
		t.Fatalf("capabilities: %+v", capabilities)
	}
	if capabilities.SingboxUserStats || capabilities.DeviceGateSupported || capabilities.ObservationContractVersion != 0 || capabilities.MultiCoreSupported {
		t.Fatalf("gost invented an account, gate or observation capability: %+v", capabilities)
	}
	if len(capabilities.Protocols) != 1 {
		t.Fatalf("protocols: %+v", capabilities.Protocols)
	}
	protocol := capabilities.Protocols[0]
	if protocol.Protocol != "forward" || !protocol.Inbound || protocol.Outbound || !protocol.Tcp || !protocol.Udp {
		t.Fatalf("protocol row: %+v", protocol)
	}
}
