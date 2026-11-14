package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestOutboundSourceStorageAndSnapshot(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	input := OutboundInput{Name: "bound", NodeID: f.nodeA, Protocol: "direct", Enabled: true, SupportTCP: true, SourceIP: "192.0.2.8", SourceInterface: "eth0"}
	saved, err := f.service.Repository.SaveOutbound(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SourceIP != input.SourceIP || saved.SourceInterface != input.SourceInterface {
		t.Fatalf("%+v", saved)
	}
	state, err := f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range state.Outbounds {
		if o.ID == saved.ID {
			found = o.SourceIP == input.SourceIP && o.SourceInterface == input.SourceInterface
		}
	}
	if !found {
		t.Fatal("snapshot lost source")
	}
	input.SourceIP = ""
	input.SourceInterface = ""
	if _, err = f.service.Repository.SaveOutbound(ctx, saved.ID, input); err != nil {
		t.Fatal(err)
	}
	state, err = f.service.Snapshot(ctx, f.nodeA, f.users)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sourceIp") || strings.Contains(string(data), "sourceInterface") {
		t.Fatalf("%s", data)
	}
}
func TestOutboundSourceValidation(t *testing.T) {
	for _, v := range []struct{ protocol, ip, iface string }{{"direct", "bad", ""}, {"direct", "127.0.0.1", ""}, {"direct", "", "bad/name"}, {"block", "192.0.2.8", ""}, {"block", "", "eth0"}} {
		if err := ValidateOutbound(OutboundInput{Name: "source", NodeID: "node", Protocol: v.protocol, SourceIP: v.ip, SourceInterface: v.iface}); err == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
}
