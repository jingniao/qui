package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/control"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/db"
	"proxycontrol/internal/platform/filelock"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing/fstest"
)

func startControl(t *testing.T, app *Server) (string, func()) {
	t.Helper()
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
	go func() { done <- app.Serve(ctx, httpListener, grpcListener) }()
	return grpcListener.Addr().String(), func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(12 * time.Second):
			t.Error("control shutdown timed out")
		}
	}
}

func trustedClient(t *testing.T, address string, ca []byte, certificate *tls.Certificate) *grpc.ClientConn {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	transport := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
	if certificate != nil {
		transport.Certificates = []tls.Certificate{*certificate}
	}
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(transport)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	return connection
}

func TestNodeEnrollmentIdentityAndDuplicateMessages(t *testing.T) {
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	repository := &node.Repository{Database: app.Database}
	pending, token, _, err := repository.CreateEnrollment(context.Background(), "test-node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	connection := trustedClient(t, address, app.Authority.PEM, nil)
	client := pb.NewAgentControlClient(connection)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	directory := t.TempDir()
	csr, err := identity.CSR(directory)
	if err != nil {
		t.Fatal(err)
	}
	request := &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "test"}
	bad := proto.Clone(request).(*pb.EnrollRequest)
	bad.CsrPem = []byte("not a CSR")
	if _, err := client.Enroll(ctx, bad); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid CSR accepted: %v", err)
	}
	enrollment, err := client.Enroll(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if enrollment.NodeId != pending.ID {
		t.Fatal("node ID mismatch")
	}
	// F3-05e (docs/node-enrollment-original-replay.md): a lost enrollment
	// response may be resumed for the same one-time token, public key and
	// machine metadata while the original token window is current and the node
	// has not started business. The one-time token must still not enroll a
	// different identity, so a different CSR public key is rejected.
	replayed, err := client.Enroll(ctx, request)
	if err != nil || replayed.NodeId != pending.ID || !bytes.Equal(replayed.CertificatePem, enrollment.CertificatePem) {
		t.Fatalf("same-request enrollment resume: %v", err)
	}
	otherCSR, err := identity.CSR(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	different := proto.Clone(request).(*pb.EnrollRequest)
	different.CsrPem = otherCSR
	if _, err := client.Enroll(ctx, different); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("enrollment token accepted a different identity: %v", err)
	}
	if err := identity.SaveEnrollment(directory, enrollment.NodeId, enrollment.CertificatePem, enrollment.CaPem); err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(filepath.Join(directory, "client.crt"), filepath.Join(directory, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodeConnection := trustedClient(t, address, app.Authority.PEM, &certificate)
	nodeClient := pb.NewAgentControlClient(nodeConnection)
	anonymous, err := client.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	anonymous.Send(&pb.AgentMessage{NodeId: pending.ID})
	if _, err := anonymous.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatal("anonymous node connection accepted", err)
	}
	stream, err := nodeClient.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream.Send(&pb.AgentMessage{NodeId: pending.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{}}})
	welcome, err := stream.Recv()
	if err != nil || welcome.GetWelcome() == nil {
		t.Fatal("missing Welcome", err)
	}
	capabilities := &pb.AgentMessage{NodeId: pending.ID, MessageId: "capabilities", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", CoreVersion: "test", Protocols: []*pb.ProtocolCapability{{Protocol: "socks5", Inbound: true, Tcp: true, Udp: true}}}}}
	if err := stream.Send(capabilities); err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil || ack.GetAck().AgentSequence != 1 {
		t.Fatal("capability ACK", err)
	}
	heartbeat := &pb.AgentMessage{NodeId: pending.ID, MessageId: "heartbeat", Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{CpuPercent: proto.Float64(23), MemoryPercent: proto.Float64(45), PublicIp: "192.0.2.15"}}}
	if err := stream.Send(heartbeat); err != nil {
		t.Fatal(err)
	}
	ack, err = stream.Recv()
	if err != nil || ack.GetAck().AgentSequence != 2 {
		t.Fatal("heartbeat ACK", err)
	}
	// R1-03j (docs/node-latest-accepted-message.md): an identical resend of the
	// last accepted sequence is acknowledged idempotently; a same-sequence
	// message whose digest differs is rejected (covered by
	// TestNodeRejectsConflictingDuplicateSequence).
	if err := stream.Send(heartbeat); err != nil {
		t.Fatal(err)
	}
	ack, err = stream.Recv()
	if err != nil || ack.GetAck().AgentSequence != 2 {
		t.Fatal("duplicate ACK", err)
	}
	recorded, err := repository.Get(ctx, pending.ID)
	if err != nil || recorded.Status != "online" || recorded.Heartbeat.GetCpuPercent() != 23 || recorded.LastSequence != 2 {
		t.Fatalf("incorrect persisted node: %+v %v", recorded, err)
	}
	state, err := identity.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(2); err != nil {
		t.Fatal(err)
	}
	state, err = identity.Load(directory)
	if err != nil || state.LastAcked != 2 {
		t.Fatal("agent ACK not persisted")
	}
	if err := repository.Revoke(ctx, pending.ID); err != nil {
		t.Fatal(err)
	}
	heartbeat.Sequence = 3
	stream.Send(heartbeat)
	if _, err := stream.Recv(); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked certificate continued: %v", err)
	}
}

