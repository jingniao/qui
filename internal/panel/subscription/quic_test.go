package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
	"testing"
)

func TestQUICSubscriptionExportsUUIDPasswordTLSAndOptions(t *testing.T) {
	for _, protocol := range []string{"hysteria2", "tuic"} {
		settings := desiredstate.ProtocolSettings{Security: "tls", Transport: "quic", ServerName: "proxy.example", QUIC: &desiredstate.QUICSettings{}}
		if protocol == "hysteria2" {
			settings.QUIC.UpMbps = 50
			settings.QUIC.DownMbps = 100
		} else {
			settings.QUIC.CongestionControl = "bbr"
			settings.QUIC.UDPRelayMode = "quic"
		}
		endpoint := Endpoint{NodeName: "Node", InboundID: "entry", Protocol: protocol, Address: "2001:db8::1", Port: 443, Credential: user.Credential{Username: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Secret: "secret-with+symbols"}, Settings: settings}
		uriValue, err := uri(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(uriValue)
		if err != nil || parsed.Scheme != protocol || parsed.Host != "[2001:db8::1]:443" || parsed.Query().Get("sni") != "proxy.example" {
			t.Fatal("invalid QUIC URI", uriValue, err)
		}
		if protocol == "tuic" {
			if password, ok := parsed.User.Password(); !ok || password != endpoint.Credential.Secret || parsed.User.Username() != endpoint.Credential.Username {
				t.Fatal("TUIC identity/password lost")
			}
		} else if parsed.User.Username() != endpoint.Credential.Secret {
			t.Fatal("Hysteria2 password lost")
		}
		data, err := Export([]Endpoint{endpoint}, "sing-box")
		if err != nil {
			t.Fatal(err)
		}
		var document struct{ Outbounds []singOutbound }
		if err := json.Unmarshal(data, &document); err != nil || len(document.Outbounds) != 1 {
			t.Fatal(err)
		}
		if document.Outbounds[0].Transport != nil || document.Outbounds[0].TLS == nil || document.Outbounds[0].Password != endpoint.Credential.Secret {
			t.Fatal("QUIC exported as legacy V2Ray transport")
		}
		encoded, err := Export([]Endpoint{endpoint}, "base64")
		if err != nil {
			t.Fatal(err)
		}
		decoded, _ := base64.StdEncoding.DecodeString(string(encoded))
		if !strings.Contains(string(decoded), protocol+"://") {
			t.Fatal("base64 exporter omitted protocol")
		}
	}
}

// A URI list cannot carry the CA a Hysteria2 or TUIC endpoint pins, and one such
// endpoint used to fail the whole document: the subscriber answered 400 and lost
// every other link. The endpoint is now left out of the URI and Base64 formats,
// while the sing-box format keeps it together with its certificate.
func TestURIFormatsOmitQUICEndpointWithPinnedCA(t *testing.T) {
	const ca = "-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIUQ2VydGlmaWNhdGU=\n-----END CERTIFICATE-----"
	quicSettings := desiredstate.ProtocolSettings{Security: "tls", Transport: "quic", ServerName: "proxy.example"}
	withCA := quicSettings
	withCA.TrustedCAPEM = ca
	vless := Endpoint{NodeName: "Node", InboundID: "entry", Protocol: "vless", Address: "node.example", Port: 443, Credential: user.Credential{Secret: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}, Settings: desiredstate.ProtocolSettings{}}
	hysteriaCA := Endpoint{NodeName: "Node", InboundID: "hysteria-ca", Protocol: "hysteria2", Address: "node.example", Port: 8443, Credential: user.Credential{Secret: "hysteria-secret"}, Settings: withCA}
	tuicCA := Endpoint{NodeName: "Node", InboundID: "tuic-ca", Protocol: "tuic", Address: "node.example", Port: 8444, Credential: user.Credential{Username: "6ebcdcf2-4dbe-4a89-9b40-a63ab69aacef", Secret: "tuic-secret"}, Settings: withCA}
	plain := Endpoint{NodeName: "Node", InboundID: "hysteria", Protocol: "hysteria2", Address: "node.example", Port: 8445, Credential: user.Credential{Secret: "plain-secret"}, Settings: quicSettings}
	for _, endpoint := range []Endpoint{hysteriaCA, tuicCA} {
		if _, err := uri(endpoint); !errors.Is(err, errURIUnrepresentable) {
			t.Fatalf("%s with a pinned CA must be reported as unrepresentable: %v", endpoint.Protocol, err)
		}
	}
	endpoints := []Endpoint{vless, hysteriaCA, tuicCA, plain}
	uris, err := Export(endpoints, "uri")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(uris)), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "vless://") || !strings.HasPrefix(lines[1], "hysteria2://") {
		t.Fatalf("URI export must keep the endpoints a URI can carry: %q", string(uris))
	}
	encoded, err := Export(endpoints, "base64")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil || string(decoded) != string(uris) {
		t.Fatalf("base64 export must carry the same list as the URI export: %q %v", string(decoded), err)
	}
	// A subscription that only has pinned-CA endpoints is empty rather than
	// failed, so the client shows "no endpoints" instead of an error.
	empty, err := Export([]Endpoint{hysteriaCA, tuicCA}, "uri")
	if err != nil || len(empty) != 0 {
		t.Fatalf("a URI export of only pinned-CA endpoints must be empty: %q %v", string(empty), err)
	}
	document, err := Export(endpoints, "sing-box")
	if err != nil {
		t.Fatal(err)
	}
	var exported struct{ Outbounds []singOutbound }
	if err := json.Unmarshal(document, &exported); err != nil {
		t.Fatal(err)
	}
	tls := map[string]*singTLS{}
	for _, outbound := range exported.Outbounds {
		tls[outbound.Tag] = outbound.TLS
	}
	if len(tls) != len(endpoints) {
		t.Fatalf("sing-box export must keep every endpoint, got %d", len(tls))
	}
	for _, endpoint := range []Endpoint{hysteriaCA, tuicCA} {
		tag := endpoint.NodeName + "-" + endpoint.InboundID + "-" + endpoint.Protocol
		if settings := tls[tag]; settings == nil || strings.Join(settings.Certificate, "\n") != ca {
			t.Fatalf("sing-box outbound %s must keep the pinned CA", tag)
		}
	}
	plainTag := plain.NodeName + "-" + plain.InboundID + "-" + plain.Protocol
	if settings := tls[plainTag]; settings == nil || len(settings.Certificate) != 0 {
		t.Fatalf("sing-box outbound %s must not grow a certificate", plainTag)
	}
}
