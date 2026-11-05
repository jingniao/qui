package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

func TestRenderServerSettingsUsesTypedPrivateMaterial(t *testing.T) {
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
		{"reality grpc", desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: reality, Transport: "grpc", ServiceName: "s0 grpc"}},
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
		rendered, err := RenderAt(context.Background(), state, goldenAt)
		if err != nil {
			t.Fatalf("%s: render: %v", test.name, err)
		}
		var document configDocument
		if err := json.Unmarshal(rendered.Data, &document); err != nil {
			t.Fatal(err)
		}
		stream := document.Inbounds[0].Stream
		if stream == nil || stream.Reality == nil || stream.Reality.PrivateKey != reality.PrivateKey || stream.Reality.Target != reality.HandshakeTarget {
			t.Fatalf("%s: missing server material", test.name)
		}
		if test.settings.XHTTP != nil && (stream.XHTTP == nil || *stream.XHTTP != *xhttp) {
			t.Fatal("xhttp options changed")
		}
	}
	if _, err := RenderAt(context.Background(), goldenScenarios()[0].state, goldenAt); err != nil {
		t.Fatalf("the unmodified scenario stopped rendering: %v", err)
	}
}
