package desiredstate

import "errors"

var ErrPortHopUnsupported = errors.New("port_hop_unsupported")

// RequirePortHopSupport refuses a state with an enabled Hysteria2 hop inbound
// when the node's Agent did not report the port-hop capability. An old Agent
// keeps publishing every other inbound unchanged.
func RequirePortHopSupport(state State, supported bool) error {
	if supported {
		return nil
	}
	for _, in := range state.Inbounds {
		if in.Enabled && in.Protocol == "hysteria2" && in.Settings.QUIC.HasHop() {
			return ErrPortHopUnsupported
		}
	}
	return nil
}
