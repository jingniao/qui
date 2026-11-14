package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestCanvasC1ProxyExits(t *testing.T) {
	for _, path := range []string{"P2-inbound", "P3-inbound", "P2-external", "P3-external"} {
		t.Run(path, func(t *testing.T) {
			p := newPublishPanel(t)
			n, err := p.service.Nodes.Get(p.ctx, p.nodeA)
			if err != nil {
				t.Fatal(err)
			}
			n.Capabilities.Protocols = append(n.Capabilities.Protocols, &pb.ProtocolCapability{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true, Udp: true})
			raw, _ := json.Marshal(n.Capabilities)
			if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), p.nodeA); err != nil {
				t.Fatal(err)
			}
			// P2/P3 start with a real VLESS user entry.
			entry, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "HK VLESS", NodeID: p.nodeA, Protocol: "vless", Listen: "0.0.0.0", Port: 10443, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "hk", Ref: "inbound:" + entry.ID, Role: "entry"}}}
			previous, previousNode := "hk", p.nodeA
			if strings.HasPrefix(path, "P3") {
				if _, err := p.service.Repository.Database.ExecContext(p.ctx, "INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,32000,32010)", p.nodeB); err != nil {
					t.Fatal(err)
				}
				layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "jp", Ref: "server:" + p.nodeB, Role: "relay", RelayPortMode: "auto"})
				c := canvasTestHop("hk", "jp", 8443)
				c.TunnelPort = nil
				layout.Connections = append(layout.Connections, c)
				previous, previousNode = "jp", p.nodeB
			}
			var final string
			if strings.HasSuffix(path, "external") {
				out, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "US external SOCKS5", NodeID: previousNode, Protocol: "socks5", Address: "198.51.100.8", Port: 1080, Enabled: true, Username: "relay", Secret: "secret", SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
				if err != nil {
					t.Fatal(err)
				}
				final = out.ID
				layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "us", Ref: "outbound:" + out.ID, Role: "exit"})
			} else {
				in, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "US SOCKS5", NodeID: p.nodeC, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
				if err != nil {
					t.Fatal(err)
				}
				layout.Instances = append(layout.Instances, CanvasInstance{InstanceID: "us", Ref: "inbound:" + in.ID, Role: "exit"})
			}
			c := canvasTestHop(previous, "us", 8443)
			c.TunnelPort = nil
			layout.Connections = append(layout.Connections, c)
			scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: path, Execution: "server", Layout: layout})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
			if err != nil || !plan.OK {
				t.Fatalf("plan %+v: %v", plan, err)
			}
			if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
				t.Fatal(err)
			}
			ins, outs, chains := p.publishedObjects(t, path)
			want := 1
			if strings.HasPrefix(path, "P3") {
				want++
			}
			if final != "" {
				want--
			}
			if len(ins) != want || len(outs) != want {
				t.Fatalf("generated inbounds=%d outbounds=%d want=%d", len(ins), len(outs), want)
			}
			for _, in := range ins {
				if !in.InternalRelay || in.SubscriptionVisible {
					t.Fatalf("relay exposed: %+v", in)
				}
				if in.NodeID == p.nodeB && in.Port != 32000 {
					t.Fatalf("JP port %d", in.Port)
				}
				if in.Protocol != "trojan" || in.Settings.Security != "tls" {
					t.Fatalf("exit protocol %s", in.Protocol)
				}
			}
			stored, err := p.service.Repository.Inbound(p.ctx, entry.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(chains) > 0 {
				wantFinal := final
				if wantFinal == "" {
					wantFinal = "direct"
				}
				if len(chains) != 1 || chains[0].FinalOutboundID != wantFinal || stored.DefaultOutbound != "chain:"+chains[0].ID {
					t.Fatalf("chain %+v entry %s", chains, stored.DefaultOutbound)
				}
			} else if final != "" && stored.DefaultOutbound != final {
				t.Fatalf("entry default %s want %s", stored.DefaultOutbound, final)
			}
			calls := p.deployer.calls
			result, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users)
			if err != nil || len(result.Nodes) != 0 || p.deployer.calls != calls {
				t.Fatalf("republish %+v %v calls=%d/%d", result, err, p.deployer.calls, calls)
			}
			again, _, _ := p.publishedObjects(t, path)
			for i := range ins {
				if again[i].ID != ins[i].ID || again[i].Port != ins[i].Port {
					t.Fatal("republish changed allocated listener")
				}
			}
			if final != "" {
				out, err := p.service.Repository.Outbound(p.ctx, final, true)
				if err != nil {
					t.Fatal(err)
				}
				_, err = p.service.SaveOutbound(p.ctx, final, OutboundInput{Name: out.Name, NodeID: out.NodeID, Protocol: out.Protocol, Address: "198.51.100.9", Port: out.Port, Enabled: true, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
					t.Fatal(err)
				}
				if p.deployer.calls != calls+1 {
					t.Fatal("external outbound edit was skipped by no-op publication")
				}
				if path == "P2-external" {
					stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
					if err != nil {
						t.Fatal(err)
					}
					stored.Layout.Connections = nil
					updated, err := p.service.SaveCanvasScheme(p.ctx, stored.ID, CanvasSchemeInput{Name: stored.Name, Execution: "server", Layout: stored.Layout, UpdatedAt: stored.UpdatedAt})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := p.service.PublishCanvasScheme(p.ctx, updated.ID, "Owner", p.users); err != nil {
						t.Fatal(err)
					}
					in, err := p.service.Repository.Inbound(p.ctx, entry.ID, false)
					if err != nil || in.DefaultOutbound != "direct" {
						t.Fatalf("detached external route %+v %v", in, err)
					}
					if _, err := p.service.Repository.Outbound(p.ctx, final, false); err != nil {
						t.Fatal("detaching deleted the external outbound", err)
					}
				}
			}
		})
	}
}

