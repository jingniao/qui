package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestLocalPinsAreScopedAndRevocationSurvivesRotation(t *testing.T) {
	first, _, _ := ed25519.GenerateKey(rand.Reader)
	second, _, _ := ed25519.GenerateKey(rand.Reader)
	pin := KeyPin{PublicKey: base64.StdEncoding.EncodeToString(first), Components: []string{"proxy-agent"}, Revoked: true}
	next := KeyPin{PublicKey: base64.StdEncoding.EncodeToString(second), Components: []string{"xray", "sing-box"}}
	keys, err := PinnedKeys([]KeyPin{pin, next})
	if err != nil || len(keys) != 2 || !keys[KeyID(first)].Revoked || keys[KeyID(second)].Components[0] != "xray" {
		t.Fatal(keys, err)
	}
	for _, pins := range [][]KeyPin{nil, {pin, pin}, {{PublicKey: "invalid", Components: []string{"xray"}}}, {{PublicKey: pin.PublicKey, Components: nil}}, {{PublicKey: pin.PublicKey, Components: []string{"shell"}}}, {{PublicKey: pin.PublicKey, Components: []string{"xray", "xray"}}}} {
		if _, err := PinnedKeys(pins); err == nil {
			t.Fatal("invalid pins accepted")
		}
	}
}
