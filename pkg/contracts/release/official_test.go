package release

import (
	"slices"
	"testing"
)

func equalKeyPins(a, b []KeyPin) bool {
	return slices.EqualFunc(a, b, func(x, y KeyPin) bool {
		return x.PublicKey == y.PublicKey && x.Revoked == y.Revoked && slices.Equal(x.Components, y.Components)
	})
}

func TestOfficialKeyPinsAreValid(t *testing.T) {
	keys, err := PinnedKeys(OfficialKeyPins)
	if err != nil {
		t.Fatalf("PinnedKeys(OfficialKeyPins): %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("no official release keys")
	}
	for _, pin := range OfficialKeyPins {
		if !slices.Equal(pin.Components, []string{"proxy-agent"}) {
			t.Fatalf("unexpected components: %v", pin.Components)
		}
	}
}

func TestEffectiveKeyPins(t *testing.T) {
	if got := EffectiveKeyPins(nil); !equalKeyPins(got, OfficialKeyPins) {
		t.Fatalf("nil config should return OfficialKeyPins, got %v", got)
	}
	if got := EffectiveKeyPins([]KeyPin{}); !equalKeyPins(got, OfficialKeyPins) {
		t.Fatalf("empty config should return OfficialKeyPins, got %v", got)
	}
	custom := []KeyPin{{PublicKey: "custom", Components: []string{"proxy-agent"}}}
	if got := EffectiveKeyPins(custom); !equalKeyPins(got, custom) {
		t.Fatalf("custom config should be kept, got %v", got)
	}
}
