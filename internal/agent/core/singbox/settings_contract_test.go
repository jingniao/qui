package singbox

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

func realityState(t *testing.T, flow string, port int, target string) desiredstate.State {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	state := stateFixture(t, "vless", port)
	state.Inbounds[0].Settings = desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Flow: flow, Reality: &desiredstate.RealityServerSettings{
		PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		HandshakeTarget: target, ServerNames: []string{"localhost"}, ShortIDs: []string{"abcd", "0123456789abcdef"},
	}}
	return state
}

func TestRealityRendererPreservesServerSemantics(t *testing.T) {
	for _, flow := range []string{"", "xtls-rprx-vision"} {
		state := realityState(t, flow, 24443, "[::1]:24444")
		before, _ := json.Marshal(state)
		rendered, err := Render(context.Background(), state, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var doc document
		if err := json.Unmarshal(rendered.Data, &doc); err != nil {
			t.Fatal(err)
		}
		in := doc.Inbounds[0]
		if in.TLS == nil || in.TLS.Reality == nil {
			t.Fatal("missing Reality TLS")
		}
		r := in.TLS.Reality
		if in.TLS.ServerName != "localhost" || r.Handshake.Server != "::1" || r.Handshake.Port != 24444 || r.PrivateKey != state.Inbounds[0].Settings.Reality.PrivateKey || strings.Join(r.ShortID, ",") != "abcd,0123456789abcdef" {
			t.Fatal("server fields were lost or transformed")
		}
		if len(in.TLS.Certificate) != 0 || len(in.TLS.Key) != 0 || in.Users[0].Flow != flow {
			t.Fatal("certificate branch or flow was corrupted")
		}
		if strings.Contains(string(rendered.Data), "public_key") || strings.Contains(string(rendered.Data), "handshake_target") {
			t.Fatal("client or internal contract fields reached native config")
		}
		after, _ := json.Marshal(state)
		if string(before) != string(after) {
			t.Fatal("render mutated historical state or keys")
		}
	}
}

func TestRealityRendererRefusesLossySettingsAndUndeclaredRuntime(t *testing.T) {
	state := realityState(t, "", 24443, "127.0.0.1:24444")
	for name, mutate := range map[string]func(*desiredstate.ProtocolSettings){
		"multiple SNI":    func(s *desiredstate.ProtocolSettings) { s.Reality.ServerNames = []string{"localhost", "other.invalid"} },
		"grpc":            func(s *desiredstate.ProtocolSettings) { s.Transport = "grpc" },
		"conflicting SNI": func(s *desiredstate.ProtocolSettings) { s.ServerName = "other.invalid" },
		"xhttp": func(s *desiredstate.ProtocolSettings) {
			s.Transport = desiredstate.TransportXHTTP
			s.XHTTP = &desiredstate.XHTTPSettings{Path: "/", Mode: "auto"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := realityState(t, "", 24443, "127.0.0.1:24444")
			mutate(&copy.Inbounds[0].Settings)
			if _, err := Render(context.Background(), copy, time.Now()); err == nil {
				t.Fatal("lossy/unsupported mapping accepted")
			}
		})
	}
	a := &Adapter{statsChecked: true}
	if _, err := a.Render(context.Background(), state); err == nil {
		t.Fatal("runtime without a native-probed contract rendered Reality")
	}
}

func TestRealityProbeReportsOnlyNativeValidatedCombinations(t *testing.T) {
	checks := 0
	combinations := probeRealityCombinations(context.Background(), func(_ context.Context, rendered core.RenderedConfig) error {
		checks++
		var doc document
		if err := json.Unmarshal(rendered.Data, &doc); err != nil {
			return err
		}
		if doc.Inbounds[0].TLS.Reality == nil {
			t.Fatal("probe bypassed production renderer")
		}
		if doc.Inbounds[0].Users[0].Flow != "" {
			return errors.New("native Vision unavailable")
		}
		return nil
	})
	if checks != 2 || len(combinations) != 1 || combinations[0].Flow != "" || combinations[0].Transport != "tcp" {
		t.Fatal("native result did not bound advertised combinations")
	}
	if got := probeRealityCombinations(context.Background(), func(context.Context, core.RenderedConfig) error { return errors.New("not supported") }); len(got) != 0 {
		t.Fatal("native failure authorized settings")
	}
}
