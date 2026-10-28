package proxy

import (
	"encoding/json"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
)

func TestForwardCanvasPublishCapabilityAndIdentity(t *testing.T) {
	p := newPublishPanel(t)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, "", []CanvasConnection{{ID: "forward-edge", Source: "i-a", Target: "i-b", Type: "port_forward", TunnelProtocol: "gost", SupportTCP: true, SupportUDP: true}})
	layout.Instances[0].ListenPort = canvasTestPort(12345)
	layout.Instances[1].TargetPort = canvasTestPort(8443)
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "forward", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil || !strings.Contains(err.Error(), "advertise") {
		t.Fatal("old Agent accepted", err)
	}
	n, err := p.service.Nodes.Get(p.ctx, p.nodeA)
	if err != nil {
		t.Fatal(err)
	}
	n.Capabilities.ForwardInboundSupported = true
	raw, _ := json.Marshal(n.Capabilities)
	if _, err = p.service.Repository.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), p.nodeA); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
			t.Fatal(err)
		}
	}
	inbounds, outbounds, chains := p.publishedObjects(t, scheme.Name)
	if len(inbounds) != 1 || len(outbounds) != 0 || len(chains) != 0 {
		t.Fatal(inbounds, outbounds, chains)
	}
	f := inbounds[0]
	if f.Protocol != "forward" || f.NodeID != p.nodeA || f.Settings.Forward.Port != 8443 || f.SubscriptionVisible || f.InternalRelay || f.RelayCredentialID != "" {
		t.Fatalf("%+v", f)
	}
	s, err := p.service.Snapshot(p.ctx, p.nodeA, p.users)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, in := range s.Inbounds {
		if in.ID == f.ID {
			found = true
			if !in.Enabled || len(in.CredentialIDs) != 0 {
				t.Fatal(in)
			}
		}
	}
	if !found {
		t.Fatal("forward missing")
	}
	input := inboundInputOf(f, "direct")
	input.SubscriptionVisible = true
	if ValidateInbound(input) == nil {
		t.Fatal("subscription accepted")
	}
	input.SubscriptionVisible = false
	input.AllowedUserIDs = []string{"user"}
	if ValidateInbound(input) == nil {
		t.Fatal("users accepted")
	}
	for _, in := range s.Inbounds {
		if in.ID == f.ID {
			in.CredentialIDs = []string{"forbidden"}
			s.Inbounds = []desiredstate.Inbound{in}
		}
	}
	if s.Validate() == nil {
		t.Fatal("credentials accepted")
	}
}
