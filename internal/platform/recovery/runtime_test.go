package recovery

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/testutil/dbtest"
	"testing"
	"testing/fstest"
	"time"
)

func listeningFixture(t *testing.T, mutateSnapshot func(*fixture)) *fixture {
	t.Helper()
	f := recoveryFixture(t)
	f.source.Config.HTTPAddress = "127.0.0.1:0"
	f.source.Config.GRPCAddress = "127.0.0.1:0"
	if mutateSnapshot != nil {
		mutateSnapshot(f)
	}
	root := t.TempDir()
	created, err := f.source.Create(context.Background(), root, "runtime_snapshot_01", "owner", recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.prepared, err = archive.PrepareRestore(context.Background(), filepath.Join(t.TempDir(), "stages"), filepath.Join(root, created.Manifest.ID+".pcb"), recoveryPassword, archive.RestoreTarget{Panel: f.source.Config, ConfigurationPath: f.prepared.Plan.Target.ConfigurationPath})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestServicePreparationFailureRollsBackBeforeCommit(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"http-port", "grpc-port", "invalid-settings", "expired-server", "canceled-after-prepare"} {
		t.Run(fault, func(t *testing.T) {
			var blocker net.Listener
			var originalServer []byte
			f := listeningFixture(t, func(f *fixture) {
				if fault == "expired-server" {
					var err error
					path := filepath.Join(f.source.Config.NodeIdentityDirectory, "server.crt")
					originalServer, err = os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					pair, err := tls.LoadX509KeyPair(path, filepath.Join(f.source.Config.NodeIdentityDirectory, "server.key"))
					if err != nil {
						t.Fatal(err)
					}
					leaf, err := x509.ParseCertificate(pair.Certificate[0])
					if err != nil {
						t.Fatal(err)
					}
					leaf.NotBefore = time.Now().Add(-48 * time.Hour)
					leaf.NotAfter = time.Now().Add(-24 * time.Hour)
					authority, err := pki.LoadExisting(f.source.Config.NodeIdentityDirectory)
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := x509.CreateCertificate(rand.Reader, leaf, authority.Certificate, pair.PrivateKey.(crypto.Signer).Public(), authority.Key)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded}), 0600); err != nil {
						t.Fatal(err)
					}
					return
				}
				if fault == "invalid-settings" {
					if _, err := f.source.Database.Exec("UPDATE system_settings SET value_json='{}' WHERE key='platform_name'"); err != nil {
						t.Fatal(err)
					}
					return
				}
				if fault == "http-port" || fault == "grpc-port" {
					var err error
					blocker, err = net.Listen("tcp", "127.0.0.1:0")
					if err != nil {
						t.Fatal(err)
					}
					if fault == "http-port" {
						f.source.Config.HTTPAddress = blocker.Addr().String()
					} else {
						f.source.Config.GRPCAddress = blocker.Addr().String()
					}
				}
			})
			if blocker != nil {
				defer blocker.Close()
			}
			if fault == "expired-server" {
				if err := os.WriteFile(filepath.Join(f.source.Config.NodeIdentityDirectory, "server.crt"), originalServer, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "invalid-settings" {
				if _, err := f.source.Database.Exec("UPDATE system_settings SET value_json='\"Current valid platform\"' WHERE key='platform_name'"); err != nil {
					t.Fatal(err)
				}
			}
			f.record(t, "runtime-new-consumption", 200, 400)
			if _, err := f.source.Database.Exec("UPDATE users SET display_name='current-before-restore' WHERE id=?", f.user.ID); err != nil {
				t.Fatal(err)
			}
			candidate, err := PrepareDatabase(context.Background(), f.owner, f.source, f.prepared, recoveryPassword)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runtime, err := installAndPrepare(ctx, f.owner, f.source, candidate, f.prepared, recoveryPassword, fstest.MapFS{"index.html": {Data: []byte("UI")}}, nil, func(point string) {
				if fault == "canceled-after-prepare" && point == "validated" {
					cancel()
				}
			})
			if err == nil || runtime != nil {
				if runtime != nil {
					runtime.Close()
				}
				t.Fatal("failed service check committed restoration", fault, err)
			}
			if fault == "canceled-after-prepare" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			database, err := dbtest.Open(context.Background(), f.source.Config.Database)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			assertText(t, database, "SELECT display_name FROM users WHERE id=?", "current-before-restore", f.user.ID)
			assertInt(t, database, "SELECT used_traffic FROM users WHERE id=?", 600, f.user.ID)
			assertInt(t, database, "SELECT count(*) FROM sessions", 1)
			if _, err := os.Stat(filepath.Join(filepath.Dir(f.source.Config.Database), journalName)); !os.IsNotExist(err) {
				t.Fatal("failure left active recovery journal", err)
			}
		})
	}
}
