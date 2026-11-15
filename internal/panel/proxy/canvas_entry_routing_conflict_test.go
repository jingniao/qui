package proxy

import (
	"strings"
	"testing"
)

func TestCanvasEntryRoutingConflictOwnedEntry(t *testing.T) {
	for _, reuse := range []bool{false} {
		t.Run(map[bool]string{false: "legacy", true: "reuse"}[reuse], func(t *testing.T) {
			p := newPublishPanel(t)
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"})
			owner := savePlanScheme(t, p, layout)
			if err := p.service.Repository.InsertCanvasEntryBinding(p.ctx, CanvasEntryBindingRecord{ID: "owned-entry", SchemeID: &owner.ID, InstanceID: "i-a", InboundID: p.inbound.ID, Mode: "create", State: "applied", AppliedDefaultOutbound: "direct"}); err != nil {
				t.Fatal(err)
			}
			current := CanvasScheme{ID: "another", Execution: "server", Layout: CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "inbound:" + p.inbound.ID, Role: "entry"}}}}
			if reuse {
				current.Layout.Instances[0].Ref = "server:" + p.nodeA
				current.Layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}
			}
			err := p.service.canvasEntryRoutingConflict(p.ctx, current, &canvasPlan{})
			if err == nil || !strings.Contains(err.Error(), "entry_routing_conflict") {
				t.Fatalf("owned entry takeover accepted: %v", err)
			}
		})
	}
}
