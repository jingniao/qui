package proxy

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestCanvasEntryProtocolSettings(t *testing.T) {
	for _, protocol := range []string{"shadowsocks", "trojan", "hysteria2", "tuic", "wireguard"} {
		t.Run(protocol, func(t *testing.T) {
			p := newPublishPanel(t)
			core := "xray"
			if protocol == "hysteria2" || protocol == "tuic" {
				core = "sing-box"
			}
			caps := &pb.Capabilities{CoreName: core, Protocols: []*pb.ProtocolCapability{{Protocol: protocol, Inbound: true, Outbound: true, Tcp: true, Udp: true}, {Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true, Udp: true}, {Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true, Udp: true}}}
			raw, err := protojson.Marshal(caps)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", raw, p.nodeA); err != nil {
				t.Fatal(err)
			}
			scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: protocol, PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}))
			plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
				t.Fatal(err)
			}
			binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
			if err != nil {
				t.Fatal(err)
			}
			in, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, true)
			if err != nil {
				t.Fatal(err)
			}
			switch protocol {
			case "shadowsocks":
				if in.Settings.Method != "chacha20-ietf-poly1305" {
					t.Fatalf("method = %q", in.Settings.Method)
				}
				// Simulate a pre-fix create-mode entry and preserve its historical baseline.
				in.Settings.Method = ""
				sealed, err := p.service.Repository.encode(in.Settings)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.database.Exec("UPDATE inbounds SET encrypted_settings=? WHERE id=?", sealed, in.ID); err != nil {
					t.Fatal(err)
				}
				loaded, err := p.service.Repository.Inbound(p.ctx, in.ID, false)
				if err != nil {
					t.Fatal(err)
				}
				edit := inboundInputOf(loaded, loaded.DefaultOutbound)
				edit.Settings.Method = "aes-128-gcm"
				if _, err := p.service.SaveInbound(p.ctx, in.ID, edit); err != nil {
					t.Fatalf("bound entry repair: %v", err)
				}
				after, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
				if err != nil || !reflect.DeepEqual(binding, after) {
					t.Fatalf("repair changed binding baseline: %v", err)
				}
				if _, _, err := p.service.SnapshotIntent(p.ctx, p.nodeA, p.users); err != nil {
					t.Fatal(err)
				}
			case "wireguard":
				if in.Settings.WireGuard == nil || in.Settings.WireGuard.SecretKey == "" || len(in.Settings.WireGuard.Addresses) == 0 {
					t.Fatal("missing WireGuard material")
				}
			default:
				if in.Settings.Security != "tls" || in.Settings.CertificatePEM == "" || in.Settings.PrivateKeyPEM == "" {
					t.Fatal("missing TLS material")
				}
			}
		})
	}
}

func TestCanvasTLSPlanRequiresCertificateAddress(t *testing.T) {
	p := newPublishPanel(t)
	if _, err := p.database.Exec("UPDATE nodes SET public_ip='',connect_host='' WHERE id=?", p.nodeA); err != nil {
		t.Fatal(err)
	}
	scheme := CanvasScheme{Execution: "server", Layout: entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "trojan", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})}
	if _, err := p.service.canvasBuildPlan(p.ctx, scheme); err == nil || !strings.Contains(err.Error(), "自签名证书") {
		t.Fatalf("plan: %v", err)
	}
}

func TestCanvasDependencyConflictHTTP(t *testing.T) {
	w := httptest.NewRecorder()
	(&Handler{}).failCanvas(w, fmt.Errorf("撤除失败：%w", ErrRelayDependency))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "dependency_not_ready") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// Tunnel publish starts its batch with the same relay dependency check; joined
// with the preparation abort it must still surface as 409, not a bare 500.
func TestTunnelDependencyConflictHTTP(t *testing.T) {
	w := httptest.NewRecorder()
	cause := fmt.Errorf("%w: 中转出站「out」→ 目标节点「node」：目标节点运行中的内部监听不匹配 (internal_inbound_unavailable)", ErrRelayDependency)
	(&Handler{}).failTunnel(w, errors.Join(cause, nil))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "dependency_not_ready") || !strings.Contains(w.Body.String(), "internal_inbound_unavailable") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// An Agent refusing a removal phase is a conflict naming the node, not a 400 with raw IDs.
