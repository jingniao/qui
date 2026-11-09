package published

import (
	"strings"
	"testing"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
)

// An inbound that presents a publicly trusted certificate (the acme source)
// carries no pinned CA, so a Hysteria2 or TUIC endpoint is an ordinary TLS link
// that a URI list can express. The published pair itself never reaches the model.
func TestPubliclyTrustedQUICEndpointExportsAsURI(t *testing.T) {
	for _, protocol := range []string{"hysteria2", "tuic"} {
		settings := desiredstate.ProtocolSettings{Security: "tls", ServerName: "edge.example.com", CertificatePEM: "published certificate", PrivateKeyPEM: "published key"}
		inbound := desiredstate.Inbound{ID: "quic", Protocol: protocol, Port: 8443, Settings: settings}
		endpoint, ok, err := build(node.Node{ID: "node", Name: "Edge", PublicIP: "203.0.113.5"}, inbound, user.Credential{Username: "alice", Secret: "5ebcdcf2-4dbe-4a89-9b40-a63ab69aacef"}, nil)
		if err != nil || !ok {
			t.Fatalf("%s: published mapping failed: %v", protocol, err)
		}
		if endpoint.TLS == nil || endpoint.TLS.PinnedCA != "" || endpoint.TLS.ServerName != "edge.example.com" {
			t.Fatalf("%s: a certificate from the library must not be pinned: %+v", protocol, endpoint.TLS)
		}
		link, err := uri.Link(endpoint)
		if err != nil {
			t.Fatalf("%s: a publicly trusted endpoint must be representable in a URI list: %v", protocol, err)
		}
		if !strings.HasPrefix(link, protocol+"://") || !strings.Contains(link, "edge.example.com") || strings.Contains(link, "published") {
			t.Fatalf("%s: link %q", protocol, link)
		}
	}
}
