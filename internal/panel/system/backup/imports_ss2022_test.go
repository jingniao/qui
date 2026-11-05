package backup

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/proxy"
)

func TestImportBackupSS2022PreservesAndRejectsCorruptSettings(t *testing.T) {
	s, directory, _ := sourceFixture(t)
	ctx := context.Background()
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	const key = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	const userKey = "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="
	content := "proxies:\n- {name: identity, type: ss, server: panel.invalid, port: 443, cipher: 2022-blake3-aes-256-gcm, password: '" + key + ":" + userKey + "'}\n- {name: bad, type: ss, server: panel.invalid, port: 443, cipher: 2022-blake3-aes-256-gcm, password: 'bad'}"
	_, result, err := store.Save(ctx, "", importer.Input{Name: "SS2022 fixture", Content: content, Interval: 60})
	if err != nil || result.Outcome != "partial" || result.Diagnostics[0].Code != "invalid_ss2022_key_encoding" {
		t.Fatalf("fixture: %+v %v", result, err)
	}
	if err := validateImports(ctx, s.Database, s.ConfigurationVault); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(ctx, directory, "imports_ss2022_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Inspect(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword); err != nil {
		t.Fatalf("valid identity archive rejected: %v", err)
	}
	var sealed []byte
	if err := s.Database.QueryRow("SELECT payload_encrypted FROM imported_proxies").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := s.ConfigurationVault.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	var original proxy.Proxy
	if json.Unmarshal(raw, &original) != nil || original.Credential.Password != key+":"+userKey {
		t.Fatal("archive source lost identity order")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*proxy.Proxy)
	}{
		{"unpadded", func(p *proxy.Proxy) { p.Credential.Password = strings.TrimRight(key, "=") }},
		{"wrong segment length", func(p *proxy.Proxy) { p.Credential.Password = key + ":AAECAwQFBgcICQoLDA0ODw==" }},
		{"unsupported identity depth", func(p *proxy.Proxy) { p.Credential.Password = key + ":" + userKey + ":" + key }},
		{"unsupported chacha identity", func(p *proxy.Proxy) { p.Method = "2022-blake3-chacha20-poly1305" }},
		{"unknown cipher", func(p *proxy.Proxy) { p.Method = "2022-unknown" }},
		{"unrepresentable transport", func(p *proxy.Proxy) { p.Transport = &proxy.Transport{Type: "ws", Path: "/"} }},
		{"unsupported TLS", func(p *proxy.Proxy) { p.Security = "tls"; p.TLS = &proxy.TLS{ServerName: "panel.invalid"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := original
			tc.mutate(&p)
			payload, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := s.ConfigurationVault.Seal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Database.Exec("UPDATE imported_proxies SET payload_encrypted=?", sealed); err != nil {
				t.Fatal(err)
			}
			if err := validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
				t.Fatalf("corrupt SS2022 accepted: %v", err)
			}
		})
	}
	for _, payload := range []string{
		strings.TrimSuffix(string(raw), "}") + `,"UnmodeledOption":"hidden"}`,
		strings.Replace(string(raw), `"Password":`, `"UnmodeledOption":0,"Password":`, 1),
		string(raw) + ` {}`,
	} {
		sealed, err := s.ConfigurationVault.Seal([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Database.Exec("UPDATE imported_proxies SET payload_encrypted=?", sealed); err != nil {
			t.Fatal(err)
		}
		if err := validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
			t.Fatalf("unknown/trailing payload accepted: %v", err)
		}
	}
}
