package deployment

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// Exercise real deployment revisions and Agent apply receipts for the C1 paths.
func TestCanvasC1PathsPublishAndRepublish(t *testing.T) {
	for _, path := range []string{"P2-inbound", "P2-external", "P3-inbound", "P3-external"} {
		t.Run(path, func(t *testing.T) {
			h := newGostHosts(t)
			for _, nodeID := range []string{h.alpine, h.uk, h.us2} {
				n, err := h.service.Nodes.Get(h.ctx, nodeID)
				if err != nil {
					t.Fatal(err)
				}
				n.Capabilities.Cores[0].Protocols = append(n.Capabilities.Cores[0].Protocols,
					&pb.ProtocolCapability{Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true, Udp: true},
					&pb.ProtocolCapability{Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true, Udp: true})
				raw, _ := json.Marshal(n.Capabilities)
				if _, err := h.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), nodeID); err != nil {
					t.Fatal(err)
				}
			}
			entry, err := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{Name: "HK VLESS", NodeID: h.alpine, Protocol: "vless", Listen: "0.0.0.0", Port: 10443, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
			if err != nil {
				t.Fatal(err)
			}
			group, err := (&node.Repository{Database: h.database}).SaveGroup(h.ctx, "", node.GroupInput{Name: "HK entry", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: []string{entry.ID}})
			if err != nil {
				t.Fatal(err)
			}
			u, err := h.users.Save(h.ctx, "", user.Input{Username: "c1_user", DisplayName: "C1", Status: "active", AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{group.ID}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.users.CreateDevice(h.ctx, u.ID, user.DeviceInput{Name: "C1 client", Protocols: []string{"vless"}}); err != nil {
				t.Fatal(err)
			}
			layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "hk", Ref: "inbound:" + entry.ID, Role: "entry"}}}
			previous, previousNode := "hk", h.alpine
			order := []string{h.us2, h.alpine}
			if strings.HasPrefix(path, "P3") {
				layout.Instances = append(layout.Instances, proxy.CanvasInstance{InstanceID: "jp", Ref: "server:" + h.uk, Role: "relay", RelayPortMode: "manual", RelayPort: portValue(30200)})
				layout.Connections = append(layout.Connections, proxy.CanvasConnection{ID: "hk-jp", Source: "hk", Target: "jp", Type: "tunnel", TunnelProtocol: "tls", SupportTCP: true})
				previous, previousNode = "jp", h.uk
				order = []string{h.us2, h.uk, h.alpine}
			}
			if strings.HasSuffix(path, "external") {
				out, err := h.service.SaveOutbound(h.ctx, "", proxy.OutboundInput{Name: "external US SOCKS5", NodeID: previousNode, Protocol: "socks5", Address: "198.51.100.9", Port: 1080, Username: "relay", Secret: "external-secret", Enabled: true, SupportTCP: true, FailurePolicy: "Block"})
				if err != nil {
					t.Fatal(err)
				}
				layout.Instances = append(layout.Instances, proxy.CanvasInstance{InstanceID: "us", Ref: "outbound:" + out.ID, Role: "exit"})
				order = order[1:]
			} else {
				in, err := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{Name: "US SOCKS5", NodeID: h.us2, Protocol: "socks5", Listen: "0.0.0.0", Port: 1080, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
				if err != nil {
					t.Fatal(err)
				}
				layout.Instances = append(layout.Instances, proxy.CanvasInstance{InstanceID: "us", Ref: "inbound:" + in.ID, Role: "exit"})
			}
			layout.Connections = append(layout.Connections, proxy.CanvasConnection{ID: "us-exit", Source: previous, Target: "us", Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true})
			scheme, err := h.service.SaveCanvasScheme(h.ctx, "", proxy.CanvasSchemeInput{Name: path, Execution: "server", Layout: layout})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := h.service.PlanCanvasScheme(h.ctx, scheme.ID, nil)
			if err != nil || !plan.OK {
				t.Fatalf("plan %+v %v", plan, err)
			}
			result := h.publishScheme(scheme)
			if got := nodeOrder(result); !reflect.DeepEqual(got, order) {
				t.Fatalf("order %v want %v", got, order)
			}
			state, err := h.repository.Published(h.ctx, h.alpine)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, in := range state.Inbounds {
				if in.ID == entry.ID {
					found = in.Enabled && len(in.CredentialIDs) > 0 && in.DefaultOutbound != "direct"
				}
			}
			if !found {
				t.Fatalf("VLESS entry is not enabled and routed: %+v", state.Inbounds)
			}
			for _, nodeID := range order {
				for _, in := range h.inbounds(nodeID) {
					if strings.HasPrefix(in.Name, "["+path+"]") && (!in.InternalRelay || in.SubscriptionVisible) {
						t.Fatalf("exposed internal relay: %+v", in)
					}
				}
			}
			before := h.revision(h.alpine)
			again := h.publishScheme(scheme)
			if len(again.Nodes) != 0 || again.BatchID != "" || h.revision(h.alpine) != before {
				t.Fatalf("republish created revisions: %+v", again)
			}
		})
	}
}
