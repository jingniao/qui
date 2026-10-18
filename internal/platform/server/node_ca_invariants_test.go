package server

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/testutil"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// startProductionServer starts a non-development panel with the testutil TLS
// material and returns it with its HTTP and gRPC listener addresses.
func startProductionServer(t *testing.T) (*Server, testutil.PKI, string, string) {
	t.Helper()
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
	done := make(chan error, 1)
	go func() { done <- secured.Serve(ctx, httpListener, grpcListener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("secure shutdown timed out")
		}
	})
	return secured, certs, httpListener.Addr().String(), grpcListener.Addr().String()
}

// TestGRPCRequiresNodeCAClientCertificate proves the gRPC control port requires
// a client certificate and verifies it against the node CA chain: a certificate
// issued by another CA (here the panel TLS CA) is refused, while the same
// transport without any client certificate is also refused.
func TestGRPCRequiresNodeCAClientCertificate(t *testing.T) {
	_, certs, _, grpcAddress := startProductionServer(t)
	otherCA, err := tls.LoadX509KeyPair(certs.Client.Certificate, certs.Client.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		config *tls.Config
	}{
		{"no certificate", certs.RootOnly.Clone()},
		{"certificate from another CA", func() *tls.Config { c := certs.RootOnly.Clone(); c.Certificates = []tls.Certificate{otherCA}; return c }()},
	} {
		connection, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(credentials.NewTLS(tc.config)))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, checkErr := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{}, grpc.WaitForReady(true))
		cancel()
		connection.Close()
		if checkErr == nil {
			t.Fatalf("gRPC accepted %s", tc.name)
		}
	}
}

// TestHTTPSDoesNotRequestClientCertificate proves the HTTPS panel port never
// sends a TLS CertificateRequest: a client that would answer one is served
// normally and its callback is never invoked.
func TestHTTPSDoesNotRequestClientCertificate(t *testing.T) {
	_, certs, httpAddress, _ := startProductionServer(t)
	requested := false
	config := certs.RootOnly.Clone()
	config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		requested = true
		return nil, nil
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get("https://" + httpAddress + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status %d", response.StatusCode)
	}
	if requested {
		t.Fatal("HTTPS server requested a client certificate")
	}
}

// TestEnrollmentTokenIsSingleUse proves an enrollment token is consumed by the
// first enrollment: a second enrollment with the same token but a different
// identity is refused.
func TestEnrollmentTokenIsSingleUse(t *testing.T) {
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	repository := &node.Repository{Database: app.Database}
	if _, token, _, err := repository.CreateEnrollment(context.Background(), "single-use-node", time.Hour); err != nil {
		t.Fatal(err)
	} else {
		connection := trustedClient(t, address, app.Authority.PEM, nil)
		client := pb.NewAgentControlClient(connection)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		firstCSR, err := identity.CSR(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: firstCSR, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "test"}); err != nil {
			t.Fatalf("first enrollment: %v", err)
		}
		secondCSR, err := identity.CSR(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: secondCSR, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "test"}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("consumed enrollment token accepted a second identity: %v", err)
		}
	}
}