func TestShadowsocksEmptyMethodSaveAndSnapshot(t *testing.T) {
	p := newPublishPanel(t)
	raw, err := protojson.Marshal(&pb.Capabilities{CoreName: "xray", Protocols: []*pb.ProtocolCapability{{Protocol: "shadowsocks", Inbound: true, Outbound: true, Tcp: true}, {Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", raw, p.nodeA); err != nil {
		t.Fatal(err)
	}
	input := InboundInput{Name: "旧入口", NodeID: p.nodeA, Protocol: "shadowsocks", Listen: "0.0.0.0", Port: 20000, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"}
	in, err := p.service.SaveInbound(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	state, intent, err := p.service.SnapshotIntent(p.ctx, p.nodeA, p.users)
	if err != nil {
		t.Fatalf("disabled snapshot: %v", err)
	}
	found := false
	for _, rendered := range state.Inbounds {
		if rendered.ID == in.ID {
			found = true
			if rendered.Enabled {
				t.Fatal("credentialless inbound enabled")
			}
		}
	}
	if !found || !strings.Contains(strings.Join(intent, ","), in.ID) {
		t.Fatal("missing inbound or enabled intent")
	}
	group, err := (&node.Repository{Database: p.database}).SaveGroup(p.ctx, "", node.GroupInput{Name: "SS users", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: []string{in.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.users.Save(p.ctx, "", user.Input{Username: "ss-user", Status: "active", NodeGroupIDs: []string{group.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.service.SnapshotIntent(p.ctx, p.nodeA, p.users); err != nil {
		t.Fatalf("enabled snapshot: %v", err)
	}
	input.Settings = desiredstate.ProtocolSettings{Method: "chacha20-ietf-poly1305"}
	if _, err := p.service.SaveInbound(p.ctx, in.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.service.SnapshotIntent(p.ctx, p.nodeA, p.users); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteCanvasBoundAndDetachedInbound(t *testing.T) {
	for _, detached := range []bool{false, true} {
		t.Run(fmt.Sprintf("detached=%t", detached), func(t *testing.T) {
			p, scheme, binding := cleanupFixture(t)
			if detached {
				if _, err := p.database.Exec("DELETE FROM canvas_schemes WHERE id=?", scheme.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := p.service.DeleteInbound(p.ctx, binding.InboundID); err != nil {
				t.Fatal(err)
			}
			if _, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, false); !errors.Is(err, ErrNotFound) {
				t.Fatalf("entry survived: %v", err)
			}
			if _, err := p.service.Repository.CanvasEntryBindingByInbound(p.ctx, binding.InboundID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("binding survived: %v", err)
			}
			if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("scheme survived: %v", err)
			}
		})
	}
}

func TestPendingDeleteRuleTargetListing(t *testing.T) {
	p := newPublishPanel(t)
	r := p.service.Repository
	tunnelTestRange(t, p.canvasPanel, p.nodeA, p.nodeB)
	rule, err := p.service.SaveTunnelRule(p.ctx, "", TunnelRuleInput{Name: "待删除关联规则", EntryNodeID: p.nodeB, PortMode: "auto", Network: "tcp", Target: TunnelTarget{Mode: "linked", InboundID: p.inbound.ID}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	markManagedPublished(t, p.canvasPanel, "tunnel_rules", rule.ID)
	if err := p.service.DeleteTunnelRule(p.ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	for _, missing := range []bool{false, true} {
		if missing {
			// A deleted target server makes derivation fail, but removal stays readable.
			if _, err := p.database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", timestamp(), p.nodeA); err != nil {
				t.Fatal(err)
			}
		}
		one, err := r.TunnelRule(p.ctx, rule.ID)
		if err != nil {
			t.Fatal(err)
		}
		all, err := r.TunnelRules(p.ctx, "", "")
		if err != nil || len(all) != 1 {
			t.Fatalf("list: %v", err)
		}
		for _, got := range []TunnelRule{one, all[0]} {
			if got.Target.InboundID != p.inbound.ID || got.Status != "pending_delete" {
				t.Fatal(got)
			}
			if !missing && (got.Target.Protocol != "socks5" || got.Target.NodeName == "" || got.Target.Port == 0) {
				t.Fatalf("target not derived: %+v", got.Target)
			}
		}
	}
}

// A dependency failure must keep naming the blocking outbound, target and reason.
