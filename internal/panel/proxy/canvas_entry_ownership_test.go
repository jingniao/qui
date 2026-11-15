package proxy

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanvasEntryInboundRejectedOnSaveAndPlan(t *testing.T) {
	p, _, binding := cleanupFixture(t)
	for _, index := range []int{0, 1} {
		layout := entryPlanLayout(p, nil)
		layout.Instances[index].Ref = "inbound:" + binding.InboundID
		_, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "other", Execution: "server", Layout: layout})
		if err == nil || !strings.Contains(err.Error(), "方案入口") {
			t.Fatalf("save instance %d: %v", index, err)
		}
		_, err = p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "other", Execution: "server", Layout: layout})
		if err == nil || !strings.Contains(err.Error(), "方案入口") {
			t.Fatalf("plan instance %d: %v", index, err)
		}
		response := httptest.NewRecorder()
		(&Handler{}).failCanvas(response, err)
		if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"inbound_is_scheme_entry"`) {
			t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
		}
	}
}

func TestLinkedTunnelRuleRejectsSchemeEntry(t *testing.T) {
	p, _, binding := cleanupFixture(t)
	_, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "linked entry", EntryNodeID: p.nodeB, PortMode: "auto", Network: "tcp", Target: TunnelTarget{Mode: "linked", InboundID: binding.InboundID}, Enabled: true})
	var target *tunnelError
	if !errors.As(err, &target) || target.Code != "inbound_is_scheme_entry" || target.Status != 400 || !strings.Contains(target.Message, "方案入口") {
		t.Fatalf("linked rule: %v", err)
	}
	response := httptest.NewRecorder()
	(&Handler{}).failTunnel(response, err)
	if response.Code != 400 || !strings.Contains(response.Body.String(), `"code":"inbound_is_scheme_entry"`) {
		t.Fatalf("HTTP %d %s", response.Code, response.Body.String())
	}
}