// R1-03j (docs/node-latest-accepted-message.md): the same last accepted
// sequence carrying a different overall message digest must be rejected before
// any business change, so a node cannot rewrite an already accepted message.
func TestNodeRejectsConflictingDuplicateSequence(t *testing.T) {
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	repository := &node.Repository{Database: app.Database}
	pending, token, _, err := repository.CreateEnrollment(context.Background(), "conflict-node", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	connection := trustedClient(t, address, app.Authority.PEM, nil)
	client := pb.NewAgentControlClient(connection)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	directory := t.TempDir()
	csr, err := identity.CSR(directory)
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := client.Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.SaveEnrollment(directory, enrollment.NodeId, enrollment.CertificatePem, enrollment.CaPem); err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(filepath.Join(directory, "client.crt"), filepath.Join(directory, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodeConnection := trustedClient(t, address, app.Authority.PEM, &certificate)
	nodeClient := pb.NewAgentControlClient(nodeConnection)
	stream, err := nodeClient.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stream.Send(&pb.AgentMessage{NodeId: pending.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{}}})
	if _, err := stream.Recv(); err != nil {
		t.Fatal("missing Welcome", err)
	}
	heartbeat := &pb.AgentMessage{NodeId: pending.ID, MessageId: "heartbeat", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{CpuPercent: proto.Float64(23)}}}
	if err := stream.Send(heartbeat); err != nil {
		t.Fatal(err)
	}
	if ack, err := stream.Recv(); err != nil || ack.GetAck().AgentSequence != 1 {
		t.Fatal("heartbeat ACK", err)
	}
	conflict := proto.Clone(heartbeat).(*pb.AgentMessage)
	conflict.GetHeartbeat().CpuPercent = proto.Float64(99)
	stream.Send(conflict)
	if _, err := stream.Recv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("conflicting duplicate sequence accepted: %v", err)
	}
}

