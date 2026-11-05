package proxy

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestRealityKeyAndPresenceValidation(t *testing.T) {
	sid := ""
	p := Proxy{Protocol: "vless", Credential: Credential{Password: "11111111-1111-4111-8111-111111111111"}, Security: "reality", TLS: &TLS{ServerName: "example.com", ClientFingerprint: "chrome"}, Reality: &RealityOptions{PublicKey: "VddTg9EFbwIYD3ymir-1P0xUxP74eFM_9UmM1e7bEwY", ShortID: &sid}}
	if code, _ := ValidateClientSettings(p); code != "" {
		t.Fatal(code)
	}
	raw, _ := json.Marshal(p)
	var again Proxy
	if err := json.Unmarshal(raw, &again); err != nil || again.Reality.ShortID == nil {
		t.Fatal("explicit empty short ID not retained")
	}
	for _, key := range []string{"", "private", base64.RawURLEncoding.EncodeToString(make([]byte, 32)), p.Reality.PublicKey + "=", p.Reality.PublicKey[:42]} {
		q := p
		r := *p.Reality
		q.Reality = &r
		q.Reality.PublicKey = key
		if code, _ := ValidateClientSettings(q); code != "invalid_reality_key" {
			t.Fatalf("bad key accepted: %s", code)
		}
	}
	p.Reality.ShortID = nil
	if code, _ := ValidateClientSettings(p); code != "invalid_reality_short_id" {
		t.Fatal("omitted SID accepted")
	}
}

func TestClientFingerprintAndCertificatePinAreDistinct(t *testing.T) {
	p := Proxy{Protocol: "vless", Security: "tls", TLS: &TLS{ClientFingerprint: "chrome", Fingerprint: strings.Repeat("ab", 32)}}
	if code, _ := ValidateClientSettings(p); code != "" {
		t.Fatal(code)
	}
	p.TLS.ClientFingerprint = p.TLS.Fingerprint
	if code, _ := ValidateClientSettings(p); code != "invalid_fingerprint" {
		t.Fatal("certificate hash accepted as client hello")
	}
	p.TLS.ClientFingerprint = "chrome"
	p.TLS.Fingerprint = "chrome"
	if code, field := ValidateClientSettings(p); code != "invalid_fingerprint" || field != "tls.certificateFingerprint" {
		t.Fatal("ClientHello accepted as certificate hash")
	}
}

func TestEmptyNewSettingsDoNotChangeExistingCacheIdentity(t *testing.T) {
	p := Proxy{Protocol: "vless", TLS: &TLS{}, Transport: &Transport{Type: "grpc"}}
	raw, _ := json.Marshal(p)
	for _, field := range []string{"ClientFingerprint", "Reality", "XHTTP"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Fatalf("new absent setting changes old payload identity: %s", field)
		}
	}
}
