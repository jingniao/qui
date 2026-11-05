package importer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
)

func ssFixtures(t *testing.T) ([]byte, []proxy.Proxy) {
	t.Helper()
	raw, err := os.ReadFile("testdata/ss2022.uri")
	if err != nil {
		t.Fatal(err)
	}
	r, p := Analyze(raw)
	if r.Outcome != "ok" || len(p) != 3 {
		t.Fatalf("three representative entries: %+v", r)
	}
	return raw, p
}

func TestSS2022RepresentativeRoundTripsAndEncryptedPersistence(t *testing.T) {
	raw, entries := ssFixtures(t)
	for _, exporter := range []format.Exporter{uri.URI{}, uri.Base64{}} {
		out, err := exporter.Render(format.Profile{Proxies: entries})
		if err != nil || len(out.Warnings) != 0 {
			t.Fatalf("%s: %v %+v", exporter.ID(), err, out.Warnings)
		}
		if exporter.ID() == "uri" && !bytes.Equal(bytes.TrimSpace(out.Body), bytes.TrimSpace(raw)) {
			t.Fatal("noncanonical SS2022 output")
		}
		_, got := Analyze(out.Body)
		before, _ := json.Marshal(entries)
		after, _ := json.Marshal(got)
		if !bytes.Equal(before, after) {
			t.Fatal("URI round trip lost credential semantics")
		}
	}
	s, userID, otherID := storeFixture(t)
	id, r, err := s.Save(context.Background(), "", Input{Name: "synthetic SS2022", Content: string(raw), Interval: 60})
	if err != nil || r.Outcome != "ok" {
		t.Fatalf("save: %v %+v", err, r)
	}
	grantImport(t, s, userID, id, "")
	viewer := source.Viewer{UserID: userID, AllowedProtocols: []string{"shadowsocks"}}
	got, _, err := s.Proxies(context.Background(), viewer)
	if err != nil || len(got) != 3 {
		t.Fatalf("read encrypted cache: %v count=%d", err, len(got))
	}
	for _, p := range got {
		matched := false
		for _, want := range entries {
			if p.Name == want.Name && p.Credential == want.Credential && p.Method == want.Method {
				matched = true
			}
		}
		if !matched {
			t.Fatal("encrypted persistence changed an SS2022 credential")
		}
	}
	rows, err := s.Database.Query("SELECT payload_encrypted FROM imported_proxies")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sealed []byte
		if err := rows.Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		for _, p := range entries {
			if bytes.Contains(sealed, []byte(p.Credential.Password)) {
				t.Fatal("plaintext credential in storage")
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	viewer.UserID = otherID
	got, _, err = s.Proxies(context.Background(), viewer)
	if err != nil || len(got) != 0 {
		t.Fatal("ungranted SS2022 endpoints exposed")
	}
}

func TestSS2022InputVariantsAndEscaping(t *testing.T) {
	_, entries := ssFixtures(t)
	for _, p := range entries {
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			line := "ss://" + encoding.EncodeToString([]byte(strings.ToUpper(p.Method)+":"+p.Credential.Password)) + "@[::1]:443#escaped%20%2B%2F%3A%E4%B8%AD"
			r, got := Analyze([]byte(line))
			if r.Outcome != "ok" || len(got) != 1 || got[0].Credential != p.Credential || got[0].Method != p.Method || got[0].Name != "escaped +/:中" {
				t.Fatalf("base64 input: %+v", r)
			}
			out, err := uri.Link(got[0])
			if err != nil || !strings.HasPrefix(out, "ss://2022-") || strings.Contains(out, "+") || !strings.Contains(out, "#escaped%20%2B%2F%3A%E4%B8%AD") {
				t.Fatal("SS2022 export used noncanonical userinfo/tag")
			}
		}
		// Tolerated raw standard-Base64, including '/', '+' and identity ':'.
		r, got := Analyze([]byte("ss://" + p.Method + ":" + p.Credential.Password + "@panel.invalid:443#plain"))
		if r.Outcome != "ok" || len(got) != 1 || got[0].Credential.Password != p.Credential.Password {
			t.Fatalf("plain input: %+v", r)
		}
	}
	// A whole-document Base64 wrapper is decoded by the panel, never emitted as
	// an SS userinfo password or treated as a separate client capability.
	raw, _ := os.ReadFile("testdata/ss2022.uri")
	r, got := Analyze([]byte(base64.StdEncoding.EncodeToString(raw)))
	if r.Outcome != "ok" || len(got) != 3 {
		t.Fatal("whole-document base64 compatibility")
	}
}

func TestSS2022RejectsUnrepresentedURIOptions(t *testing.T) {
	_, entries := ssFixtures(t)
	link, err := uri.Link(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	base := strings.SplitN(link, "#", 2)[0]
	for _, suffix := range []string{"?plugin=unsupported", "?type=ws&path=%2F", "?security=tls", "?flow=unknown", "/unmodeled"} {
		r, p := Analyze([]byte(base + suffix + "#unsupported"))
		if len(p) != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "unsupported_option" {
			t.Fatalf("unrepresented URI option survived: %+v", r)
		}
	}
	r, p := Analyze([]byte(base + "/#optional-slash"))
	if r.Outcome != "ok" || len(p) != 1 {
		t.Fatal("optional SIP002 slash rejected")
	}
}

func TestSS2022PreciseKeyDiagnosticsAcrossURIAndClash(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{255}, 32))
	for _, tc := range []struct{ name, method, password, code string }{
		{"unpadded", "2022-blake3-aes-256-gcm", strings.TrimRight(key, "="), "invalid_ss2022_key_encoding"},
		{"urlsafe", "2022-blake3-aes-256-gcm", base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{255}, 32)), "invalid_ss2022_key_encoding"},
		{"length", "2022-blake3-aes-256-gcm", base64.StdEncoding.EncodeToString(make([]byte, 16)), "invalid_ss2022_key_length"},
		{"empty identity segment", "2022-blake3-aes-256-gcm", key + ":", "invalid_ss2022_key_length"},
		{"identity segment encoding", "2022-blake3-aes-256-gcm", key + ":invalid", "invalid_ss2022_key_encoding"},
		{"identity segment length", "2022-blake3-aes-256-gcm", key + ":" + base64.StdEncoding.EncodeToString(make([]byte, 16)), "invalid_ss2022_key_length"},
		{"identity depth", "2022-blake3-aes-256-gcm", key + ":" + key + ":" + key, "unsupported_ss2022_identity"},
		{"chacha identity", "2022-blake3-chacha20-poly1305", key + ":" + key, "unsupported_ss2022_identity"},
		{"unknown cipher", "2022-unknown", key, "unsupported_cipher"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, input := range []string{
				"ss://" + base64.RawURLEncoding.EncodeToString([]byte(tc.method+":"+tc.password)) + "@panel.invalid:443#bad",
				"proxies:\n- {name: bad, type: ss, server: panel.invalid, port: 443, cipher: " + tc.method + ", password: '" + tc.password + "'}",
			} {
				r, p := Analyze([]byte(input))
				if len(p) != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.code {
					t.Fatalf("key diagnostic: %+v", r)
				}
				if strings.Contains(r.Diagnostics[0].Message, key) {
					t.Fatal("diagnostic exposed secret")
				}
			}
		})
	}
}