func TestCanvasC1NarrowingKeepsLegacy(t *testing.T) {
	p := newPublishPanel(t)
	enableCanvasForward(t, p)
	layout := forwardCanvasLayout(p)
	input := CanvasSchemeInput{Name: "legacy forward", Execution: "server", Layout: layout}
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", input); err == nil || !strings.Contains(err.Error(), "端口转发与 GOST 隧道已移至隧道管理") {
		t.Fatalf("new forward: %v", err)
	}
	// Seed an already stored pre-C1 drawing, through the persistence boundary.
	scheme, err := p.service.Repository.SaveCanvasScheme(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	input.Layout = scheme.Layout
	input.UpdatedAt = scheme.UpdatedAt
	if _, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err != nil {
		t.Fatal(err)
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	input.Layout = stored.Layout
	input.UpdatedAt = stored.UpdatedAt
	input.Layout.Connections[0].SupportUDP = true
	if _, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, input); err == nil {
		t.Fatal("legacy mutation with tunnelId accepted")
	}
	// Inline edits use no tunnelId, as in the compatibility API.
	input.Layout.Connections[0].TunnelID = ""
	if _, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, input); err == nil {
		t.Fatal("legacy forward mutation accepted")
	}
}

func TestCanvasC1ManualRelayPort(t *testing.T) {
	p := newPublishPanel(t)
	c := canvasTestHop("i-a", "i-b", 8443)
	layout := canvasTestLayout(p, canvasTestSet{A: p.nodeA, B: p.nodeB}, p.inbound.ID, []CanvasConnection{c})
	layout.Instances[1].RelayPortMode = "manual"
	layout.Instances[1].RelayPort = canvasTestPort(30100)
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "manual", Execution: "server", Layout: layout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveInbound(p.ctx, "", InboundInput{Name: "occupied", NodeID: p.nodeB, Protocol: "socks5", Listen: "0.0.0.0", Port: 30100, Enabled: true, DefaultOutbound: "direct"}); err != nil {
		t.Fatal(err)
	}
	plan, err := p.service.PlanCanvasScheme(p.ctx, scheme.ID, nil)
	if err != nil || plan.OK || len(plan.Conflicts) == 0 || plan.Conflicts[0].Port != 30100 {
		t.Fatalf("manual port conflict %+v %v", plan, err)
	}
	if _, err := p.service.PublishCanvasScheme(p.ctx, scheme.ID, "Owner", p.users); err == nil {
		t.Fatal("manual conflict published")
	}
	stored, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Layout.Instances[1].RelayPort = canvasTestPort(30101)
	updated, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: stored.Name, Execution: "server", Layout: stored.Layout, UpdatedAt: stored.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := p.service.PublishCanvasScheme(p.ctx, updated.ID, "Owner", p.users); err != nil {
			t.Fatal(err)
		}
	}
	ins, _, _ := p.publishedObjects(t, scheme.Name)
	if len(ins) != 1 || ins[0].Port != 30101 || p.deployer.calls != 1 {
		t.Fatalf("manual listener %+v calls=%d", ins, p.deployer.calls)
	}
}

func TestCanvasC1ExternalExitValidation(t *testing.T) {
	p := newPublishPanel(t)
	out, err := p.service.SaveOutbound(p.ctx, "", OutboundInput{Name: "wrong node", NodeID: p.nodeB, Protocol: "socks5", Address: "198.51.100.8", Port: 1080, Enabled: true, SupportTCP: true, SupportUDP: true, FailurePolicy: "Block"})
	if err != nil {
		t.Fatal(err)
	}
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "inbound:" + p.inbound.ID, Role: "entry"}, {InstanceID: "exit", Ref: "outbound:" + out.ID, Role: "exit"}}, Connections: []CanvasConnection{canvasTestHop("entry", "exit", 8443)}}
	input := CanvasSchemeInput{Name: "invalid exit", Execution: "server", Layout: layout}
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", input); err == nil || !strings.Contains(err.Error(), "前一跳") {
		t.Fatalf("wrong owner: %v", err)
	}
	input.Layout.Instances[1].Ref = "outbound:missing"
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", input); err == nil {
		t.Fatal("missing outbound accepted")
	}
	input.Layout.Instances[1].Ref = "outbound:" + out.ID
	if _, err := p.database.Exec("UPDATE nodes SET deleted_at='2026-11-04T00:00:00Z' WHERE id=?", p.nodeB); err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", input); err == nil {
		t.Fatal("deleted node outbound accepted")
	}
	input.Layout.Instances[1].Ref = "server:" + p.nodeA
	input.Layout.Connections = []CanvasConnection{{ID: "gost", Source: "entry", Target: "exit", Type: "tunnel", TunnelProtocol: "gost", SupportTCP: true}}
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", input); err == nil || !strings.Contains(err.Error(), "端口转发与 GOST 隧道已移至隧道管理") {
		t.Fatalf("new GOST: %v", err)
	}
}