func TestAgentEnrollsAndSendsRealControlHeartbeat(t *testing.T) {
	app := testServer(t)
	address, stop := startControl(t, app)
	defer stop()
	repository := &node.Repository{Database: app.Database}
	pending, token, _, err := repository.CreateEnrollment(context.Background(), "actual-agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	c := config.DefaultAgent()
	c.ControlAddress = address
	c.DataDirectory = filepath.Join(directory, "agent")
	c.TLS.ClientCA = caPath
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := control.Enroll(ctx, c, token, "test-agent"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- control.Run(ctx, c, app.Logger) }()
	deadline := time.Now().Add(5 * time.Second)
	online := false
	for time.Now().Before(deadline) {
		n, err := repository.Get(ctx, pending.ID)
		if err == nil && n.Status == "online" {
			online = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !online {
		t.Fatal("Agent did not report heartbeat")
	}
	keyPath := filepath.Join(c.DataDirectory, "identity", "client.key")
	originalKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := control.Run(ctx, c, app.Logger); !errors.Is(err, filelock.ErrBusy) {
		t.Fatal("second Agent did not reject live ownership", err)
	}
	if err := control.Enroll(ctx, c, "unused-token", "duplicate-agent"); !errors.Is(err, filelock.ErrBusy) {
		t.Fatal("enrollment bypassed live ownership", err)
	}
	retainedKey, err := os.ReadFile(keyPath)
	if err != nil || string(retainedKey) != string(originalKey) {
		t.Fatal("duplicate Agent changed node identity", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Agent did not stop")
	}
	if _, err := identity.Load(filepath.Join(c.DataDirectory, "identity")); err != nil {
		t.Fatal(err)
	}
	// A normal stop must release both guards after its workers and core close.
	for _, path := range []string{filepath.Join(c.DataDirectory, ".agent-owner.lock"), filepath.Join(c.DataDirectory, "identity", ".agent-owner.lock")} {
		lock, err := filelock.Try(path)
		if err != nil {
			t.Fatal("Agent shutdown retained ownership", err)
		}
		lock.Close()
	}
}

func TestNodeHTTPAuthorizationAndExpiredEnrollment(t *testing.T) {
	app := testServer(t)
	call := func(method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.RemoteAddr = "192.0.2.1:123"
		request.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		request.Header.Set("X-CSRF-Token", csrf)
		response := httptest.NewRecorder()
		app.HTTP.Handler.ServeHTTP(response, request)
		return response
	}
	response := call("POST", "/api/v1/auth/setup", `{"token":"`+app.SetupToken()+`","username":"owner","password":"node-test-password-123","platformName":"Test","timezone":"UTC"}`, nil, "")
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	var session auth.Session
	json.Unmarshal(response.Body.Bytes(), &session)
	response = call("POST", "/api/v1/nodes/enrollment-token", `{"name":"Node A"}`, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	var enrolled struct {
		Node            node.Node
		EnrollmentToken string
		Install         node.InstallInfo
	}
	json.Unmarshal(response.Body.Bytes(), &enrolled)
	if enrolled.EnrollmentToken == "" || enrolled.Node.Status != "pending" {
		t.Fatal("missing enrollment")
	}
	// The development panel has no certificate to trust and no release version,
	// so the answer carries the fields empty instead of an install command.
	if enrolled.Install.Version != "" || enrolled.Install.CASHA256 != "" {
		t.Fatalf("development enrollment carried install metadata: %+v", enrolled.Install)
	}
	response = call("GET", "/api/v1/nodes", "", cookie, "")
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	if strings.Contains(response.Body.String(), enrolled.EnrollmentToken) || strings.Contains(response.Body.String(), "certificate_pem") {
		t.Fatal("node list leaked credentials")
	}
	response = call("POST", "/api/v1/admins", `{"username":"viewer","password":"node-test-password-123","displayName":"Viewer","role":"Viewer"}`, cookie, session.CSRFToken)
	if response.Code != 201 {
		t.Fatal(response.Body.String())
	}
	response = call("POST", "/api/v1/auth/login", `{"username":"viewer","password":"node-test-password-123"}`, nil, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	viewerCookie := response.Result().Cookies()[0]
	var viewerSession auth.Session
	json.Unmarshal(response.Body.Bytes(), &viewerSession)
	if response := call("GET", "/api/v1/nodes", "", viewerCookie, ""); response.Code != 200 {
		t.Fatal("Viewer cannot read nodes")
	}
	if response := call("POST", "/api/v1/nodes/enrollment-token", `{"name":"unauthorized"}`, viewerCookie, viewerSession.CSRFToken); response.Code != 403 {
		t.Fatal("Viewer created a node")
	}
	repository := &node.Repository{Database: app.Database}
	if _, err := app.Database.Exec("UPDATE node_enrollment_tokens SET expires_at=?", time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.EnrollmentNode(context.Background(), node.Hash(enrolled.EnrollmentToken)); err != node.ErrToken {
		t.Fatalf("expired enrollment accepted: %v", err)
	}
	response = call("GET", "/api/v1/nodes/missing", "", cookie, "")
	if response.Code != 404 {
		t.Fatal("unknown node did not return 404")
	}
	// Cancelling a registration is Owner-only, and an enrolled node is kept: the
	// Agent's certificate is revoked instead of deleting the record.
	if response := call("DELETE", "/api/v1/nodes/"+enrolled.Node.ID, "", viewerCookie, viewerSession.CSRFToken); response.Code != 403 {
		t.Fatalf("Viewer cancelled a node enrollment: %d", response.Code)
	}
	if _, err := app.Database.Exec("INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,'pem','enrolled-fingerprint',?)", enrolled.Node.ID, time.Now().UTC().Add(time.Hour).Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		t.Fatal(err)
	}
	response = call("DELETE", "/api/v1/nodes/"+enrolled.Node.ID, "", cookie, session.CSRFToken)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "node_delete_not_allowed") {
		t.Fatalf("enrolled node was cancelled: %d %s", response.Code, response.Body.String())
	}
}

func TestAgentReconnectsAfterCentralDatabaseReopen(t *testing.T) {
	app := testServer(t)
	app.Config.ShutdownTimeout = time.Second
	launch := func(server *Server, address string) (string, func()) {
		httpListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		grpcListener, err := net.Listen("tcp", address)
		if err != nil {
			httpListener.Close()
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- server.Serve(ctx, httpListener, grpcListener) }()
		return grpcListener.Addr().String(), func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(3 * time.Second):
				t.Error("restart shutdown timed out")
			}
		}
	}
	address, stop := launch(app, "127.0.0.1:0")
	firstStopped := false
	defer func() {
		if !firstStopped {
			stop()
		}
	}()
	repository := &node.Repository{Database: app.Database}
	n, token, _, err := repository.CreateEnrollment(context.Background(), "restart-agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	caPath := filepath.Join(directory, "ca.crt")
	if err := os.WriteFile(caPath, app.Authority.PEM, 0600); err != nil {
		t.Fatal(err)
	}
	c := config.DefaultAgent()
	c.ControlAddress = address
	c.DataDirectory = filepath.Join(directory, "agent")
	c.TLS.ClientCA = caPath
	agentCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := control.Enroll(agentCtx, c, token, "restart-test"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- control.Run(agentCtx, c, app.Logger) }()
	waitOnline := func(repository *node.Repository, minimum uint64) {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			current, err := repository.Get(context.Background(), n.ID)
			if err == nil && current.Status == "online" && current.LastSequence >= minimum {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("Agent did not reach online sequence %d", minimum)
	}
	waitOnline(repository, 2)
	stop()
	firstStopped = true
	app.Database.Close()
	database, err := db.Open(context.Background(), app.Config.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	second, err := New(app.Config, database, fstest.MapFS{"index.html": {Data: []byte("UI")}}, app.Logger)
	if err != nil {
		t.Fatal(err)
	}
	_, stopSecond := launch(second, address)
	defer stopSecond()
	waitOnline(&node.Repository{Database: database}, 4)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Agent failed to stop")
	}
	state, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil || state.LastAcked < 4 {
		t.Fatalf("reconnected ACK not persisted: %v %v", state, err)
	}
}
