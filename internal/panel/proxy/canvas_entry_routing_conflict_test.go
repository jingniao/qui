package proxy

import (
	"strings"
	"testing"
)

func TestCanvasEntryRoutingConflictBindingIdentity(t *testing.T) {
	for _, currentBound := range []bool{false, true} {
		for _, samePath := range []bool{false, true} {
			name := "legacy_to_reuse"
			if currentBound {
				name = "reuse_to_legacy"
			}
			if samePath {
				name += "_same_path"
			}
			t.Run(name, func(t *testing.T) {
				p := newPublishPanel(t)
				layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"})
				if currentBound {
					layout.Instances[0].EntryBinding = nil
					layout.Instances[0].Ref = "inbound:" + p.inbound.ID
				}
				other := savePlanScheme(t, p, layout)
				current := other
				current.ID = "another-scheme"
				current.Layout.Instances = append([]CanvasInstance{}, other.Layout.Instances...)
				current.Layout.Connections = append([]CanvasConnection{}, other.Layout.Connections...)
				if currentBound {
					current.Layout.Instances[0].Ref = "server:" + p.nodeA
					current.Layout.Instances[0].EntryBinding = &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}
				} else {
					current.Layout.Instances[0].Ref = "inbound:" + p.inbound.ID
					current.Layout.Instances[0].EntryBinding = nil
				}
				if !samePath {
					current.Layout.Connections[0].TunnelID = "different-path"
				}
				err := p.service.canvasEntryRoutingConflict(p.ctx, current, &canvasPlan{})
				if samePath {
					if err != nil {
						t.Fatalf("same route rejected: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "entry_routing_conflict") {
					t.Fatalf("different routes for the same inbound accepted: %v", err)
				}
			})
		}
	}
}

func TestCanvasEntryRoutingConflictOwnedEntry(t *testing.T) {
	for _, reuse := range []bool{false, true} {
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
