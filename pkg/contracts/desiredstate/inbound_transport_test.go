package desiredstate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateInboundTransport(t *testing.T) {
	for _, protocol := range []string{"http", "socks5", "shadowsocks"} {
		for _, transport := range []string{"", "tcp"} {
			if err := ValidateInboundTransport(protocol, transport); err != nil {
				t.Fatalf("%s/%q refused: %v", protocol, transport, err)
			}
		}
		for _, transport := range []string{"ws", "grpc", "xhttp"} {
			err := ValidateInboundTransport(protocol, transport)
			if err == nil || !strings.Contains(err.Error(), "只支持 TCP") {
				t.Fatalf("%s/%s accepted: %v", protocol, transport, err)
			}
		}
	}
	for _, protocol := range []string{"vless", "trojan"} {
		for _, transport := range []string{"", "tcp", "ws", "grpc"} {
			if err := ValidateInboundTransport(protocol, transport); err != nil {
				t.Fatalf("%s/%q refused: %v", protocol, transport, err)
			}
		}
	}
	for _, protocol := range []string{"forward", "wireguard", "hysteria2", "tuic"} {
		if err := ValidateInboundTransport(protocol, "quic"); err != nil {
			t.Fatalf("%s changed: %v", protocol, err)
		}
	}
}

// The rule must not live in the decode path: stored revisions and backups still
// carry the combinations older panels accepted.
func TestDecodeStillReadsLegacyInvalidInboundTransport(t *testing.T) {
	state := valid()
	state.Inbounds[0].Protocol = "http"
	state.Inbounds[0].Settings = ProtocolSettings{Transport: "ws", Path: "/ws"}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err != nil {
		t.Fatalf("legacy http+ws revision no longer decodes: %v", err)
	}
	if err := ValidateInboundTransport("http", "ws"); err == nil {
		t.Fatal("the standalone rule must still refuse it")
	}
}
