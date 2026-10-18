package recovery

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/server"
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
			database, err := db.Open(context.Background(), f.source.Config.Database)
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

func TestCommittedReservationServesAndRealAgentRejoinsPreservedIdentity(t *testing.T) {
	var firstGRPC net.Listener
	f := listeningFixture(t, func(f *fixture) {
		var err error
		firstGRPC, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		f.source.Config.GRPCAddress = firstGRPC.Addr().String()
	})
	ctx := context.Background()
	f.record(t, "newer-runtime-count", 200, 300)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	agentConfig := config.DefaultAgent()
	agentConfig.ControlAddress = f.source.Config.GRPCAddress
	agentConfig.DataDirectory = filepath.Dir(f.agentDirectory)
	agentConfig.CorePath = filepath.Join(t.TempDir(), "missing-xray")
	agentConfig.TLS.ClientCA = filepath.Join(f.agentDirectory, "node-ca.crt")
	firstHTTP, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		firstGRPC.Close()
		t.Fatal(err)
	}
	firstServer, err := server.New(f.source.Config, f.source.Database, fstest.MapFS{"index.html": {Data: []byte("UI")}}, logger)
	if err != nil {
		firstHTTP.Close()
		firstGRPC.Close()
		t.Fatal(err)
	}
	firstCtx, stopFirst := context.WithCancel(ctx)
	firstDone := make(chan error, 1)
	go func() { firstDone <- firstServer.Serve(firstCtx, firstHTTP, firstGRPC) }()
	firstAgentCtx, stopFirstAgent := context.WithCancel(ctx)
	firstAgentDone := make(chan error, 1)
	go func() { firstAgentDone <- control.Run(firstAgentCtx, agentConfig, logger) }()
	firstStopped := false
	defer func() {
		if !firstStopped {
			stopFirstAgent()
			stopFirst()
			select {
			case <-firstAgentDone:
			case <-time.After(5 * time.Second):
			}
			select {
			case <-firstDone:
			case <-time.After(5 * time.Second):
			}
		}
	}()
	beforeDeadline := time.Now().Add(8 * time.Second)
	var firstAck uint64
	for {
		state, err := identity.Load(f.agentDirectory)
		if err == nil && state.LastAcked >= 2 {
			firstAck = state.LastAcked
			break
		}
		if time.Now().After(beforeDeadline) {
			t.Fatal("original Agent never established control connection")
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopFirstAgent()
	select {
	case err := <-firstAgentDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original Agent shutdown timeout")
	}
	stopFirst()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original service shutdown timeout")
	}
	firstStopped = true
	candidate, err := PrepareDatabase(ctx, f.owner, f.source, f.prepared, recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := InstallAndPrepare(ctx, f.owner, f.source, candidate, f.prepared, recoveryPassword, fstest.MapFS{"index.html": {Data: []byte("UI")}}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	// Serve requires the original startup result recorder that cmd/panel and
	// panelhost bind from the restore task store; record its single call here.
	startupRecorded := make(chan struct{}, 1)
	if err := runtime.ConfigureStartupRecorder(func(context.Context) error {
		startupRecorded <- struct{}{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	httpAddress, grpcAddress := runtime.Addresses()
	for _, address := range []string{httpAddress, grpcAddress} {
		if listener, err := net.Listen("tcp", address); err == nil {
			listener.Close()
			t.Fatal("prepared port can be stolen before service activation", address)
		}
	}
	// Nothing serves a request before activation, and no worker writes have run.
	client := &http.Client{Timeout: 80 * time.Millisecond}
	if response, err := client.Get("http://" + httpAddress + "/healthz"); err == nil {
		response.Body.Close()
		t.Fatal("precommit reservation served HTTP")
	}
	readonly, err := db.OpenImmutable(ctx, f.source.Config.Database)
	if err != nil {
		t.Fatal(err)
	}
	assertInt(t, readonly, "SELECT count(*) FROM sessions", 0)
	assertInt(t, readonly, "SELECT used_traffic FROM users WHERE id=?", 500, f.user.ID)
	readonly.Close()
	serviceCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runtime.Serve(serviceCtx) }()
	defer func() {
		stop()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("restored service shutdown timeout")
		}
	}()
	client.Timeout = time.Second
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get("http://" + httpAddress + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("reserved service failed to become healthy")
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-startupRecorded:
	default:
		t.Fatal("restored service served without recording its startup result")
	}
	agentConfig.ControlAddress = grpcAddress
	agentCtx, stopAgent := context.WithCancel(ctx)
	agentDone := make(chan error, 1)
	go func() { agentDone <- control.Run(agentCtx, agentConfig, logger) }()
	defer func() {
		stopAgent()
		select {
		case err := <-agentDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("reconnected Agent did not stop")
		}
	}()
	database, err := db.Open(ctx, f.source.Config.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &node.Repository{Database: database}
	deadline = time.Now().Add(8 * time.Second)
	for {
		current, err := repository.Get(ctx, f.node.ID)
		if err == nil && current.LastSequence >= firstAck+2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual Agent failed to reconnect with restored certificate")
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		state, err := identity.Load(f.agentDirectory)
		if err == nil && state.LastAcked >= firstAck+2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rejoined Agent did not persist ACK")
		}
		time.Sleep(20 * time.Millisecond)
	}
	assertInt(t, database, "SELECT used_traffic FROM users WHERE id=?", 500, f.user.ID)
}
