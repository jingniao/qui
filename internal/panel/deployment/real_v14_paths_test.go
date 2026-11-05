package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/testutil"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

var v14Paths = []string{"P1", "P2-inbound", "P2-external", "P3", "P4", "P4-three-implicit-SNI"}

type v14Template struct{ value rules.Template }

func (r v14Template) Template(ctx context.Context) (rules.Template, error) {
	return r.value, nil
}

type v14Fixture struct {
	h         *gostHosts
	states    []desiredstate.State
	profile   []byte
	name      string
	mixedPort int
	ca        string
	ports     map[int]bool
}

// Reserve distinct numbers across simulated hosts as well as within each host.
// The listeners are released before the real cores start, like the existing
// real-core fixtures; bind failures are errors, never an allowed CI skip.
func (f *v14Fixture) port(t *testing.T) int {
	t.Helper()
	for {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := l.Addr().(*net.TCPAddr).Port
		l.Close()
		if !f.ports[p] {
			f.ports[p] = true
			return p
		}
	}
}

func v14Must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// This same builder runs without binaries: it catches panel/subscription fixture
// regressions locally instead of hiding them behind the real-core skip.
func newV14Fixture(t *testing.T, path, primary string) *v14Fixture {
	t.Helper()
	f := &v14Fixture{h: newGostHosts(t), ports: map[int]bool{}}
	h := f.h
	nodes := &node.Repository{Database: h.database}
	for i, id := range []string{h.alpine, h.uk, h.us2} {
		v14Must(t, nodes.SetPublicIP(h.ctx, id, "127.0.0.1"))
		caps := &pb.Capabilities{CoreName: primary, MultiCoreSupported: true, ForwardInboundSupported: true,
			Cores: []*pb.CoreCapability{{Name: primary, ForwardInboundSupported: true,
				Protocols: []*pb.ProtocolCapability{{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true}, {Protocol: "socks5", Inbound: true, Outbound: true, Tcp: true}, {Protocol: "trojan", Inbound: true, Outbound: true, Tcp: true}}},
				{Name: "gost", ForwardInboundSupported: true, GostContractVersion: desiredstate.GostContractVersion, Protocols: []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true, Tcp: true, Udp: true}}}}}
		raw, err := json.Marshal(caps)
		v14Must(t, err)
		_, err = h.database.Exec("UPDATE nodes SET name=?,capabilities_json=? WHERE id=?", []string{"HK", "JP", "US"}[i], string(raw), id)
		v14Must(t, err)
		// Canvas may allocate an exit's internal SOCKS listener. A single free
		// port is a valid configured range, and cannot overlap another host.
		p := f.port(t)
		_, err = h.database.Exec("INSERT INTO node_meta(node_id,port_range_start,port_range_end) VALUES(?,?,?)", id, p, p)
		v14Must(t, err)
	}
	pki := testutil.Certificates(t)
	f.ca = pki.Server.ClientCA
	cert, err := os.ReadFile(pki.Server.Certificate)
	v14Must(t, err)
	key, err := os.ReadFile(pki.Server.Key)
	v14Must(t, err)
	settings := desiredstate.ProtocolSettings{Transport: "tcp", Security: "tls", ServerName: "localhost", CertificatePEM: string(cert), PrivateKeyPEM: string(key)}
	if strings.Contains(path, "implicit") {
		settings.ServerName = ""
	}
	entryNode := h.alpine
	if path == "P1" || strings.HasPrefix(path, "P4") {
		entryNode = h.us2
	}
	entry, err := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{Name: "user VLESS", NodeID: entryNode, Protocol: "vless", Listen: "0.0.0.0", Port: f.port(t), Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", Settings: settings})
	v14Must(t, err)
	group, err := nodes.SaveGroup(h.ctx, "", node.GroupInput{Name: "user entries", Purpose: "subscription", Enabled: true, SubscriptionVisible: true, InboundIDs: []string{entry.ID}})
	v14Must(t, err)
	u, err := h.users.Save(h.ctx, "", user.Input{Username: "a1a_user", DisplayName: "A1a", Status: "active", AllowedProtocols: []string{"vless"}, NodeGroupIDs: []string{group.ID}})
	v14Must(t, err)
	device, err := h.users.CreateDevice(h.ctx, u.ID, user.DeviceInput{Name: "client", Protocols: []string{"vless"}})
	v14Must(t, err)
	publishDirect := func() proxy.CanvasPublishResult {
		state, intent, e := h.service.SnapshotIntent(h.ctx, entryNode, h.users)
		v14Must(t, e)
		return h.publish("direct VLESS", []proxy.NodeSnapshot{{State: state, InboundIntent: intent, ExpandedOnly: true}})
	}
	wantPort := entry.Port
	if path == "P1" {
		publishDirect()
		if again := publishDirect(); len(again.Nodes) != 0 || again.BatchID != "" {
			t.Fatal("direct republish created work", again)
		}
	} else if strings.HasPrefix(path, "P4") {
		publishDirect()
		hops := []proxy.TunnelHop{{Role: "entry", NodeID: h.alpine, PortMode: "auto"}}
		exitNode := h.uk
		if strings.Contains(path, "three") {
			hops = append(hops, proxy.TunnelHop{Role: "mid", NodeID: h.uk, PortMode: "manual", ListenPort: portValue(f.port(t))})
			exitNode = h.us2
		}
		hops = append(hops, proxy.TunnelHop{Role: "exit", NodeID: exitNode, PortMode: "manual", ListenPort: portValue(f.port(t))})
		tunnel, e := h.service.SaveTunnel(h.ctx, "", proxy.TunnelInput{Name: path, Transport: "tls", Hops: hops})
		v14Must(t, e)
		wantPort = f.port(t)
		rule, e := h.service.CreateNodeRelay(h.ctx, h.us2, proxy.NodeRelayInput{TunnelID: tunnel.ID, InboundID: entry.ID, PortMode: "manual", ListenPort: portValue(wantPort), SubscriptionVisible: true})
		v14Must(t, e)
		selection := proxy.PublishSelection{RuleIDs: []string{rule.ID}}
		result, _ := driveTunnelPublish(t, h, selection, "")
		for _, s := range result.Servers {
			if s.Status != "applied" {
				t.Fatal(result)
			}
		}
		again, order := driveTunnelPublish(t, h, selection, "")
		if len(order) != 0 || again.BatchID != "" {
			t.Fatal("tunnel republish created work", again, order)
		}
		for _, s := range again.Servers {
			if s.Status != "unchanged" || s.Revision != nil {
				t.Fatal(again)
			}
		}
	} else {
		layout := proxy.CanvasLayout{Instances: []proxy.CanvasInstance{{InstanceID: "hk", Ref: "inbound:" + entry.ID, Role: "entry"}}}
		previous, previousNode := "hk", h.alpine
		if path == "P3" {
			layout.Instances = append(layout.Instances, proxy.CanvasInstance{InstanceID: "jp", Ref: "server:" + h.uk, Role: "relay", RelayPortMode: "manual", RelayPort: portValue(f.port(t))})
			layout.Connections = append(layout.Connections, proxy.CanvasConnection{ID: "hk-jp", Source: "hk", Target: "jp", Type: "tunnel", TunnelProtocol: "tls", SupportTCP: true})
			previous, previousNode = "jp", h.uk
		}
		if path == "P2-external" {
			p := f.port(t)
			out, e := h.service.SaveOutbound(h.ctx, "", proxy.OutboundInput{Name: "external SOCKS5", NodeID: previousNode, Protocol: "socks5", Address: "127.0.0.1", Port: p, Username: "external", Secret: "external-secret", Enabled: true, SupportTCP: true, FailurePolicy: "Block"})
			v14Must(t, e)
			layout.Instances = append(layout.Instances, proxy.CanvasInstance{InstanceID: "us", Ref: "outbound:" + out.ID, Role: "exit"})
			// An independent real core stands in for a proxy outside the panel.
			f.states = append(f.states, desiredstate.State{
				SchemaVersion: 1, Revision: 1, NodeID: "external", Logging: desiredstate.Logging{DomainOnly: true},
				Credentials: []desiredstate.Credential{{ID: "external-auth", Protocol: "socks5", Username: "external", Secret: "external-secret", Internal: true}},
				Inbounds:    []desiredstate.Inbound{{ID: "external-socks", Core: primary, Protocol: "socks5", Listen: "127.0.0.1", Port: p, Enabled: true, CredentialIDs: []string{"external-auth"}, DefaultOutbound: "direct"}},
				Outbounds:   []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true, SupportTCP: true}},
			})
		} else {
			in, e := h.service.SaveInbound(h.ctx, "", proxy.InboundInput{Name: "US SOCKS5", NodeID: h.us2, Protocol: "socks5", Listen: "0.0.0.0", Port: f.port(t), Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct"})
			v14Must(t, e)
			layout.Instances = append(layout.Instances, proxy.CanvasInstance{InstanceID: "us", Ref: "inbound:" + in.ID, Role: "exit"})
		}
		layout.Connections = append(layout.Connections, proxy.CanvasConnection{ID: "us-exit", Source: previous, Target: "us", Type: "proxy_chain", TunnelProtocol: "tls", SupportTCP: true})
		scheme, e := h.service.SaveCanvasScheme(h.ctx, "", proxy.CanvasSchemeInput{Name: path, Execution: "server", Layout: layout})
		v14Must(t, e)
		plan, e := h.service.PlanCanvasScheme(h.ctx, scheme.ID, nil)
		v14Must(t, e)
		if !plan.OK {
			t.Fatal(plan)
		}
		h.publishScheme(scheme)
		if again := h.publishScheme(scheme); len(again.Nodes) != 0 || again.BatchID != "" {
			t.Fatal("canvas republish created work", again)
		}
	}
	for _, id := range []string{h.us2, h.uk, h.alpine} {
		state, e := h.repository.Published(h.ctx, id)
		if e == sql.ErrNoRows {
			continue
		}
		v14Must(t, e)
		v14Must(t, state.Validate())
		f.states = append(f.states, state)
		for _, in := range h.inbounds(id) {
			if (in.InternalRelay || in.ManagedBy != nil) && in.SubscriptionVisible {
				t.Fatalf("generated/internal inbound %s is subscription visible", in.ID)
			}
		}
	}
	for _, s := range f.states {
		v14Must(t, s.Validate())
	}
	sub := &subscription.Service{Repository: &subscription.Repository{Database: h.database, Vault: h.repository.Vault}, Users: h.users,
		Sources: source.NewRegistry(&published.Source{Users: h.users, Nodes: nodes, Published: h.repository, Relays: &published.RelayStore{Database: h.database}})}
	proxies, warnings, err := sub.Proxies(h.ctx, u.ID, device.ID)
	v14Must(t, err)
	wantCount := 1
	if strings.HasPrefix(path, "P4") {
		wantCount = 2
	}
	if len(proxies) != wantCount || len(warnings) != 0 {
		t.Fatalf("subscription proxies=%d want=%d warnings=%v", len(proxies), wantCount, warnings)
	}
	var secret string
	for _, s := range f.states {
		for _, c := range s.Credentials {
			if c.DeviceID == device.ID && c.Protocol == "vless" {
				secret = c.Secret
			}
		}
	}
	if secret == "" {
		t.Fatal("no published user VLESS credential")
	}
	for _, p := range proxies {
		if p.Protocol != "vless" || p.Server != "127.0.0.1" || p.Credential.Password != secret || p.Security != "tls" || p.TLS == nil {
			t.Fatalf("incorrect subscription endpoint: %s", p.Name)
		}
		if p.Port == wantPort {
			f.name = p.Name
		}
		if p.Origin.Relayed {
			wantSNI := settings.ServerName
			if wantSNI == "" {
				wantSNI = "127.0.0.1"
			}
			if p.Port != wantPort || p.TLS.ServerName != wantSNI {
				t.Fatalf("relayed port/SNI %d/%s", p.Port, p.TLS.ServerName)
			}
		} else if p.Port != entry.Port || p.Origin.Ref != entry.ID {
			t.Fatal("internal or generated inbound leaked into subscription")
		}
	}
	if f.name == "" {
		t.Fatal("expected subscription entry absent")
	}
	expectedName := "HK · vless"
	if path == "P1" {
		expectedName = "US · vless"
	}
	if strings.HasPrefix(path, "P4") {
		expectedName = "US · vless · 经 HK"
	}
	if f.name != expectedName {
		t.Fatalf("subscription name=%q want=%q", f.name, expectedName)
	}
	f.mixedPort = f.port(t)
	template := rules.Default()
	template.Client.MixedPort = f.mixedPort
	template.Client.AllowLAN = false
	template.Groups = []rules.Group{{Name: "A1a", Type: "select", Members: []string{rules.MemberAll}}}
	template.Rules = []rules.Rule{{ID: "final", Type: rules.RuleMatch, Target: "A1a", Enabled: true, Priority: 1}}
	sub.Rules = v14Template{template}
	_, token, e := sub.Rotate(h.ctx, u.ID, device.ID)
	v14Must(t, e)
	rendered, e := sub.Render(h.ctx, token, "mihomo", "", nil)
	v14Must(t, e)
	var doc map[string]any
	v14Must(t, yaml.Unmarshal(rendered.Body, &doc))
	entries := doc["proxies"].([]any)
	if len(entries) != wantCount {
		t.Fatal("mihomo exporter lost or exposed an endpoint")
	}
	for _, raw := range entries {
		p := raw.(map[string]any)
		if p["server"] != "127.0.0.1" || p["uuid"] != secret || p["tls"] != true || p["type"] != "vless" {
			t.Fatal("incorrect mihomo server/credential/TLS fields")
		}
		if p["skip-cert-verify"] == true {
			t.Fatal("mihomo disabled TLS verification")
		}
		matched := false
		for _, endpoint := range proxies {
			if p["name"] != endpoint.Name {
				continue
			}
			matched = true
			if p["port"] != endpoint.Port {
				t.Fatal("incorrect mihomo port")
			}
			if endpoint.TLS.ServerName != "" && p["servername"] != endpoint.TLS.ServerName {
				t.Fatalf("mihomo servername=%v want=%s", p["servername"], endpoint.TLS.ServerName)
			}
		}
		if !matched {
			t.Fatal("unexpected mihomo proxy name")
		}
	}
	// Selection is by the exact exported name. This is client configuration,
	// leaving every subscription proxy and its TLS/credential fields untouched.
	doc["proxy-groups"] = []any{map[string]any{"name": "A1a", "type": "select", "proxies": []string{f.name}}}
	f.profile, e = yaml.Marshal(doc)
	v14Must(t, e)
	return f
}

func TestV14PathPanelFixtures(t *testing.T) {
	for _, primary := range []string{"xray", "sing-box"} {
		for _, path := range v14Paths {
			t.Run(fmt.Sprintf("%s/%s", primary, path), func(t *testing.T) { newV14Fixture(t, path, primary) })
		}
	}
}
