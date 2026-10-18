package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func releaseFixture(t *testing.T) (Signed, Policy, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)
	m := Manifest{SchemaVersion: 1, Component: "proxy-agent", Version: "1.2.3", OS: "linux", Architecture: "amd64", Sequence: 7, Bytes: 4096, SHA256: strings.Repeat("a", 64), IssuedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	signed, err := Sign(m, private)
	if err != nil {
		t.Fatal(err)
	}
	return signed, Policy{Component: m.Component, OS: m.OS, Architecture: m.Architecture, MinimumSequence: 7, Now: now, Keys: map[string]TrustedKey{signed.KeyID: {PublicKey: public, Components: []string{m.Component}}}}, private
}
func TestSignedManifestTrustPlatformSequenceAndExpiry(t *testing.T) {
	signed, policy, _ := releaseFixture(t)
	if err := signed.Verify(policy); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Signed, *Policy){
		"tampered-digest": func(s *Signed, _ *Policy) { s.Manifest.SHA256 = strings.Repeat("b", 64) },
		"component":       func(_ *Signed, p *Policy) { p.Component = "xray" },
		"platform":        func(_ *Signed, p *Policy) { p.OS = "windows" },
		"architecture":    func(_ *Signed, p *Policy) { p.Architecture = "arm64" },
		"rollback":        func(_ *Signed, p *Policy) { p.MinimumSequence = 8 },
		"expiry":          func(_ *Signed, p *Policy) { p.Now = p.Now.Add(24 * time.Hour) },
		"future":          func(_ *Signed, p *Policy) { p.Now = p.Now.Add(-6 * time.Minute) },
		"revoked": func(s *Signed, p *Policy) {
			key := p.Keys[s.KeyID]
			key.Revoked = true
			p.Keys = map[string]TrustedKey{s.KeyID: key}
		},
		"key-purpose": func(s *Signed, p *Policy) {
			key := p.Keys[s.KeyID]
			key.Components = []string{"sing-box"}
			p.Keys = map[string]TrustedKey{s.KeyID: key}
		},
		"unknown-key": func(_ *Signed, p *Policy) { p.Keys = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s, p := signed, policy
			mutate(&s, &p)
			if err := s.Verify(p); err == nil {
				t.Fatal("release policy or signature bypass")
			}
		})
	}
}
func TestManifestStrictDecodeRejectsDuplicateKeysAndOperations(t *testing.T) {
	signed, _, private := releaseFixture(t)
	data, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(data), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(string(data), `"manifest":{`, `"manifest":{"script":"run" ,`, 1),
		strings.Replace(string(data), `"key_id":`, `"path":"../other", "key_id":`, 1),
		string(data) + `{}`,
	} {
		if _, err := Decode([]byte(bad)); err == nil {
			t.Fatal("ambiguous or untyped release accepted")
		}
	}
	m := signed.Manifest
	m.Component = "shell"
	if _, err := Sign(m, private); err == nil {
		t.Fatal("arbitrary component signed")
	}
	m = signed.Manifest
	m.Version = "../binary"
	if _, err := Sign(m, private); err == nil {
		t.Fatal("release version became path")
	}
	broken := append(ed25519.PrivateKey(nil), private...)
	broken[len(broken)-1] ^= 1
	if _, err := Sign(signed.Manifest, broken); err == nil {
		t.Fatal("malformed private key accepted")
	}
}
