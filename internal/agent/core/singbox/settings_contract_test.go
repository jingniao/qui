package singbox

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

// M4 owns the sing-box renderer; until it lands, the render entry refuses the
// server-private xhttp/Reality settings instead of dropping them. A pinned
// sing-box does not even have the xhttp transport (S0 evidence §4), so nothing
// here may pretend otherwise.
func TestRenderRefusesServerSettingsPendingTheM4Renderer(t *testing.T) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reality := &desiredstate.RealityServerSettings{
		PrivateKey:      base64.RawURLEncoding.EncodeToString(key.Bytes()),
		PublicKey:       base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		HandshakeTarget: "www.example.com:443",
		ServerNames:     []string{"www.example.com"},
		ShortIDs:        []string{"abcd"},
	}
	xhttp := &desiredstate.XHTTPSettings{Host: "cdn.example.com", Path: "/assets", Mode: desiredstate.XHTTPModeAuto}
	cases := []struct {
		name     string
		settings desiredstate.ProtocolSettings
	}{
		{"reality", desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: reality}},
		{"xhttp", desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP, XHTTP: xhttp}},
	}
	for _, test := range cases {
		state := goldenScenarios()[0].state
		state.Inbounds[0].Protocol = "vless"
		state.Credentials[0].Protocol = "vless"
		state.Credentials[0].Secret = "6f1a1d4e-2f4a-4a4f-9d6e-3a1c0b7e5d21"
		state.Inbounds[0].Settings = test.settings
		if err := state.Validate(); err != nil {
			t.Fatalf("%s: the typed settings are structurally invalid: %v", test.name, err)
		}
		if _, err := Render(context.Background(), state, goldenAt); !errors.Is(err, desiredstate.ErrServerSettingsRendererPending) {
			t.Fatalf("%s: the renderer accepted settings it cannot apply: %v", test.name, err)
		}
	}
	if _, err := Render(context.Background(), goldenScenarios()[0].state, goldenAt); err != nil {
		t.Fatalf("the unmodified scenario stopped rendering: %v", err)
	}
}
