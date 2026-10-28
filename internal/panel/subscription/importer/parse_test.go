package importer

import (
	"encoding/base64"
	"strings"
	"testing"
)

func FuzzParseURI(f *testing.F) {
	for _, seed := range []string{"socks5://sample:fixture@example.com:1080#one", "vless://fixture@example.com:443?security=tls#two", "ss://YWVzLTI1Ni1nY206Zml4dHVyZQ@example.com:443#three", "\x00", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > MaxBody {
			return
		}
		p, _ := (URI{}).Parse([]byte(s))
		for _, v := range p {
			if !valid(v) {
				t.Fatal("invalid parsed proxy")
			}
		}
		_, _ = (Base64{}).Parse([]byte(s))
	})
}
func FuzzParseClash(f *testing.F) {
	for _, seed := range []string{"proxies:\n- {name: one, type: socks5, server: example.com, port: 1080}", "proxies: [*undefined]", "proxies: []", "proxies:\n- {name: x, type: vmess, server: example.com, port: 443}"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > MaxBody {
			return
		}
		p, _ := (Clash{}).Parse([]byte(s))
		for _, v := range p {
			if !valid(v) {
				t.Fatal("invalid parsed proxy")
			}
		}
	})
}
func TestParsersDropUnsupportedAndInvalidEntries(t *testing.T) {
	uri := "socks5://fixture:fixture@example.com:1080#one\nvmess://unsupported\nvless://fixture@example.com:443?security=reality#bad"
	p, w, err := Parse([]byte(uri))
	if err != nil || len(p) != 1 || len(w) == 0 {
		t.Fatal("URI filtering")
	}
	b := base64.StdEncoding.EncodeToString([]byte(uri))
	p, _, err = Parse([]byte(b))
	if err != nil || len(p) != 1 {
		t.Fatal("Base64 parser")
	}
	p, w, err = Parse([]byte("proxies:\n- {name: one, type: socks5, server: example.com, port: 1080}\n- {name: bad, type: socks5, server: example.com, port: 1080, plugin: unexpected}\n- {name: invalid, type: http, server: example.com, port: 0}"))
	if err != nil || len(p) != 1 || len(w) == 0 {
		t.Fatal("Clash filtering")
	}
}

func TestParserRejectsCanonicalCacheExpansion(t *testing.T) {
	input := []byte("proxies:\n- {name: expansion, type: socks5, server: example.com, port: 1080, username: \"" + strings.Repeat("\\0", 400000) + "\"}")
	if len(input) > MaxBody {
		t.Fatal("fixture exceeds fetch cap")
	}
	if p, _, err := Parse(input); err == nil || len(p) != 0 {
		t.Fatal("oversized canonical encrypted cache accepted")
	}
}
