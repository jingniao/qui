package backup

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

func backupReality(t *testing.T) desiredstate.ProtocolSettings {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return desiredstate.ProtocolSettings{Security: desiredstate.SecurityReality, Reality: &desiredstate.RealityServerSettings{
		PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		HandshakeTarget: "target.example:443", ServerNames: []string{"target.example"}, ShortIDs: []string{"abcd"},
	}}
}

func TestArchiveValidatesNewSettingsByProtocolAndDirection(t *testing.T) {
	t.Parallel()
	// Draft rows need validation even when no revision has ever included them.
	cases := []struct {
		name     string
		outbound bool
		protocol string
		mutate   func(*desiredstate.ProtocolSettings)
		valid    bool
	}{
		{name: "valid_reality", protocol: "vless", valid: true},
		{name: "mismatched_key", protocol: "vless", mutate: func(s *desiredstate.ProtocolSettings) {
			s.Reality.PublicKey = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
		}},
		{name: "missing_target", protocol: "vless", mutate: func(s *desiredstate.ProtocolSettings) { s.Reality.HandshakeTarget = "" }},
		{name: "wildcard_sni", protocol: "vless", mutate: func(s *desiredstate.ProtocolSettings) { s.Reality.ServerNames = []string{"*.example"} }},
		{name: "wrong_protocol", protocol: "trojan"},
		{name: "private_outbound", protocol: "vless", outbound: true},
		{name: "missing_reality_object", protocol: "vless", mutate: func(s *desiredstate.ProtocolSettings) { s.Reality = nil }},
		{name: "invalid_xhttp_mode", protocol: "vless", mutate: func(s *desiredstate.ProtocolSettings) {
			*s = desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Path: "/", Mode: "h3"}}
		}},
		{name: "missing_xhttp_object", protocol: "vless", mutate: func(s *desiredstate.ProtocolSettings) {
			*s = desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP}
		}},
		{name: "valid_xhttp", protocol: "vless", valid: true, mutate: func(s *desiredstate.ProtocolSettings) {
			*s = desiredstate.ProtocolSettings{Transport: desiredstate.TransportXHTTP, XHTTP: &desiredstate.XHTTPSettings{Path: "/assets", Mode: "auto"}}
		}},
		// An old draft with incomplete ordinary TLS material keeps its historical
		// structural-only backup semantics; this change does not make it deployable.
		{name: "legacy_draft", protocol: "vless", valid: true, mutate: func(s *desiredstate.ProtocolSettings) { *s = desiredstate.ProtocolSettings{Security: "tls"} }},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, directory, _ := sourceFixture(t)
			ctx := context.Background()
			settings := backupReality(t)
			if tc.mutate != nil {
				tc.mutate(&settings)
			}
			plaintext, err := json.Marshal(settings)
			if err != nil {
				t.Fatal(err)
			}
			sealed, err := source.ConfigurationVault.Seal(plaintext)
			if err != nil {
				t.Fatal(err)
			}
			var nodeID string
			if err := source.Database.QueryRow("SELECT id FROM nodes LIMIT 1").Scan(&nodeID); err != nil {
				t.Fatal(err)
			}
			table := "inbounds"
			query := "INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('settings','settings',?,?,'0.0.0.0',24443,0,0,0,'direct','[]',?,'2026-11-05T00:00:00Z')"
			if tc.outbound {
				table = "outbounds"
				query = "INSERT INTO outbounds(id,name,node_id,protocol,address,port,enabled,encrypted_settings,support_tcp,support_udp,failure_policy,created_at) VALUES('settings','settings',?,?,'target.example',443,0,?,1,0,'Block','2026-11-05T00:00:00Z')"
			}
			if _, err := source.Database.Exec(query, nodeID, tc.protocol, sealed); err != nil {
				t.Fatal(err)
			}
			id := fmt.Sprintf("settings_backup_%03d", index)
			archivePath := filepath.Join(directory, id+".pcb")
			if tc.valid {
				if _, err := source.Create(ctx, directory, id, "owner", testPassword); err != nil {
					t.Fatal(err)
				}
			} else {
				// Create itself verifies before publication. Construct an authenticated
				// external archive to exercise the receiving/restore verifier too.
				sources, err := source.snapshot(ctx, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				files := map[string][]byte{}
				for _, file := range sources {
					data, err := os.ReadFile(file.source)
					if err != nil {
						t.Fatal(err)
					}
					files[file.Name] = data
				}
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				repackFixture(t, archivePath, Manifest{Version: 1, ID: id, CreatedAt: time.Now().UTC(), CreatedBy: "owner"}, files, "")
			}
			if tc.valid {
				archiveBytes, err := os.ReadFile(archivePath)
				if err != nil {
					t.Fatal(err)
				}
				if settings.Reality != nil && bytes.Contains(archiveBytes, []byte(settings.Reality.PrivateKey)) {
					t.Fatal("private key escaped archive encryption")
				}
			}
			_, err = Examine(ctx, directory, archivePath, testPassword, func(_ Result, work string) error {
				copy, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
				if err != nil {
					return err
				}
				defer copy.Close()
				var encrypted []byte
				if err := copy.QueryRow("SELECT encrypted_settings FROM " + table + " WHERE id='settings'").Scan(&encrypted); err != nil {
					return err
				}
				decoded, err := source.ConfigurationVault.Open(encrypted)
				if err != nil {
					return err
				}
				if !bytes.Equal(decoded, plaintext) {
					t.Fatal("archive changed encrypted settings or private material")
				}
				return nil
			})
			if tc.valid && err != nil {
				t.Fatalf("valid settings archive refused: %v", err)
			}
			if !tc.valid && !errors.Is(err, ErrArchive) {
				t.Fatalf("invalid settings archive accepted: %v", err)
			}
		})
	}
}
