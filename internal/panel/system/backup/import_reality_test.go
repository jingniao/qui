package backup

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/proxy"
)

func TestRealityImportArchivePreservesPublicClientSettings(t *testing.T) {
	t.Parallel()
	s, directory, _ := sourceFixture(t)
	ctx := context.Background()
	const content = "vless://11111111-1111-4111-8111-111111111111@example.net:443?security=reality&sni=example.com&pbk=VddTg9EFbwIYD3ymir-1P0xUxP74eFM_9UmM1e7bEwY&sid=&fp=chrome&type=grpc&serviceName=s0%20grpc&spx=%2Fsearch#reality"
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	if _, _, err := store.Save(ctx, "", importer.Input{Name: "Reality archive", Content: content, Interval: 60}); err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err := s.Database.QueryRow("SELECT payload_encrypted FROM imported_proxies").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := s.ConfigurationVault.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	var want proxy.Proxy
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(ctx, directory, "reality_backup_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		database, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		var archived []byte
		if err = database.QueryRow("SELECT payload_encrypted FROM imported_proxies").Scan(&archived); err != nil {
			return err
		}
		plain, err := s.ConfigurationVault.Open(archived)
		if err != nil {
			return err
		}
		defer clear(plain)
		var got proxy.Proxy
		if err = json.Unmarshal(plain, &got); err != nil {
			return err
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatal("archive changed public settings")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A foreign/corrupted public model is refused instead of changing its meaning.
	want.Reality.PublicKey = "invalid"
	raw, _ = json.Marshal(want)
	sealed, err = s.ConfigurationVault.Seal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Database.Exec("UPDATE imported_proxies SET payload_encrypted=?", sealed); err != nil {
		t.Fatal(err)
	}
	if err = validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
		t.Fatal("archive accepted invalid Reality key")
	}
}

func TestXHTTPImportArchivePreservesHostAndEmptyPath(t *testing.T) {
	t.Parallel()
	s, directory, _ := sourceFixture(t)
	ctx := context.Background()
	const content = "vless://11111111-1111-4111-8111-111111111111@example.net:443?security=reality&sni=example.com&pbk=VddTg9EFbwIYD3ymir-1P0xUxP74eFM_9UmM1e7bEwY&sid=&fp=chrome&alpn=h2&type=xhttp&host=transport.example.com&path=&mode=auto#xhttp"
	store := &importer.Store{Database: s.Database, Vault: s.ConfigurationVault}
	if _, _, err := store.Save(ctx, "", importer.Input{Name: "xhttp archive", Content: content, Interval: 60}); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(ctx, directory, "xhttp_backup_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, created.Manifest.ID+".pcb"), testPassword, func(_ Result, work string) error {
		database, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		var archived []byte
		if err = database.QueryRow("SELECT payload_encrypted FROM imported_proxies").Scan(&archived); err != nil {
			return err
		}
		plain, err := s.ConfigurationVault.Open(archived)
		if err != nil {
			return err
		}
		defer clear(plain)
		var p proxy.Proxy
		if err = json.Unmarshal(plain, &p); err != nil {
			return err
		}
		if p.Transport == nil || p.Transport.XHTTP == nil || *p.Transport.XHTTP != (proxy.XHTTPOptions{Host: "transport.example.com", Path: "", Mode: "auto"}) || p.TLS.ServerName != "example.com" {
			t.Fatal("archive conflated transport host and SNI or dropped empty path")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var sealed []byte
	if err = s.Database.QueryRow("SELECT payload_encrypted FROM imported_proxies").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := s.ConfigurationVault.Open(sealed)
	if err != nil {
		t.Fatal(err)
	}
	var p proxy.Proxy
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	for _, alpn := range []string{"h3", "h3-29", "private-fixture-secret"} {
		p.TLS.ALPN = []string{alpn}
		raw, _ = json.Marshal(p)
		sealed, err = s.ConfigurationVault.Seal(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Database.Exec("UPDATE imported_proxies SET payload_encrypted=?", sealed); err != nil {
			t.Fatal(err)
		}
		if err = validateImports(ctx, s.Database, s.ConfigurationVault); !errors.Is(err, ErrArchive) {
			t.Fatal("archive accepted non-TCP xhttp ALPN")
		}
	}
}
