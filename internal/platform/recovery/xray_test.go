package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/deployment"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/server"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestRealXrayContinuesOfflineAndReplaysTrafficAfterCentralRestore(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for actual recovery data plane verification")
	}
	var grpcListener net.Listener
	f := listeningFixture(t, func(f *fixture) {
		var err error
		grpcListener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		f.source.Config.GRPCAddress = grpcListener.Addr().String()
		f.source.Config.ShutdownTimeout = time.Second
	})
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := f.source.Database.Exec("UPDATE users SET traffic_limit=? WHERE id=?", 10<<20, f.user.ID); err != nil {
		t.Fatal(err)
	}
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyPort := portListener.Addr().(*net.TCPAddr).Port
	portListener.Close()
	state := f.state
	state.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	state.Inbounds[0].Port = proxyPort
	revision, err := f.deployments.Create(ctx, state, "owner", "real recovery proxy")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.deployments.Deploy(ctx, revision.ID); err != nil {
		t.Fatal(err)
	}
	// The chosen backup precedes actual application and native consumption.
	root := t.TempDir()
	created, err := f.source.Create(ctx, root, "real_xray_snapshot_01", "owner", recoveryPassword)
	if err != nil {
		t.Fatal(err)
	}
	f.prepared, err = archive.PrepareRestore(ctx, filepath.Join(t.TempDir(), "stages"), filepath.Join(root, created.Manifest.ID+".pcb"), recoveryPassword, f.prepared.Plan.Target)
	if err != nil {
		t.Fatal(err)
	}
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app, err := server.New(f.source.Config, f.source.Database, fstest.MapFS{"index.html": {Data: []byte("UI")}}, logger)
	if err != nil {
		t.Fatal(err)
	}
	panelCtx, stopPanel := context.WithCancel(ctx)
	panelDone := make(chan error, 1)
	go func() { panelDone <- app.Serve(panelCtx, httpListener, grpcListener) }()
	panelStopped := false
	defer func() {
		if !panelStopped {
			stopPanel()
			select {
			case <-panelDone:
			case <-time.After(5 * time.Second):
				t.Error("original panel shutdown timeout")
			}
		}
	}()
	agentConfig := config.DefaultAgent()
	agentConfig.ControlAddress = f.source.Config.GRPCAddress
	agentConfig.DataDirectory = filepath.Dir(f.agentDirectory)
	agentConfig.CorePath = binary
	agentConfig.TLS.ClientCA = filepath.Join(f.agentDirectory, "node-ca.crt")
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
		case <-time.After(8 * time.Second):
			t.Error("recovery Agent shutdown timeout")
		}
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		current, err := f.deployments.Current(ctx, f.node.ID)
		if err == nil && current == revision.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real recovery configuration did not apply")
		}
		time.Sleep(30 * time.Millisecond)
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, strings.Repeat("r", 2048)) }))
	defer origin.Close()
	request := func() {
		proxyURL, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(proxyPort))
		credential := f.device.Credentials[0]
		proxyURL.User = url.UserPassword(credential.Username, credential.Secret)
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		response, err := (&http.Client{Transport: transport, Timeout: 3 * time.Second}).Get(origin.URL)
		if err != nil {
			t.Fatal("real proxy failed across recovery", err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || len(body) != 2048 {
			t.Fatal("proxy response incomplete", len(body), err)
		}
	}
	request()
	deadline = time.Now().Add(30 * time.Second)
	var before int64
	for {
		current, err := f.users.Get(ctx, f.user.ID)
		if err == nil && current.UsedTraffic > 300 {
			before = current.UsedTraffic
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("original native traffic did not reach central ledger")
		}
		time.Sleep(50 * time.Millisecond)
	}
	stopPanel()
	select {
	case err := <-panelDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("panel did not stop while Agent continued")
	}
	panelStopped = true
	request()
	request()
	queue, key := queueObserver(t, agentConfig.DataDirectory)
	defer queue.Close()
	deadline = time.Now().Add(15 * time.Second)
	var pending traffic.Batch
	for {
		var payload []byte
		if err := queue.QueryRow("SELECT payload FROM traffic_batches ORDER BY rowid LIMIT 1").Scan(&payload); err == nil {
			plain, err := key.Open(payload)
			if err != nil {
				t.Fatal(err)
			}
			batch, err := traffic.Decode(plain)
			clear(plain)
			if err != nil {
				t.Fatal(err)
			}
			pending = batch
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("offline native traffic was not persisted")
		}
		time.Sleep(50 * time.Millisecond)
	}
	current, err := f.users.Get(ctx, f.user.ID)
	if err != nil || current.UsedTraffic != before {
		t.Fatal("offline consumption fabricated centrally", current.UsedTraffic, before, err)
	}
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
	reserved, _ := db.OpenImmutable(ctx, f.source.Config.Database)
	if reserved == nil {
		t.Fatal("restored ledger cannot be inspected")
	}
	assertInt(t, reserved, "SELECT used_traffic FROM users WHERE id=?", before, f.user.ID)
	reserved.Close()
	restoredCtx, stopRestored := context.WithCancel(ctx)
	restoredDone := make(chan error, 1)
	go func() { restoredDone <- runtime.Serve(restoredCtx) }()
	defer func() {
		stopRestored()
		select {
		case err := <-restoredDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("restored panel shutdown timeout")
		}
	}()
	request()
	select {
	case <-startupRecorded:
	default:
		t.Fatal("restored service served without recording its startup result")
	}
	database, err := db.Open(ctx, f.source.Config.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	deadline = time.Now().Add(60 * time.Second)
	for {
		var used int64
		var count int
		if err := database.QueryRow("SELECT used_traffic FROM users WHERE id=?", f.user.ID).Scan(&used); err != nil {
			t.Fatal(err)
		}
		queue.QueryRow("SELECT count(*) FROM traffic_batches").Scan(&count)
		if count == 0 && used > before {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovery did not reconcile actual offline/native consumption exactly")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Replay a real captured encrypted queue batch after its acknowledged upload.
	var beforeReplay int64
	database.QueryRow("SELECT used_traffic FROM users WHERE id=?", f.user.ID).Scan(&beforeReplay)
	users := &user.Repository{Database: database, Vault: f.source.ConfigurationVault}
	deployments := &deployment.Repository{Database: database, Vault: f.source.ConfigurationVault}
	data, _ := json.Marshal(pending)
	if _, err := (&access.Service{Repository: &access.Repository{Database: database}, Snapshots: deployments, Users: users}).RecordTraffic(ctx, f.node.ID, &pb.TrafficBatch{BatchJson: data}, false); err != nil {
		t.Fatal("acknowledged offline batch could not replay", err)
	}
	assertInt(t, database, "SELECT used_traffic FROM users WHERE id=?", beforeReplay, f.user.ID)
}

func queueObserver(t *testing.T, directory string) (*sql.DB, *vault.Vault) {
	t.Helper()
	absolute, err := filepath.Abs(filepath.Join(directory, "spool", "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(absolute)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "busy_timeout(5000)"}}.Encode()}
	database, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	keyBytes, err := os.ReadFile(filepath.Join(directory, "spool", "spool.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := vault.FromKey(keyBytes)
	clear(keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	return database, key
}
