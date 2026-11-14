package proxy

import (
	"strings"
	"testing"
)

func TestInboundDeletionRelayOutboundConflict(t *testing.T) {
	p := newCanvasPanel(t)
	owner, csrf := p.session("Owner")
	outbound, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "A to B", NodeID: p.nodeA, Protocol: "socks5", Enabled: true, TargetInboundID: p.inbound.ID, SupportTCP: true, SupportUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	response := p.call("DELETE", "/api/v1/inbounds/"+p.inbound.ID, nil, owner, csrf)
	assertTunnelHTTP(t, response, 409, "inbound_has_relay_outbound")
	for _, value := range []string{outbound.ID, outbound.Name, p.nodeA} {
		if !strings.Contains(response.Body.String(), value) {
			t.Fatal(response.Body.String())
		}
	}
	if err := p.service.Repository.DeleteOutbound(p.ctx, outbound.ID); err != nil {
		t.Fatal(err)
	}
	assertTunnelHTTP(t, p.call("DELETE", "/api/v1/inbounds/"+p.inbound.ID, nil, owner, csrf), 204, "")
}
