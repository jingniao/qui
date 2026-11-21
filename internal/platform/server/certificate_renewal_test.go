package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func nearExpiryNode(t *testing.T, app *Server) (node.Node, config.Agent, []byte) {
	t.Helper()
	ctx := context.Background()
	repository := &node.Repository{Database: app.Database}
	n, token, _, err := repository.CreateEnrollment(ctx, "Renewing node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.DefaultAgent()
	configuration.DataDirectory = filepath.Join(t.TempDir(), "agent")
	configuration.CorePath = filepath.Join(t.TempDir(), "missing-xray")
	directory := filepath.Join(configuration.DataDirectory, "identity")
	csr, err := identity.CSR(directory)
	if err != nil {
		t.Fatal(err)
	}
	request, err := pki.ParseCSR(csr)
	if err != nil {
		t.Fatal(err)
	}
	encoded, leaf, err := app.Authority.IssueNode(request, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	leaf.NotAfter = time.Now().Add(10 * 24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, app.Authority.Certificate, request.PublicKey, app.Authority.Key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	encoded = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := repository.ConsumeEnrollment(ctx, node.Hash(token), n.ID, "host", "test", "dev", string(encoded), pki.Fingerprint(leaf), leaf.NotAfter); err != nil {
		t.Fatal(err)
	}
	if err := identity.SaveEnrollment(directory, n.ID, encoded, app.Authority.PEM); err != nil {
		t.Fatal(err)
	}
	configuration.TLS.ClientCA = filepath.Join(directory, "node-ca.crt")
	return n, configuration, csr
}

func renewalClient(t *testing.T, c config.Agent, address string) *grpc.ClientConn {
	t.Helper()
	tlsConfig, err := c.TLS.ServerTrustConfig(c.ServerName)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(filepath.Join(c.DataDirectory, "identity", "client.crt"), filepath.Join(c.DataDirectory, "identity", "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig.Certificates = []tls.Certificate{certificate}
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	return connection
}

func TestRenewalLostResponseIsIdempotentAndPossessionActivatesNewIdentity(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	n, c, csr := nearExpiryNode(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	oldClient := pb.NewAgentControlClient(renewalClient(t, c, address))
	first, err := oldClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr})
	if err != nil {
		t.Fatal(err)
	}
	second, err := oldClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr})
	if err != nil || !bytes.Equal(first.CertificatePem, second.CertificatePem) {
		t.Fatal("lost response issued a conflicting identity", err)
	}
	var before string
	app.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", n.ID).Scan(&before)
	state, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(17); err != nil {
		t.Fatal(err)
	}
	privateBefore, _ := os.ReadFile(filepath.Join(state.Directory, "client.key"))
	if err := state.SaveRenewal(first.CertificatePem, first.CaPem); err != nil {
		t.Fatal(err)
	}
	privateAfter, _ := os.ReadFile(filepath.Join(state.Directory, "client.key"))
	if !bytes.Equal(privateBefore, privateAfter) {
		t.Fatal("renewal changed local private key")
	}
	loaded, err := identity.Load(state.Directory)
	if err != nil || loaded.LastAcked != 17 {
		t.Fatal("renewal reset ACK", loaded, err)
	}
	newClient := pb.NewAgentControlClient(renewalClient(t, c, address))
	// The unary request authenticates new certificate possession and activates
	// it, then rejects another premature renewal of the new 90-day certificate.
	if _, err := newClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("new identity did not authenticate", err)
	}
	var after string
	app.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", n.ID).Scan(&after)
	if after == before {
		t.Fatal("new certificate possession was not activated")
	}
	if _, err := oldClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("old identity remained usable after confirmation", err)
	}
}

func TestRenewalCannotReplaceAuthenticatedKeyOrBypassRevocation(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	n, c, csr := nearExpiryNode(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := pb.NewAgentControlClient(renewalClient(t, c, address))
	otherCSR, err := identity.CSR(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: otherCSR}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unrelated private key renewal accepted", err)
	}
	if _, err := client.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); err != nil {
		t.Fatal(err)
	}
	if err := (&node.Repository{Database: app.Database}).Revoke(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("revoked node renewed", err)
	}
	var pending int
	app.Database.QueryRow("SELECT count(*) FROM node_certificate_renewals WHERE node_id=?", n.ID).Scan(&pending)
	if pending != 0 {
		t.Fatal("revoked pending certificate retained")
	}
}

func TestCertificateRenewalAndActivationAuditFailureLeaveOldIdentityUsable(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	n, c, csr := nearExpiryNode(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	oldClient := pb.NewAgentControlClient(renewalClient(t, c, address))
	if _, err := app.Database.Exec("CREATE TRIGGER refuse_issue_audit BEFORE INSERT ON admin_operations WHEN NEW.action='签发节点续期证书' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := oldClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("audit failure returned issued renewal", err)
	}
	var pending int
	app.Database.QueryRow("SELECT count(*) FROM node_certificate_renewals WHERE node_id=?", n.ID).Scan(&pending)
	if pending != 0 {
		t.Fatal("failed audit left issued certificate")
	}
	if _, err := app.Database.Exec("DROP TRIGGER refuse_issue_audit"); err != nil {
		t.Fatal(err)
	}
	issued, err := oldClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr})
	if err != nil {
		t.Fatal(err)
	}
	state, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveRenewal(issued.CertificatePem, issued.CaPem); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Database.Exec("CREATE TRIGGER refuse_confirm_audit BEFORE INSERT ON admin_operations WHEN NEW.action='确认节点续期证书' BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	newClient := pb.NewAgentControlClient(renewalClient(t, c, address))
	if _, err := newClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("uncommitted activation returned valid identity", err)
	}
	if response, err := oldClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); err != nil || !bytes.Equal(response.CertificatePem, issued.CertificatePem) {
		t.Fatal("failed activation invalidated old identity or pending retry", err)
	}
	if _, err := app.Database.Exec("DROP TRIGGER refuse_confirm_audit"); err != nil {
		t.Fatal(err)
	}
	if _, err := newClient.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); status.Code(err) != codes.FailedPrecondition {
		t.Fatal("activation cannot recover after audit repair", err)
	}
}

// TestDisabledNodeIsRefusedAtMTLS proves that disabling a node denies its
// existing certificate on the mTLS control channel, and that re-enabling it
// restores access. This is the supported replacement path for a lost node:
// disable it (which revokes mTLS access) and enroll a new one.
func TestDisabledNodeIsRefusedAtMTLS(t *testing.T) {
	t.Parallel()
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	n, c, csr := nearExpiryNode(t, app)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repository := &node.Repository{Database: app.Database}
	client := pb.NewAgentControlClient(renewalClient(t, c, address))
	if err := repository.UpdateSettings(ctx, n.ID, node.SettingsInput{Name: n.Name, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("disabled node was not refused at mTLS: %v", err)
	}
	if err := repository.UpdateSettings(ctx, n.ID, node.SettingsInput{Name: n.Name, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr}); err != nil {
		t.Fatalf("re-enabled node was not accepted: %v", err)
	}
}
