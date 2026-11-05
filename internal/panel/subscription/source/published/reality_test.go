package published

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
)

func TestPublishedRealityAndRelayExposeOnlyMatchingPublicSettings(t *testing.T) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private := base64.RawURLEncoding.EncodeToString(key.Bytes())
	public := base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	in := desiredstate.Inbound{ID: "target", Protocol: "vless", Port: 24443, Settings: desiredstate.ProtocolSettings{Security: "reality", Transport: "xhttp", Reality: &desiredstate.RealityServerSettings{PrivateKey: private, PublicKey: public, HandshakeTarget: "127.0.0.1:443", ServerNames: []string{"target.panel.invalid"}, ShortIDs: []string{"abcd"}}, XHTTP: &desiredstate.XHTTPSettings{Host: "host.panel.invalid", Path: "/path", Mode: "auto"}}}
	endpoint, ok, err := build(node.Node{ID: "target-node", Name: "target", PublicIP: "target.panel.invalid"}, in, user.Credential{Secret: "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"}, nil)
	if err != nil || !ok {
		t.Fatal("published mapping failed", err)
	}
	if endpoint.Reality == nil || endpoint.Reality.PublicKey != public || *endpoint.Reality.ShortID != "abcd" || endpoint.TLS.ClientFingerprint != "chrome" || endpoint.Transport.XHTTP.Host != "host.panel.invalid" {
		t.Fatal("client options changed")
	}
	encoded, _ := json.Marshal(endpoint)
	if strings.Contains(string(encoded), private) || strings.Contains(string(encoded), "HandshakeTarget") || strings.Contains(string(encoded), "PrivateKey") {
		t.Fatal("private server material reached subscription model")
	}
	relay := Relay{ID: "relay", Entry: proxy.Ref{ID: "entry", Name: "entry"}, Server: "entry.panel.invalid", Port: 24444, Network: "tcp", DestinationAddress: "target.panel.invalid"}
	relayed, ok := RelayEndpoint(endpoint, relay)
	if !ok || relayed.Server != "entry.panel.invalid" || relayed.Port != 24444 || relayed.TLS.ServerName != "target.panel.invalid" || relayed.Reality.PublicKey != public || *relayed.Transport.XHTTP != *endpoint.Transport.XHTTP {
		t.Fatal("relay lost destination client settings")
	}
	relay.Network = "udp"
	if _, ok := RelayEndpoint(endpoint, relay); ok {
		t.Fatal("TCP xhttp selected a UDP-only relay")
	}
	if endpoint.Server != "target.panel.invalid" || endpoint.TLS.ServerName != "target.panel.invalid" {
		t.Fatal("relay mapping changed direct endpoint")
	}
}
