// Package clienttls holds the client TLS values shared by the neutral
// subscription model and the Agent desired state (v1.6.0): an imported endpoint
// and the outbound converted from it must accept exactly the same values.
package clienttls

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
)

// Fingerprints are the uTLS fingerprints both Xray and sing-box accept.
var Fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"}

// ValidFingerprint reports whether value is one of Fingerprints.
func ValidFingerprint(value string) bool {
	for _, fingerprint := range Fingerprints {
		if value == fingerprint {
			return true
		}
	}
	return false
}

// ValidRealityPublicKey accepts a canonical unpadded base64url X25519 public key
// that can complete a handshake. A syntactically valid low-order point (for
// example all zeroes) cannot, so it is refused as well.
func ValidRealityPublicKey(value string) error {
	key, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != value {
		return errors.New("invalid reality public key")
	}
	peer, err := ecdh.X25519().NewPublicKey(key)
	if err != nil {
		return errors.New("invalid reality public key")
	}
	private, err := ecdh.X25519().NewPrivateKey([]byte("01234567890123456789012345678901"))
	if err != nil {
		return err
	}
	if _, err = private.ECDH(peer); err != nil {
		return errors.New("invalid reality public key")
	}
	return nil
}
