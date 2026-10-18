package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

// KeyPin is local operator configuration, never accepted from a release or
// control command. Keeping revoked pins prevents an old key becoming trusted
// accidentally when the active signing key is rotated.
type KeyPin struct {
	PublicKey  string   `json:"public_key" yaml:"public_key"`
	Components []string `json:"components" yaml:"components"`
	Revoked    bool     `json:"revoked" yaml:"revoked"`
}

func PinnedKeys(pins []KeyPin) (map[string]TrustedKey, error) {
	if len(pins) == 0 || len(pins) > 32 {
		return nil, errors.New("release trust requires between 1 and 32 pinned public keys")
	}
	result := make(map[string]TrustedKey, len(pins))
	for _, pin := range pins {
		public, err := base64.StdEncoding.DecodeString(pin.PublicKey)
		if err != nil || len(public) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(public) != pin.PublicKey || len(pin.Components) == 0 || len(pin.Components) > 3 {
			return nil, ErrSignature
		}
		id := KeyID(public)
		if _, exists := result[id]; exists {
			return nil, errors.New("duplicate release signing key")
		}
		seen := map[string]bool{}
		for _, component := range pin.Components {
			if !Component(component) || seen[component] {
				return nil, ErrPolicy
			}
			seen[component] = true
		}
		result[id] = TrustedKey{PublicKey: public, Components: append([]string(nil), pin.Components...), Revoked: pin.Revoked}
	}
	return result, nil
}
