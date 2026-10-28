package proxy

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"testing"
)

// keyPair builds a real X25519 identity so the checks below exercise the same
// path a published credential takes.
func keyPair(t *testing.T, seed byte) (private, public string) {
	t.Helper()
	raw := make([]byte, 32)
	for index := range raw {
		raw[index] = seed + byte(index)
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw), base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
}

func wireguardFixture(t *testing.T) Proxy {
	t.Helper()
	private, public := keyPair(t, 1)
	_, server := keyPair(t, 101)
	return Proxy{
		Protocol:   "wireguard",
		Credential: Credential{Password: private, PublicKey: public},
		WireGuard:  &WireGuard{ServerPublicKey: server, Addresses: []string{"10.99.0.2/32", "2001:db8:99::2/128"}, MTU: 1420},
	}
}

func TestWireGuardAddressesValidatesThePublishedIdentity(t *testing.T) {
	valid := wireguardFixture(t)
	addresses, err := valid.WireGuardAddresses()
	if err != nil || len(addresses) != 2 || addresses[0] != "10.99.0.2/32" {
		t.Fatalf("valid peer: %v %v", addresses, err)
	}

	_, otherPublic := keyPair(t, 55)
	private, _ := keyPair(t, 1)
	for _, testCase := range []struct {
		name   string
		mutate func(*Proxy)
		want   error
	}{
		{"public key of another private key", func(p *Proxy) { p.Credential.PublicKey = otherPublic }, ErrWireGuardKeyPair},
		{"non-canonical private key", func(p *Proxy) { p.Credential.Password = private + "=" }, ErrWireGuardKeyPair},
		{"missing private key", func(p *Proxy) { p.Credential.Password = "" }, ErrWireGuardKeyPair},
		{"server key is not 32 bytes", func(p *Proxy) { p.WireGuard.ServerPublicKey = base64.StdEncoding.EncodeToString([]byte("short")) }, ErrWireGuardKeyPair},
		{"no peer material", func(p *Proxy) { p.WireGuard = nil }, ErrWireGuardPeer},
		{"no address", func(p *Proxy) { p.WireGuard.Addresses = nil }, ErrWireGuardPeer},
		{"three addresses", func(p *Proxy) {
			p.WireGuard.Addresses = []string{"10.99.0.2/32", "2001:db8:99::2/128", "10.99.0.3/32"}
		}, ErrWireGuardPeer},
		{"two addresses of one family", func(p *Proxy) { p.WireGuard.Addresses = []string{"10.99.0.2/32", "10.99.0.3/32"} }, ErrWireGuardAddress},
		{"a subnet instead of a host", func(p *Proxy) { p.WireGuard.Addresses = []string{"10.99.0.0/24"} }, ErrWireGuardAddress},
		{"loopback", func(p *Proxy) { p.WireGuard.Addresses = []string{"127.0.0.1/32"} }, ErrWireGuardAddress},
		{"unspecified", func(p *Proxy) { p.WireGuard.Addresses = []string{"0.0.0.0/32"} }, ErrWireGuardAddress},
		{"IPv4-mapped", func(p *Proxy) { p.WireGuard.Addresses = []string{"::ffff:10.99.0.2/128"} }, ErrWireGuardAddress},
		{"garbage", func(p *Proxy) { p.WireGuard.Addresses = []string{"not-an-address"} }, ErrWireGuardAddress},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			endpoint := wireguardFixture(t)
			testCase.mutate(&endpoint)
			if _, err := endpoint.WireGuardAddresses(); !errors.Is(err, testCase.want) {
				t.Fatalf("error %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestOriginLabels(t *testing.T) {
	origin := Origin{Labels: []string{"nodeGroup:japan", "nodeGroup:asia"}}
	if !origin.HasLabel("nodeGroup:asia") {
		t.Fatal("label lookup failed")
	}
	if origin.HasLabel("nodeGroup:europe") {
		t.Fatal("unknown label reported as present")
	}
	if (Origin{}).HasLabel("nodeGroup:asia") {
		t.Fatal("an origin without labels matched")
	}
}
