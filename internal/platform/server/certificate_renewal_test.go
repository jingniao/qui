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
	"io"
	"net"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/pki"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"strconv"
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

func TestActualAgentAutomaticallyRenewsAndReconnectsWithoutResettingACK(t *testing.T) {
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	n, c, _ := nearExpiryNode(t, app)
	c.ControlAddress = address
	var before string
	app.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", n.ID).Scan(&before)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- control.Run(ctx, c, app.Logger) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("renewing Agent did not stop")
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var current string
		app.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", n.ID).Scan(&current)
		state, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
		if err == nil && current != before && state.LastAcked >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent failed automatic renewal and control reconnection")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRenewalCannotReplaceAuthenticatedKeyOrBypassRevocation(t *testing.T) {
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

func TestRealXrayExistingConnectionSurvivesAutomaticCertificateRenewal(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY for renewal data-plane continuity")
	}
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	n, c, _ := nearExpiryNode(t, app)
	c.ControlAddress = address
	c.CorePath = binary
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer origin.Close()
	go func() {
		for {
			connection, err := origin.Accept()
			if err != nil {
				return
			}
			go func() { defer connection.Close(); io.Copy(connection, connection) }()
		}
	}()
	key, err := vault.Open(filepath.Join(app.Config.NodeIdentityDirectory, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	repository := &deployment.Repository{Database: app.Database, Vault: key}
	state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Inbounds: []desiredstate.Inbound{{ID: "socks", Protocol: "socks5", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct"}}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	revision, err := repository.Create(context.Background(), state, "owner", "renewal continuity")
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Deploy(context.Background(), revision.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Result(context.Background(), n.ID, &pb.ApplyResult{Revision: revision.ID, Success: true}); err != nil {
		t.Fatal(err)
	}
	state, err = repository.State(context.Background(), revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &xray.Adapter{Path: binary, Directory: filepath.Join(c.DataDirectory, "runtime"), NodeID: n.ID}
	manager, err := reconcile.New(c.DataDirectory, n.ID, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := manager.Apply(context.Background(), state); err != nil || !result.Success {
		adapter.Close()
		t.Fatal("local retained config failed", result, err)
	}
	adapter.Close()
	var before string
	app.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", n.ID).Scan(&before)
	// Hold the panel connection while the Agent restores its local core. This
	// creates a live TCP session before control registration and renewal proceed.
	blocker, err := app.Database.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- control.Run(ctx, c, app.Logger) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("renewing data-plane Agent did not stop")
		}
	}()
	var connection net.Conn
	deadline := time.Now().Add(15 * time.Second)
	for {
		connection, err = net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent did not restore retained proxy")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(20 * time.Second))
	connection.Write([]byte{5, 1, 0})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil || reply[1] != 0 {
		t.Fatal("SOCKS greeting failed", err)
	}
	target := origin.Addr().(*net.TCPAddr)
	connection.Write([]byte{5, 1, 0, 1, 127, 0, 0, 1, byte(target.Port >> 8), byte(target.Port)})
	accepted := make([]byte, 10)
	if _, err := io.ReadFull(connection, accepted); err != nil || accepted[1] != 0 {
		t.Fatal("SOCKS target failed", err)
	}
	connection.Write([]byte("ping"))
	echo := make([]byte, 4)
	if _, err := io.ReadFull(connection, echo); err != nil || string(echo) != "ping" {
		t.Fatal("pre-renewal data transfer failed", err)
	}
	if err := blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		var current string
		app.Database.QueryRow("SELECT fingerprint FROM node_identities WHERE node_id=?", n.ID).Scan(&current)
		local, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
		if err == nil && current != before && local.LastAcked >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("certificate renewal did not confirm")
		}
		time.Sleep(20 * time.Millisecond)
	}
	connection.Write([]byte("pong"))
	if _, err := io.ReadFull(connection, echo); err != nil || string(echo) != "pong" {
		t.Fatal("renewal restarted core or broke existing TCP session", err)
	}
	current, err := repository.Current(context.Background(), n.ID)
	if err != nil || current != revision.ID {
		t.Fatal("renewal changed deployed revision", current, err)
	}
}

// TestDisabledNodeIsRefusedAtMTLS proves that disabling a node denies its
// existing certificate on the mTLS control channel, and that re-enabling it
// restores access. This is the supported replacement path for a lost node:
// disable it (which revokes mTLS access) and enroll a new one.
func TestDisabledNodeIsRefusedAtMTLS(t *testing.T) {
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
