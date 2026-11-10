package desiredstate

import "errors"

var ErrPortHopUnsupported = errors.New("port_hop_unsupported")

// RequirePortHopSupport checks serialized hop fields, including disabled
// inbounds: old Agents reject unknown fields before checking enabled status.
func RequirePortHopSupport(state State, supported bool) error {
	if supported {
		return nil
	}
	for _, in := range state.Inbounds {
		if in.Settings.QUIC.HasHop() {
			return ErrPortHopUnsupported
		}
	}
	return nil
}