func TestSS2022ClashInputAndPartialSuccess(t *testing.T) {
	raw, entries := ssFixtures(t)
	out, err := (clash.Meta{}).Render(format.Profile{Proxies: entries})
	if err != nil {
		t.Fatal(err)
	}
	// The full output contains only exporter-supported fields; parsing its
	// proxies block must retain every PSK and its identity order.
	r, got := Analyze(out.Body)
	if r.Outcome != "ok" || len(got) != 3 {
		t.Fatalf("Clash SS2022 input: %+v", r)
	}
	r, got = Analyze(append(raw, []byte("ss://2022-blake3-aes-256-gcm:bad@panel.invalid:443#bad\n")...))
	if r.Outcome != "partial" || len(got) != 3 || r.Counts.Accepted != 3 || *r.Counts.Rejected != 1 || r.Diagnostics[0].Code != "invalid_ss2022_key_encoding" {
		t.Fatalf("partial success: %+v", r)
	}
	for _, exporter := range []format.Exporter{clash.Meta{}, singbox.Exporter{}} {
		out, err := exporter.Render(format.Profile{Proxies: got})
		if err != nil || len(out.Warnings) != 0 || !bytes.Contains(out.Body, []byte(entries[2].Credential.Password)) {
			t.Fatalf("%s dropped identity material: %v", exporter.ID(), err)
		}
	}
}
