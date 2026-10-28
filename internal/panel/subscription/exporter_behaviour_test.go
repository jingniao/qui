package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
)

// singBoxDocument is the part of the sing-box fragment these tests read.
type singBoxDocument struct {
	Outbounds []struct {
		Type      string           `json:"type"`
		Tag       string           `json:"tag"`
		Password  string           `json:"password"`
		Transport *json.RawMessage `json:"transport"`
		TLS       *struct {
			Certificate []string `json:"certificate"`
		} `json:"tls"`
	} `json:"outbounds"`
}

func TestQUICSubscriptionExportsUUIDPasswordTLSAndOptions(t *testing.T) {
	for _, protocol := range []string{"hysteria2", "tuic"} {
		endpoint := proxy.Proxy{
			Origin:     proxy.Origin{Source: "published", Ref: "entry", Node: proxy.Ref{ID: "node", Name: "Node"}},
			Name:       "Node · " + protocol,
			Server:     "2001:db8::1",
			Port:       443,
			Protocol:   protocol,
			Credential: proxy.Credential{Username: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Password: "secret-with+symbols"},
			Security:   "tls",
			TLS:        &proxy.TLS{ServerName: "proxy.example"},
			Transport:  &proxy.Transport{Type: "quic"},
			QUIC:       &proxy.QUIC{},
		}
		if protocol == "hysteria2" {
			endpoint.QUIC.UpMbps, endpoint.QUIC.DownMbps = 50, 100
		} else {
			endpoint.QUIC.CongestionControl, endpoint.QUIC.UDPRelayMode = "bbr", "quic"
		}
		link, err := uri.Link(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(link)
		if err != nil || parsed.Scheme != protocol || parsed.Host != "[2001:db8::1]:443" || parsed.Query().Get("sni") != "proxy.example" {
			t.Fatal("invalid QUIC URI", link, err)
		}
		if protocol == "tuic" {
			if password, ok := parsed.User.Password(); !ok || password != endpoint.Credential.Password || parsed.User.Username() != endpoint.Credential.Username {
				t.Fatal("TUIC identity/password lost")
			}
		} else if parsed.User.Username() != endpoint.Credential.Password {
			t.Fatal("Hysteria2 password lost")
		}

		rendered, err := singbox.Exporter{}.Render(format.Profile{Proxies: []proxy.Proxy{endpoint}})
		if err != nil {
			t.Fatal(err)
		}
		var document singBoxDocument
		if err := json.Unmarshal(rendered.Body, &document); err != nil || len(document.Outbounds) != 1 {
			t.Fatal(err)
		}
		if document.Outbounds[0].Transport != nil || document.Outbounds[0].TLS == nil || document.Outbounds[0].Password != endpoint.Credential.Password {
			t.Fatal("QUIC exported as legacy V2Ray transport")
		}

		encoded, err := uri.Base64{}.Render(format.Profile{Proxies: []proxy.Proxy{endpoint}})
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := base64.StdEncoding.DecodeString(string(encoded.Body))
		if err != nil || !strings.Contains(string(decoded), protocol+"://") {
			t.Fatal("base64 exporter omitted protocol", err)
		}
	}
}

// A URI list cannot carry the CA a Hysteria2 or TUIC endpoint pins, and one such
// endpoint used to fail the whole document: the subscriber answered 400 and lost
// every other link. The endpoint is now left out of the URI and Base64 formats,
// while the sing-box format keeps it together with its certificate.
func TestURIFormatsOmitQUICEndpointWithPinnedCA(t *testing.T) {
	pinned := func(inbound, protocol string) proxy.Proxy {
		return proxy.Proxy{Origin: proxy.Origin{Source: "published", Ref: inbound, Node: proxy.Ref{ID: "node", Name: "Node"}}, Name: "Node · " + protocol, Server: "node.example", Port: 8443, Protocol: protocol, Credential: proxy.Credential{Password: "pinned-secret"}, Security: "tls", TLS: &proxy.TLS{ServerName: "proxy.example", PinnedCA: fixtureCA}, Transport: &proxy.Transport{Type: "quic"}}
	}
	vless := proxy.Proxy{Origin: proxy.Origin{Source: "published", Ref: "entry", Node: proxy.Ref{ID: "node", Name: "Node"}}, Name: "Node · vless", Server: "node.example", Port: 443, Protocol: "vless", Credential: proxy.Credential{Password: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}}
	plain := proxy.Proxy{Origin: proxy.Origin{Source: "published", Ref: "hysteria", Node: proxy.Ref{ID: "node", Name: "Node"}}, Name: "Node · hysteria2", Server: "node.example", Port: 8445, Protocol: "hysteria2", Credential: proxy.Credential{Password: "plain-secret"}, Security: "tls", TLS: &proxy.TLS{ServerName: "proxy.example"}, Transport: &proxy.Transport{Type: "quic"}}
	hysteriaCA, tuicCA := pinned("hysteria-ca", "hysteria2"), pinned("tuic-ca", "tuic")
	for _, endpoint := range []proxy.Proxy{hysteriaCA, tuicCA} {
		if _, err := uri.Link(endpoint); !errors.Is(err, uri.ErrUnrepresentable) {
			t.Fatalf("%s with a pinned CA must be reported as unrepresentable: %v", endpoint.Protocol, err)
		}
	}
	proxies := []proxy.Proxy{vless, hysteriaCA, tuicCA, plain}
	list, err := uri.URI{}.Render(format.Profile{Proxies: proxies})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(list.Body)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "vless://") || !strings.HasPrefix(lines[1], "hysteria2://") {
		t.Fatalf("URI export must keep the endpoints a URI can carry: %q", list.Body)
	}
	// The omission is reported to the admin preview instead of vanishing.
	if len(list.Warnings) != 2 || list.Warnings[0].Stage != "format" || list.Warnings[0].Ref != "uri" {
		t.Fatalf("omitted endpoints produced no warning: %+v", list.Warnings)
	}
	encoded, err := uri.Base64{}.Render(format.Profile{Proxies: proxies})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(encoded.Body))
	if err != nil || string(decoded) != string(list.Body) {
		t.Fatalf("base64 export must carry the same list as the URI export: %q %v", decoded, err)
	}
	// A subscription that only has pinned-CA endpoints is empty rather than
	// failed, so the client shows "no endpoints" instead of an error.
	empty, err := uri.URI{}.Render(format.Profile{Proxies: []proxy.Proxy{hysteriaCA, tuicCA}})
	if err != nil || len(empty.Body) != 0 {
		t.Fatalf("a URI export of only pinned-CA endpoints must be empty: %q %v", empty.Body, err)
	}
	document, err := singbox.Exporter{}.Render(format.Profile{Proxies: proxies})
	if err != nil {
		t.Fatal(err)
	}
	var exported singBoxDocument
	if err := json.Unmarshal(document.Body, &exported); err != nil {
		t.Fatal(err)
	}
	certificates := map[string][]string{}
	for _, outbound := range exported.Outbounds {
		if outbound.TLS != nil {
			certificates[outbound.Tag] = outbound.TLS.Certificate
			continue
		}
		certificates[outbound.Tag] = nil
	}
	if len(certificates) != len(proxies) {
		t.Fatalf("sing-box export must keep every endpoint, got %d", len(certificates))
	}
	for _, endpoint := range []proxy.Proxy{hysteriaCA, tuicCA} {
		tag := endpoint.Origin.Node.Name + "-" + endpoint.Origin.Ref + "-" + endpoint.Protocol
		if strings.Join(certificates[tag], "\n") != fixtureCA {
			t.Fatalf("sing-box outbound %s must keep the pinned CA", tag)
		}
	}
	plainTag := plain.Origin.Node.Name + "-" + plain.Origin.Ref + "-" + plain.Protocol
	if certificate, known := certificates[plainTag]; !known || len(certificate) != 0 {
		t.Fatalf("sing-box outbound %s must not grow a certificate", plainTag)
	}
}
