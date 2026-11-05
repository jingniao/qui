package importer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

func TestSS2022ExportCapabilityOmissionsAreIndividual(t *testing.T) {
	_, fixtures := ssFixtures(t)
	ordinary := proxy.Proxy{Name: "ordinary", Server: "panel.invalid", Port: 443, Protocol: "shadowsocks", Method: "aes-256-gcm", Credential: proxy.Credential{Password: "ordinary-fixture"}}
	for _, tc := range []struct {
		method                    string
		size                      int
		identity, mihomo, singbox bool
	}{
		{"2022-blake3-aes-256-gcm", 32, false, true, true},
		{"2022-blake3-aes-256-gcm", 32, true, true, true},
		{"2022-blake3-aes-128-gcm", 16, false, true, true},
		{"2022-blake3-aes-128-gcm", 16, true, false, true},
		{"2022-blake3-chacha20-poly1305", 32, false, true, false},
	} {
		p := fixtures[0]
		p.Method = tc.method
		p.Credential.Password = base64.StdEncoding.EncodeToString(make([]byte, tc.size))
		if tc.identity {
			p.Credential.Password += ":" + p.Credential.Password
		}
		for _, exporter := range []format.Exporter{clash.Meta{}, clash.Legacy{}, singbox.Exporter{}} {
			supported := exporter.ID() == "clash" && tc.mihomo || exporter.ID() == "sing-box" && tc.singbox
			out, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{ordinary, p}})
			if err != nil || !bytes.Contains(out.Body, []byte(ordinary.Credential.Password)) {
				t.Fatalf("%s lost usable endpoint: %v", exporter.ID(), err)
			}
			if bytes.Contains(out.Body, []byte(p.Method)) != supported || (len(out.Warnings) == 0) != supported {
				t.Fatalf("wrong %s capability for %s identity=%t", exporter.ID(), tc.method, tc.identity)
			}
		}
	}
	for _, mutate := range []func(*proxy.Proxy){
		func(p *proxy.Proxy) { p.Credential.Password = "corrupt-fixture" },
		func(p *proxy.Proxy) { p.Security = "tls" },
		func(p *proxy.Proxy) { p.Transport = &proxy.Transport{Type: "ws", Path: "/"} },
		func(p *proxy.Proxy) { p.Flow = "unmodeled" },
		func(p *proxy.Proxy) { p.Method = "2022-unknown" },
	} {
		bad := fixtures[0]
		mutate(&bad)
		for _, exporter := range []format.Exporter{uri.URI{}, uri.Base64{}, clash.Meta{}, clash.Legacy{}, singbox.Exporter{}} {
			if _, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{ordinary, bad}}); err == nil {
				t.Fatalf("%s hid corrupt SS2022 as ordinary omission", exporter.ID())
			}
		}
	}
}

func TestSS2022OmissionsPreserveRejectAndChainDependencies(t *testing.T) {
	_, p := ssFixtures(t)
	p[0].Method = "2022-blake3-aes-128-gcm"
	k := base64.StdEncoding.EncodeToString(make([]byte, 16))
	p[0].Credential.Password = k + ":" + k // unmeasured Mihomo identity
	p[0].ClientChain = "chain"
	p[1].ClientChain, p[1].DialerProxy = "chain", p[0].Name
	template := rules.Default()
	template.Groups = []rules.Group{{Name: "only", Type: "select", Members: []string{rules.MemberAll}, EmptyPolicy: "reject"}}
	resolved := rules.Expand(template, p[:2])
	out, err := (clash.Meta{}).Render(format.Profile{Proxies: p[:2], Rules: &resolved})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Proxies []any `yaml:"proxies"`
		Groups  []struct {
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal(out.Body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Proxies) != 0 || len(doc.Groups) != 1 || strings.Join(doc.Groups[0].Proxies, ",") != "REJECT" {
		t.Fatalf("omission leaked proxy or replaced REJECT: %s", out.Body)
	}
}

func TestSS2022EncodedOutputsContainCompleteCipherAndPassword(t *testing.T) {
	_, entries := ssFixtures(t)
	out, err := (singbox.Exporter{}).Render(format.Profile{Proxies: entries})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []struct {
			Method   string `json:"method"`
			Password string `json:"password"`
		} `json:"outbounds"`
	}
	if json.Unmarshal(out.Body, &doc) != nil || len(doc.Outbounds) != 3 {
		t.Fatal("incomplete sing-box output")
	}
	for i, p := range entries {
		if doc.Outbounds[i].Method != p.Method || doc.Outbounds[i].Password != p.Credential.Password {
			t.Fatal("sing-box changed key/cipher")
		}
	}
}
