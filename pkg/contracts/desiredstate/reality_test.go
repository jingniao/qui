package desiredstate

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func testTLSCertificate(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "desiredstate-test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
}

func testRealityKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(key.Bytes()), base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
}

func testReality(t *testing.T) *RealityServerSettings {
	t.Helper()
	private, public := testRealityKeys(t)
	return &RealityServerSettings{PrivateKey: private, PublicKey: public, HandshakeTarget: "www.example.com:443", ServerNames: []string{"www.example.com"}, ShortIDs: []string{"0123abcd"}}
}

func testXHTTP() *XHTTPSettings {
	return &XHTTPSettings{Host: "cdn.example.com", Path: "/assets", Mode: XHTTPModeAuto}
}

// The M1 contract accepts the self-hosted combinations the plan names — Reality
// over TCP (with and without Vision), xhttp over Reality, TLS and plain HTTP — and
// keeps every legacy combination unchanged.
func TestValidateSettingsAcceptsServerSettingsCombinations(t *testing.T) {
	certificate, privateKey := testTLSCertificate(t)
	valid := []struct {
		name     string
		inbound  bool
		settings ProtocolSettings
	}{
		{"reality tcp", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t)}},
		{"reality tcp vision", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t), Flow: "xtls-rprx-vision"}},
		{"reality xhttp", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t), Transport: TransportXHTTP, XHTTP: testXHTTP()}},
		{"tls xhttp", true, ProtocolSettings{Security: "tls", Transport: TransportXHTTP, XHTTP: testXHTTP(), CertificatePEM: certificate, PrivateKeyPEM: privateKey}},
		{"plain xhttp", true, ProtocolSettings{Transport: TransportXHTTP, XHTTP: testXHTTP()}},
		{"legacy vision outbound", false, ProtocolSettings{Security: "tls", Flow: "xtls-rprx-vision", ServerName: "site.example"}},
	}
	for _, test := range valid {
		if err := ValidateSettings("vless", test.settings, test.inbound); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

// A Reality key pair must be role-correct: the panel derives the public key from
// the private one, so a swapped, foreign or wrongly encoded key is refused rather
// than stored for the Agent to discover at apply time.
func TestRealitySettingsFieldBounds(t *testing.T) {
	private, public := testRealityKeys(t)
	_, otherPublic := testRealityKeys(t)
	padded := base64.StdEncoding.EncodeToString(make([]byte, RealityKeyBytes))
	tooManyNames := make([]string, MaxRealityServerNames+1)
	for index := range tooManyNames {
		tooManyNames[index] = "name" + strings.Repeat("a", index) + ".example"
	}
	tooManyIDs := make([]string, MaxRealityShortIDs+1)
	for index := range tooManyIDs {
		tooManyIDs[index] = "aa"
	}
	cases := []struct {
		name   string
		mutate func(*RealityServerSettings)
		want   string
	}{
		{"swapped keys", func(r *RealityServerSettings) { r.PrivateKey, r.PublicKey = public, private }, "public key does not match the private key"},
		{"foreign public key", func(r *RealityServerSettings) { r.PublicKey = otherPublic }, "public key does not match the private key"},
		{"padded base64 private key", func(r *RealityServerSettings) { r.PrivateKey = padded }, "invalid reality private key"},
		{"short private key", func(r *RealityServerSettings) { r.PrivateKey = "AAAA" }, "invalid reality private key"},
		{"missing public key", func(r *RealityServerSettings) { r.PublicKey = "" }, "invalid reality public key"},
		{"odd short id", func(r *RealityServerSettings) { r.ShortIDs = []string{"abc"} }, "invalid reality short id"},
		{"long short id", func(r *RealityServerSettings) { r.ShortIDs = []string{"0123456789abcdef00"} }, "invalid reality short id"},
		{"non hex short id", func(r *RealityServerSettings) { r.ShortIDs = []string{"zz"} }, "invalid reality short id"},
		{"empty short id", func(r *RealityServerSettings) { r.ShortIDs = []string{""} }, "invalid reality short id"},
		{"no short id", func(r *RealityServerSettings) { r.ShortIDs = nil }, "reality requires between one and sixteen short ids"},
		{"too many short ids", func(r *RealityServerSettings) { r.ShortIDs = tooManyIDs }, "reality requires between one and sixteen short ids"},
		{"no server name", func(r *RealityServerSettings) { r.ServerNames = nil }, "reality requires between one and sixteen server names"},
		{"empty server name", func(r *RealityServerSettings) { r.ServerNames = []string{""} }, "invalid reality server name"},
		{"wildcard server name", func(r *RealityServerSettings) { r.ServerNames = []string{"*.example.com"} }, "invalid reality server name"},
		{"server name with control character", func(r *RealityServerSettings) { r.ServerNames = []string{"site\n.example"} }, "invalid reality server name"},
		{"server name with space", func(r *RealityServerSettings) { r.ServerNames = []string{"site example"} }, "invalid reality server name"},
		{"too many server names", func(r *RealityServerSettings) { r.ServerNames = tooManyNames }, "reality requires between one and sixteen server names"},
		{"target without port", func(r *RealityServerSettings) { r.HandshakeTarget = "www.example.com" }, "invalid reality handshake target"},
		{"target with scheme", func(r *RealityServerSettings) { r.HandshakeTarget = "https://www.example.com:443" }, "invalid reality handshake target"},
		{"target with zero port", func(r *RealityServerSettings) { r.HandshakeTarget = "www.example.com:0" }, "invalid reality handshake target"},
		{"target with high port", func(r *RealityServerSettings) { r.HandshakeTarget = "www.example.com:70000" }, "invalid reality handshake target"},
		{"target with unspecified address", func(r *RealityServerSettings) { r.HandshakeTarget = "0.0.0.0:443" }, "invalid reality handshake target"},
		{"missing target", func(r *RealityServerSettings) { r.HandshakeTarget = "" }, "reality requires the handshake target"},
	}
	for _, test := range cases {
		reality := *testReality(t)
		test.mutate(&reality)
		err := ValidateSettings("vless", ProtocolSettings{Security: SecurityReality, Reality: &reality}, true)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
	// Concrete server names, an IPv6 target and a literal IPv4 target are legal.
	for _, mutate := range []func(*RealityServerSettings){
		func(r *RealityServerSettings) { r.ServerNames = []string{"www.example.com", "edge.example.com"} },
		func(r *RealityServerSettings) { r.HandshakeTarget = "[2001:db8::1]:443" },
		func(r *RealityServerSettings) { r.HandshakeTarget = "203.0.113.5:8443" },
		func(r *RealityServerSettings) { r.ShortIDs = []string{"a1B2"} },
	} {
		reality := *testReality(t)
		mutate(&reality)
		if err := ValidateSettings("vless", ProtocolSettings{Security: SecurityReality, Reality: &reality}, true); err != nil {
			t.Fatalf("legal reality spelling refused: %v", err)
		}
	}
}

// The server-private portion is inbound-only, typed and mutually exclusive with
// ordinary TLS material: the panel refuses a listener it could not describe.
func TestValidateSettingsRejectsInvalidServerSettings(t *testing.T) {
	invalid := []struct {
		name     string
		protocol string
		inbound  bool
		settings ProtocolSettings
		want     string
	}{
		{"reality without settings", "vless", true, ProtocolSettings{Security: SecurityReality}, "reality security requires"},
		{"xhttp without settings", "vless", true, ProtocolSettings{Transport: TransportXHTTP}, "xhttp transport requires"},
		{"xhttp settings without transport", "vless", true, ProtocolSettings{XHTTP: testXHTTP()}, "require the xhttp transport"},
		{"reality settings without security", "vless", true, ProtocolSettings{Reality: testReality(t)}, "require reality security"},
		{"reality security with xhttp but no reality settings", "vless", true, ProtocolSettings{Security: SecurityReality, Transport: TransportXHTTP, XHTTP: testXHTTP()}, "reality security requires"},
		{"reality settings with xhttp transport but no xhttp settings", "vless", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t), Transport: TransportXHTTP}, "xhttp transport requires"},
		{"reality with TLS material", "vless", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t), CertificatePEM: "pem"}, "cannot carry TLS certificate material"},
		{"reality with trusted CA", "vless", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t), TrustedCAPEM: "pem"}, "cannot carry TLS certificate material"},
		{"reality on trojan", "trojan", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t)}, "require a VLESS inbound"},
		{"reality on an outbound", "vless", false, ProtocolSettings{Security: SecurityReality, Reality: testReality(t)}, "require a VLESS inbound"},
		{"reality with shadowsocks method", "vless", true, ProtocolSettings{Security: SecurityReality, Reality: testReality(t), Method: "aes-256-gcm"}, "cannot carry other protocol options"},
		{"xhttp with websocket path", "vless", true, ProtocolSettings{Transport: TransportXHTTP, Path: "/ws", XHTTP: testXHTTP()}, "keeps its own path"},
		{"xhttp with gRPC service name", "vless", true, ProtocolSettings{Transport: TransportXHTTP, ServiceName: "svc", XHTTP: testXHTTP()}, "keeps its own path"},
		{"xhttp with http/3 mode", "vless", true, ProtocolSettings{Transport: TransportXHTTP, XHTTP: &XHTTPSettings{Path: "/", Mode: "h3"}}, "unsupported xhttp mode"},
		{"xhttp with empty mode", "vless", true, ProtocolSettings{Transport: TransportXHTTP, XHTTP: &XHTTPSettings{Path: "/"}}, "unsupported xhttp mode"},
		{"xhttp unrooted path", "vless", true, ProtocolSettings{Transport: TransportXHTTP, XHTTP: &XHTTPSettings{Path: "assets", Mode: XHTTPModeAuto}}, "must begin with slash"},
		{"xhttp host with scheme", "vless", true, ProtocolSettings{Transport: TransportXHTTP, XHTTP: &XHTTPSettings{Host: "https://cdn.example.com", Path: "/", Mode: XHTTPModeAuto}}, "invalid xhttp host"},
		{"xhttp host with port", "vless", true, ProtocolSettings{Transport: TransportXHTTP, XHTTP: &XHTTPSettings{Host: "cdn.example.com:8443", Path: "/", Mode: XHTTPModeAuto}}, "invalid xhttp host"},
		{"vision over xhttp", "vless", true, ProtocolSettings{Security: "tls", Flow: "xtls-rprx-vision", Transport: TransportXHTTP, XHTTP: testXHTTP()}, "TCP with TLS or Reality"},
		{"vision over grpc", "vless", true, ProtocolSettings{Security: "tls", Flow: "xtls-rprx-vision", Transport: "grpc"}, "TCP with TLS or Reality"},
		{"vision without TLS", "vless", true, ProtocolSettings{Flow: "xtls-rprx-vision"}, "TCP with TLS or Reality"},
		{"vision on trojan", "trojan", true, ProtocolSettings{Security: "tls", Flow: "xtls-rprx-vision"}, "TCP with TLS or Reality"},
	}
	for _, test := range invalid {
		if err := ValidateSettings(test.protocol, test.settings, test.inbound); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}
