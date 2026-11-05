package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

// M1 stores and structurally validates the server-private xhttp/Reality settings
// but ships no renderer for them: the render entry must refuse such a state instead
// of producing a listener with the fields silently dropped.
func TestRenderRefusesServerSettingsPendingTheM2Renderer(t *testing.T) {
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
		{"reality vision", desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: reality, Flow: "xtls-rprx-vision"}},
		{"xhttp", desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP, XHTTP: xhttp}},
		{"reality xhttp", desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: reality, Transport: desiredstate.TransportXHTTP, XHTTP: xhttp}},
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
		if _, err := RenderAt(context.Background(), state, goldenAt); !errors.Is(err, desiredstate.ErrServerSettingsRendererPending) {
			t.Fatalf("%s: the renderer accepted settings it cannot apply: %v", test.name, err)
		}
	}
	if _, err := RenderAt(context.Background(), goldenScenarios()[0].state, goldenAt); err != nil {
		t.Fatalf("the unmodified scenario stopped rendering: %v", err)
	}
}
