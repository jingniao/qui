package proxy

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestCanvasEntrySecurityValidation(t *testing.T) {
	for _, tc := range []struct {
		fields string
		valid  bool
	}{
		{``, true}, {`,"security":"none"`, true}, {`,"security":"reality","realityTarget":"www.example.com:443","realityServerName":"www.example.com"`, true},
		{`,"security":"tls","transport":"xhttp"`, true}, {`,"security":"bad"`, false}, {`,"transport":"ws"`, false},
		{`,"security":"tls","flow":"xtls-rprx-vision"`, true}, {`,"flow":"unknown"`, false},
		{`,"security":"tls","realityServerName":"www.example.com"`, false},
		{`,"transport":"xhttp"`, false}, {`,"flow":"xtls-rprx-vision"`, false}, {`,"security":"tls","transport":"xhttp","flow":"xtls-rprx-vision"`, false},
		{`,"security":"reality"`, false}, {`,"security":"tls","realityTarget":"www.example.com:443"`, false},
		{`,"security":"reality","realityTarget":"www.example.com","realityServerName":"www.example.com"`, false},
		{`,"security":"reality","realityTarget":"www.example.com:443","realityServerName":"*.example.com"`, false},
		{`,"security":"reality","realityTarget":"www.example.com:443","realityServerName":"1.2.3.4"`, false},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			var b CanvasEntryBinding
			if err := json.Unmarshal([]byte(`{"mode":"create","protocol":"vless","portMode":"auto","groupMode":"none"`+tc.fields+`}`), &b); err != nil {
				t.Fatal(err)
			}
			layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "server:a", Role: "entry", EntryBinding: &b}}}
			if err := layout.validateEntryBindings("server"); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
			if tc.valid && tc.fields != "" {
				b.Protocol = "socks5"
				if err := layout.validateEntryBindings("server"); err == nil {
					t.Fatal("non-vless accepted security fields")
				}
			}
		})
	}
}

func canvasEntrySecurityCapabilities(t *testing.T, p *publishPanel, core string, contract bool) {
	t.Helper()
	n, err := p.service.Nodes.Get(p.ctx, p.nodeA)
	if err != nil {
		t.Fatal(err)
	}
	caps := n.Capabilities
	caps.CoreName = core
	caps.Protocols = append(caps.Protocols, &pb.ProtocolCapability{Protocol: "vless", Inbound: true, Outbound: true, Tcp: true, Udp: true})
	if contract {
		caps.ServerSettingsContractVersion = desiredstate.SettingsContractVersion
		for _, transport := range []string{"tcp", "xhttp"} {
			for _, flow := range []string{"", "xtls-rprx-vision"} {
				if transport == "xhttp" && (core == "sing-box" || flow != "") {
					continue
				}
				caps.ServerSettingsCombinations = append(caps.ServerSettingsCombinations, &pb.SettingsCombination{Protocol: "vless", Direction: "inbound", Security: "reality", Transport: transport, Flow: flow})
			}
		}
		if core == "xray" {
			caps.ServerSettingsCombinations = append(caps.ServerSettingsCombinations, &pb.SettingsCombination{Protocol: "vless", Direction: "inbound", Security: "tls", Transport: "xhttp"})
		}
	}
	raw, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", string(raw), p.nodeA); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasEntrySecurityMaterialization(t *testing.T) {
	for _, tc := range []struct{ security, transport, flow string }{
		{"", "", ""}, {"none", "tcp", ""}, {"tls", "tcp", "xtls-rprx-vision"}, {"tls", "xhttp", ""},
		{"reality", "tcp", ""}, {"reality", "tcp", "xtls-rprx-vision"}, {"reality", "xhttp", ""},
	} {
		t.Run(tc.security+tc.transport+tc.flow, func(t *testing.T) {
			p := newPublishPanel(t)
			canvasEntrySecurityCapabilities(t, p, "xray", true)
			b := &CanvasEntryBinding{Mode: "create", Protocol: "vless", Security: tc.security, Transport: tc.transport, Flow: tc.flow, PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}
			if tc.security == "reality" {
				b.RealityTarget = "www.example.com:443"
				b.RealityServerName = "www.example.com"
			}
			scheme := savePlanScheme(t, p, entryPlanLayout(p, b))
			plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
				t.Fatal(err)
			}
			record, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
			if err != nil {
				t.Fatal(err)
			}
			in, err := p.service.Repository.Inbound(p.ctx, record.InboundID, true)
			if err != nil {
				t.Fatal(err)
			}
			settings := in.Settings
			if settings.Security != tc.security || settings.Transport != tc.transport || settings.Flow != tc.flow {
				t.Fatalf("combination %s / %s / %s", settings.Security, settings.Transport, settings.Flow)
			}
			if err := desiredstate.ValidateSettings("vless", settings, true); err != nil {
				t.Fatal(err)
			}
			if tc.security == "tls" && (in.TLSSource != TLSSourceSelfSigned || settings.CertificatePEM == "" || settings.PrivateKeyPEM == "") {
				t.Fatal("missing self-signed certificate")
			}
			if tc.security == "reality" {
				r := settings.Reality
				if r == nil || r.PrivateKey == "" || r.PublicKey == "" || len(r.ShortIDs) != 1 || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(r.ShortIDs[0]) {
					t.Fatalf("missing Reality key pair or Short ID")
				}
			}
			if tc.transport == "xhttp" && (settings.XHTTP == nil || settings.XHTTP.Mode != "auto" || settings.XHTTP.Host != "" || !regexp.MustCompile(`^/[a-z0-9]{8,12}$`).MatchString(settings.XHTTP.Path)) {
				t.Fatalf("xhttp %+v", settings.XHTTP)
			}
			if tc.security == "" && !reflect.DeepEqual(settings, desiredstate.ProtocolSettings{}) {
				t.Fatal("legacy entry changed")
			}
			if err := ValidateCanvasEntryBaseline(record.CreationConfigJSON, record.CreationGroupsJSON); err != nil {
				t.Fatal(err)
			}
			// A stale or invalid security edit in the layout cannot alter a published entry.
			scheme.Layout.Instances[0].EntryBinding.Security = "changed"
			scheme.Layout.Instances[0].EntryBinding.Transport = "changed"
			plan, err = p.service.canvasBuildPlan(p.ctx, scheme)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = p.service.canvasApplyPlan(p.ctx, plan); err != nil {
				t.Fatal(err)
			}
			after, err := p.service.Repository.Inbound(p.ctx, record.InboundID, true)
			if err != nil || !reflect.DeepEqual(after.Settings, settings) {
				t.Fatalf("published settings changed: %v", err)
			}
			if _, err := p.service.SaveCanvasScheme(p.ctx, scheme.ID, CanvasSchemeInput{Name: scheme.Name, Execution: scheme.Execution, Layout: scheme.Layout, UpdatedAt: scheme.UpdatedAt}); err != nil {
				t.Fatal(err)
			}
			if err := ValidateCanvasTunnels(p.ctx, p.database); err != nil {
				t.Fatalf("saved layout rejected by archive inspection: %v", err)
			}
			projected := &CanvasEntryBinding{Protocol: "vless"}
			canvasEntrySecurityFields(projected, settings)
			wire, _ := json.Marshal(projected)
			if strings.Contains(string(wire), "private_key") || projected.Security != settings.Security || tc.security == "reality" && projected.RealityTarget != b.RealityTarget {
				t.Fatalf("unsafe projection %s", wire)
			}
		})
	}
}

