package subscription

import (
	"encoding/base64"
	"encoding/json"
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
