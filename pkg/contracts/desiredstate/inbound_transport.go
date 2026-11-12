package desiredstate

import "fmt"

// ValidateInboundTransport reports a transport the protocol cannot run as an
// inbound. WebSocket and gRPC belong to VLESS and Trojan; xhttp to VLESS only
// (checked by the settings contract); HTTP, SOCKS5 and Shadowsocks are plain TCP.
// The Agent renderers either reject the combination (sing-box) or publish a
// listener the exported client link cannot describe (Xray).
//
// This is deliberately not part of Decode or State.Validate: stored revisions,
// rollbacks and backups still carry older combinations and must stay readable.
// The panel calls it on save, in SnapshotIntent and before publication, through
// proxy.ValidateInboundServerSettings.
func ValidateInboundTransport(protocol, transport string) error {
	if transport == "" || transport == "tcp" {
		return nil
	}
	var name string
	switch protocol {
	case "http":
		name = "HTTP"
	case "socks5":
		name = "SOCKS5"
	case "shadowsocks":
		name = "Shadowsocks"
	default:
		return nil
	}
	return fmt.Errorf("%s 入站只支持 TCP 传输（当前为 %s），请把传输改为 TCP", name, transport)
}
