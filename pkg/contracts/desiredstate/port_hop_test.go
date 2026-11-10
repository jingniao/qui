package desiredstate

import (
	"reflect"
	"testing"
)

func hopSettings(start, end, interval int) ProtocolSettings {
	return ProtocolSettings{
		Transport: "quic", Security: "tls",
		CertificatePath: CertificateDirectory + "/a.crt",
		PrivateKeyPath:  CertificateDirectory + "/a.key",
		QUIC:            &QUICSettings{HopPortStart: start, HopPortEnd: end, HopIntervalSeconds: interval},
	}
}

func TestHopRangeValidation(t *testing.T) {
	for _, interval := range []int{30, 0} {
		if err := ValidateSettings("hysteria2", hopSettings(20000, 20010, interval), true); err != nil {
			t.Fatal("valid hop range refused, interval", interval, err)
		}
	}
	bad := []struct {
		name              string
		start, end, every int
		protocol          string
		inbound           bool
	}{
		{"end below start", 20010, 20000, 30, "hysteria2", true},
		{"end equals start", 20000, 20000, 30, "hysteria2", true},
		{"too many ports", 1, 1 + MaxHopPorts, 30, "hysteria2", true},
		{"start below 1", 0, 10, 30, "hysteria2", true},
		{"end above 65535", 65530, 65536, 30, "hysteria2", true},
		{"interval too small", 20000, 20010, 4, "hysteria2", true},
		{"interval too large", 20000, 20010, 301, "hysteria2", true},
		{"tuic", 20000, 20010, 30, "tuic", true},
		{"outbound", 20000, 20010, 30, "hysteria2", false},
	}
	for _, tc := range bad {
		if err := ValidateSettings(tc.protocol, hopSettings(tc.start, tc.end, tc.every), tc.inbound); err == nil {
			t.Fatal("invalid hop accepted:", tc.name)
		}
	}
}

func TestHopRangeCannotContainTheListener(t *testing.T) {
	if err := ValidateListenPorts(8443, hopSettings(8400, 8500, 30)); err == nil {
		t.Fatal("a hop range covering its own listener was accepted")
	}
	if err := ValidateListenPorts(8443, hopSettings(20000, 20010, 30)); err != nil {
		t.Fatal("a hop range away from the listener was refused:", err)
	}
}

func TestRequirePortHopSupport(t *testing.T) {
	hop := Inbound{Protocol: "hysteria2", Enabled: true, Settings: hopSettings(20000, 20010, 30)}
	if err := RequirePortHopSupport(State{Inbounds: []Inbound{hop}}, true); err != nil {
		t.Fatal("a capable Agent refused a hop inbound:", err)
	}
	if err := RequirePortHopSupport(State{Inbounds: []Inbound{hop}}, false); err != ErrPortHopUnsupported {
		t.Fatal("an old Agent accepted a hop inbound:", err)
	}
	// A disabled hop, another protocol or an inbound without a hop never needs
	// the capability, so an old Agent keeps publishing everything else unchanged.
	disabled := hop
	disabled.Enabled = false
	plain := hop
	plain.Settings = ProtocolSettings{Security: "tls", CertificatePath: CertificateDirectory + "/a.crt", PrivateKeyPath: CertificateDirectory + "/a.key"}
	for _, state := range []State{
		{Inbounds: []Inbound{disabled}},
		{Inbounds: []Inbound{{Protocol: "vless", Enabled: true, Settings: hop.Settings}}},
		{Inbounds: []Inbound{plain}},
		{},
	} {
		if err := RequirePortHopSupport(state, false); err != nil {
			t.Fatal("old Agent refused a state without an enabled hop:", err)
		}
	}
}

func TestClaimedPortsIncludesTheHopRange(t *testing.T) {
	settings := hopSettings(20000, 20002, 30)
	want := []int{8443, 20000, 20001, 20002}
	if got := ClaimedPorts(8443, settings); !reflect.DeepEqual(got, want) {
		t.Fatalf("ClaimedPorts = %v, want %v", got, want)
	}
	if ports := ListenPorts(8443, settings); len(ports) != 1 || ports[0] != 8443 {
		t.Fatalf("ListenPorts changed for a hop inbound: %v", ports)
	}
	if (&QUICSettings{}).HasHop() {
		t.Fatal("empty QUIC settings report a hop")
	}
	if !settings.QUIC.HasHop() {
		t.Fatal("hop settings do not report a hop")
	}
}
