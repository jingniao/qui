package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/testutil"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "panel.db")
	database, err := db.Open(context.Background(), databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	files := fstest.MapFS{"index.html": {Data: []byte("<html>Proxy Control</html>")}, "assets/app.js": {Data: []byte("window.app=true")}}
	c := config.DefaultPanel()
	c.Database = databasePath
	c.NodeIdentityDirectory = filepath.Join(t.TempDir(), "identity")
	app, err := New(c, database, files, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestHTTPHealthAndSPARouting(t *testing.T) {
	app := testServer(t)
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"GET", "/healthz", 200}, {"GET", "/users/user-1", 200}, {"GET", "/assets/app.js", 200}, {"GET", "/assets/missing.js", 404}, {"GET", "/api/v1/missing", 401}, {"GET", "/sub/token", 403}, {"POST", "/dashboard", 405}, {"GET", "/.gitkeep", 404}} {
		w := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.status {
			t.Fatalf("%s %s: %d %s", tt.method, tt.path, w.Code, w.Body.String())
		}
		if w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("security headers missing")
		}
	}
	app.Database.Close()
	w := httptest.NewRecorder()
	app.HTTP.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 503 {
		t.Fatal("closed database reported ready")
	}
}

func TestHTTPAndGRPCStartAndStopTogether(t *testing.T) {
	app := testServer(t)
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		httpListener.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, httpListener, grpcListener) }()
	directory := t.TempDir()
	csrData, err := identity.CSR(directory)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(csrData)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := app.Authority.IssueNode(csr, "health-test")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(directory, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(encoded, key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(app.Authority.PEM)
	connection, err := grpc.NewClient(grpcListener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}})))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	timeout, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	status, err := healthpb.NewHealthClient(connection).Check(timeout, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	if err != nil || status.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("gRPC health: %v %v", status, err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + httpListener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish")
	}
}

func TestProductionTLSRequiresNodeCertificateButNotBrowserCertificate(t *testing.T) {
	app := testServer(t)
	certs := testutil.Certificates(t)
	c := app.Config
	c.Development = false
	c.TLS = certs.Server
	secured, err := New(c, app.Database, fstest.MapFS{"index.html": {Data: []byte("UI")}}, app.Logger)
	if err != nil {
		t.Fatal(err)
	}
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		httpListener.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- secured.Serve(ctx, httpListener, grpcListener) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("secure shutdown timed out")
		}
	}()
	connection, err := grpc.NewClient(grpcListener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(certs.RootOnly)))
	if err != nil {
		t.Fatal(err)
	}
	timeout, stop := context.WithTimeout(ctx, time.Second)
	_, err = healthpb.NewHealthClient(connection).Check(timeout, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
	stop()
	connection.Close()
	if err == nil {
		t.Fatal("gRPC accepted a caller without node certificate")
	}
	// The gRPC listener authenticates nodes against the panel node CA
	// (node.EnrollmentTLS uses authority.VerificationPEM), so the client must
	// present a certificate issued by app.Authority, not the panel TLS CA.
	directory := t.TempDir()
	csrData, err := identity.CSR(directory)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(csrData)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _, err := app.Authority.IssueNode(csr, "browser-tls-test")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(directory, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodeCertificate, err := tls.X509KeyPair(encoded, key)
	if err != nil {
		t.Fatal(err)
	}
	clientTLS := certs.RootOnly.Clone()
	clientTLS.Certificates = []tls.Certificate{nodeCertificate}
	connection, err = grpc.NewClient(grpcListener.Addr().String(), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	timeout, stop = context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	if _, err := healthpb.NewHealthClient(connection).Check(timeout, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true)); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: certs.RootOnly}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("https://" + httpListener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
}