func TestCanvasEntrySecurityTLSRequiresAddress(t *testing.T) {
	p := newPublishPanel(t)
	canvasEntrySecurityCapabilities(t, p, "xray", true)
	if _, err := p.database.Exec("UPDATE nodes SET public_ip='',connect_host='' WHERE id=?", p.nodeA); err != nil {
		t.Fatal(err)
	}
	layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "vless", Security: "tls", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
	result, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "TLS address", Execution: "server", Layout: layout})
	if err != nil || result.OK || !strings.Contains(result.Message, "无法为 vless 入口生成自签名证书") {
		t.Fatalf("plan %+v: %v", result, err)
	}
}

func TestCanvasEntrySecurityPlanCapability(t *testing.T) {
	for _, tc := range []struct {
		core, transport string
		contract, ok    bool
	}{
		{"xray", "tcp", false, false}, {"sing-box", "xhttp", true, false}, {"sing-box", "tcp", true, true}, {"xray", "xhttp", true, true},
	} {
		t.Run(tc.core+tc.transport, func(t *testing.T) {
			p := newPublishPanel(t)
			canvasEntrySecurityCapabilities(t, p, tc.core, tc.contract)
			layout := entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "vless", Security: "reality", Transport: tc.transport, RealityTarget: "www.example.com:443", RealityServerName: "www.example.com", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"})
			result, err := p.service.PlanCanvas(p.ctx, CanvasScheme{Name: "security", Execution: "server", Layout: layout})
			if err != nil || result.OK != tc.ok {
				t.Fatalf("plan %+v: %v", result, err)
			}
			if !tc.ok && (!strings.Contains(result.Message, "Node A") || !strings.Contains(result.Message, "reality")) {
				t.Fatalf("missing server/combination: %s", result.Message)
			}
		})
	}
}

func TestQuickCanvasSecurityFieldsAndRetry(t *testing.T) {
	p, input := quickCanvasFixture(t)
	canvasEntrySecurityCapabilities(t, p, "xray", true)
	input.EntryProtocol = "vless"
	input.EntrySecurity = "reality"
	input.EntryTransport = "tcp"
	input.EntryFlow = "xtls-rprx-vision"
	input.EntryRealityTarget = "www.example.com:443"
	input.EntryRealityServerName = "www.example.com"
	scheme, err := p.service.quickCanvasScheme(p.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	b := scheme.Layout.Instances[0].EntryBinding
	if b.Security != input.EntrySecurity || b.Transport != input.EntryTransport || b.Flow != input.EntryFlow || b.RealityTarget != input.EntryRealityTarget || b.RealityServerName != input.EntryRealityServerName {
		t.Fatalf("binding %+v", b)
	}
	first, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.service.QuickCanvas(p.ctx, input, "Owner", p.users)
	if err != nil || first.SchemeID != second.SchemeID || first.Publication.Entries[0].InboundID != second.Publication.Entries[0].InboundID || p.deployer.calls != 1 {
		t.Fatalf("retry %+v: %v", second, err)
	}
}

func TestCanvasEntryPlaintextWarning(t *testing.T) {
	p := newPublishPanel(t)
	entryPlanRange(t, p, p.nodeA, 20000, 20002)
	result := entryPlanResult(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"}))
	if !strings.Contains(strings.Join(result.Warnings, ""), "使用不加密的 SOCKS5") {
		t.Fatalf("warnings %v", result.Warnings)
	}
}
