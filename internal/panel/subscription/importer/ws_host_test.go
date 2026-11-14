package importer

import (
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"strings"
	"testing"
)

func TestClashLocalOptions(t *testing.T) {
	for option, accepted := range map[string]bool{"tfo: true": true, "ip-version: ipv4": true, "packet-encoding: xudp": true, "udp-over-tcp: false": true, "udp-over-tcp: true": false, "unknown: true": false} {
		t.Run(option, func(t *testing.T) {
			result, entries := Analyze([]byte("proxies:\n  - name: ws\n    type: trojan\n    server: example.com\n    port: 443\n    password: secret\n    " + option + "\n"))
			if accepted {
				if len(entries) != 1 {
					t.Fatalf("%+v", result)
				}
			} else if result.Diagnostics[0].Code != "unsupported_option" {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestWSHostImportExport(t *testing.T) {
	inputs := []string{"trojan://secret@example.com:443?type=ws&host=cdn.example.com", "proxies:\n  - name: ws\n    type: trojan\n    server: example.com\n    port: 443\n    password: secret\n    network: ws\n    ws-opts:\n      headers:\n        Host: cdn.example.com\n"}
	inputs = append(inputs, strings.ReplaceAll(inputs[1], "Host:", "host:"))
	for _, input := range inputs {
		result, entries := Analyze([]byte(input))
		if len(entries) != 1 {
			t.Fatalf("%+v", result)
		}
		p := entries[0]
		if p.Transport.Host != "cdn.example.com" {
			t.Fatalf("%+v", p.Transport)
		}
		link, err := uri.Link(p)
		if err != nil || !strings.Contains(link, "host=cdn.example.com") {
			t.Fatalf("%s %v", link, err)
		}
		for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}} {
			out, err := exporter.Render(format.Profile{Proxies: []proxy.Proxy{p}})
			if err != nil || !strings.Contains(string(out.Body), "cdn.example.com") {
				t.Fatalf("%s %v", out.Body, err)
			}
		}
	}
}
