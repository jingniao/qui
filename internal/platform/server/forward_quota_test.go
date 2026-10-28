package server

import (
	"proxycontrol/internal/agent/collector"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

func TestForwardUnaffectedByQuotaAndHasNoAccessIdentity(t *testing.T) {
	s := desiredstate.State{Inbounds: []desiredstate.Inbound{{ID: "forward", Protocol: "forward", Enabled: true, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "1.1.1.1", Port: 443, Network: "tcp"}}}}}
	next := mergeUserAccess(s, desiredstate.State{}, nil)
	if !next.Inbounds[0].Enabled || len(next.Inbounds[0].CredentialIDs) != 0 {
		t.Fatal("quota modified forward", next.Inbounds)
	}
	runtime, err := next.WithoutCredentials(map[string]bool{"revoked": true})
	if err != nil || !runtime.Inbounds[0].Enabled {
		t.Fatal(runtime, err)
	}
	if _, exists := collector.NewContext(next).Inbounds["forward"]; exists {
		t.Fatal("forward entered user access-log attribution")
	}
}
