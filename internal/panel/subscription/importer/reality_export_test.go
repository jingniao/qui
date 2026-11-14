package importer

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
)

func TestMeasuredRealityExporterFields(t *testing.T) {
	for _, transport := range []string{"tcp", "vision", "grpc", "auto", "stream-one", "packet-up", "stream-up", "h2", "tls"} {
		t.Run(transport, func(t *testing.T) {
			_, entries := Analyze([]byte(realityURI("tcp", "")))
			p := entries[0]
			spx := "/private-spider"
			p.Reality.SpiderX = &spx
			p.TLS.ALPN = nil
			switch transport {
			case "vision":
				p.Flow = "xtls-rprx-vision"
			case "grpc":
				p.Transport = &proxy.Transport{Type: "grpc", ServiceName: "service"}
			case "tcp":
			default:
				mode := transport
				if mode == "h2" || mode == "tls" {
					mode = "auto"
				}
				p.Transport = &proxy.Transport{Type: "xhttp", XHTTP: &proxy.XHTTPOptions{Mode: mode}}
				if transport == "h2" {
					p.TLS.ALPN = []string{"h2"}
				}
				if transport == "tls" {
					p.Security = "tls"
					p.Reality = nil
				}
			}
			out, err := (clash.Meta{}).Render(format.Profile{Proxies: []proxy.Proxy{p}})
			if err != nil || len(out.Warnings) != 0 {
				t.Fatalf("%v %+v", err, out.Warnings)
			}
			var doc struct {
				Proxies []map[string]any `yaml:"proxies"`
			}
			if err := yaml.Unmarshal(out.Body, &doc); err != nil || len(doc.Proxies) != 1 {
				t.Fatalf("%v %s", err, out.Body)
			}
			v := doc.Proxies[0]
			if transport != "vision" && v["flow"] != nil {
				t.Fatal(v)
			}
			if transport != "h2" && v["alpn"] != nil {
				t.Fatal(v)
			}
			if transport == "h2" && v["alpn"].([]any)[0] != "h2" {
				t.Fatal(v)
			}
			if p.Reality != nil && v["reality-opts"] == nil {
				t.Fatal(v)
			}
			if transport == "grpc" && (v["network"] != "grpc" || v["grpc-opts"].(map[string]any)["grpc-service-name"] != "service") {
				t.Fatal(v)
			}
			if p.Transport != nil && p.Transport.Type == "xhttp" && (v["network"] != "xhttp" || v["xhttp-opts"].(map[string]any)["mode"] != p.Transport.XHTTP.Mode) {
				t.Fatal(v)
			}
			if strings.Contains(string(out.Body), "spx") || strings.Contains(string(out.Body), spx) {
				t.Fatal(string(out.Body))
			}
			if p.Reality != nil {
				link, err := uri.Link(p)
				if err != nil || !strings.Contains(link, "spx=") {
					t.Fatal(link, err)
				}
			}
			if transport == "vision" || transport == "grpc" || transport == "auto" {
				sb, err := (singbox.Exporter{}).Render(format.Profile{Proxies: []proxy.Proxy{p}})
				if err != nil {
					t.Fatal(err)
				}
				if transport == "vision" {
					if len(sb.Warnings) != 0 || !strings.Contains(string(sb.Body), realityKey) || strings.Contains(string(sb.Body), spx) {
						t.Fatal(string(sb.Body), sb.Warnings)
					}
				} else {
					if len(sb.Warnings) == 0 || !strings.Contains(sb.Warnings[0].Message, p.Name) {
						t.Fatal(sb.Warnings)
					}
					for _, secret := range []string{realityUUID, realityKey, *p.Reality.ShortID} {
						if secret != "" && strings.Contains(sb.Warnings[0].Message, secret) {
							t.Fatal(sb.Warnings)
						}
					}
				}
			}
		})
	}
}
