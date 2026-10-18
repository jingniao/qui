package user

import "errors"

var ErrTrafficIdentity = errors.New("traffic identity changed within one epoch")

// Application services receive fixed public failure categories, never need to
// inspect this module's tables or parse raw internal error strings.
func (r *Repository) TrafficFailureCode(err error) string {
	if errors.Is(err, ErrTrafficMaterial) {
		return "material_invalid"
	}
	if errors.Is(err, ErrTrafficIdentity) {
		return "identity_conflict"
	}
	return "accounting_pending"
}
