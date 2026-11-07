package node

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// enrollmentPeer is the peer gRPC hands Enroll. Enrollment only needs the
// request to arrive over TLS: the certificate comes from the CSR.
func enrollmentPeer() context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{}})
}

// agentPeer is the peer a connected Agent presents, including the certificate
// the panel issued for it.
func agentPeer(t *testing.T, leaf *x509.Certificate) context.Context {
	t.Helper()
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, VerifiedChains: [][]*x509.Certificate{{leaf}}}}})
}

// enrolledAgentNode is a node whose stored identity is a real certificate of the
// test panel, so the connection path authenticates it.
func enrolledAgentNode(t *testing.T, p *recoveryPanel) (Node, *x509.Certificate) {
	t.Helper()
	n, _, _, err := p.CreateEnrollment(p.ctx, "Connected", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, csr := signedRequest(t)
	certificate, leaf, err := p.authority.IssueNode(csr, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Database.ExecContext(p.ctx, "INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES(?,?,?,?)", n.ID, string(certificate), pki.Fingerprint(leaf), timestamp(leaf.NotAfter)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Database.ExecContext(p.ctx, "UPDATE nodes SET status='offline' WHERE id=?", n.ID); err != nil {
		t.Fatal(err)
	}
	return n, leaf
}

// The real enrollment path consumes a recovery code, drops the old Agent
// connection, replays the same certificate for a retried request and refuses a
// different key.
func TestEnrollRecoveryReplacesIdentityAndReplays(t *testing.T) {
	p := newRecoveryPanel(t)
	n := recoveryTestNode(t, p.Repository, p.ctx)
	previous := strings.Repeat("a", 64)
	live, cancel := context.WithCancel(context.Background())
	control := &Control{Repository: p.Repository, Authority: p.authority, connected: map[string]*liveStream{n.ID: {cancel: cancel, fingerprint: previous}}}
	_, token, _, err := p.IssueRecovery(p.ctx, n.ID, "owner", 15*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := p.request(t, token, "reinstalled")
	response, err := control.Enroll(enrollmentPeer(), request)
	if err != nil {
		t.Fatalf("recovery enrollment failed: %v", err)
	}
	if response.NodeId != n.ID {
		t.Fatalf("recovery enrolled as %q, want %q", response.NodeId, n.ID)
	}
	if live.Err() == nil {
		t.Fatal("the previous Agent connection stayed open")
	}
	fingerprint := recoveryIdentity(t, p.Repository, p.ctx, n.ID)
	if fingerprint == previous {
		t.Fatal("recovery kept the previous certificate")
	}
	if err := p.ValidateIdentity(p.ctx, n.ID, previous); !errors.Is(err, ErrIdentity) {
		t.Fatalf("the replaced certificate still authenticates: %v", err)
	}
	replay, err := control.Enroll(enrollmentPeer(), request)
	if err != nil || replay.NodeId != n.ID || !bytes.Equal(replay.CertificatePem, response.CertificatePem) {
		t.Fatalf("identical retry was not replayed: %v", err)
	}
	other, _ := p.request(t, token, "another host")
	if _, err := control.Enroll(enrollmentPeer(), other); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("a different key took the recovered identity: %v", err)
	}
	if identity := recoveryIdentity(t, p.Repository, p.ctx, n.ID); identity != fingerprint {
		t.Fatalf("the refused key changed the identity to %q", identity)
	}
}

// fakeAgentStream is the panel side of one Agent connection: it delivers the
// Hello and then waits until the test releases it.
type fakeAgentStream struct {
	ctx      context.Context
	release  chan struct{}
	welcome  chan struct{}
	mu       sync.Mutex
	hello    *pb.AgentMessage
	welcomed bool
	sent     []*pb.ServerMessage
}

func newFakeAgentStream(t *testing.T, leaf *x509.Certificate, nodeID, messageID string) *fakeAgentStream {
	t.Helper()
	stream := &fakeAgentStream{ctx: agentPeer(t, leaf), release: make(chan struct{}), welcome: make(chan struct{}), hello: &pb.AgentMessage{NodeId: nodeID, MessageId: messageID, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{AgentVersion: "1.4.0"}}}}
	t.Cleanup(func() { close(stream.release) })
	return stream
}

func (s *fakeAgentStream) Context() context.Context     { return s.ctx }
func (s *fakeAgentStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeAgentStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeAgentStream) SetTrailer(metadata.MD)       {}
func (s *fakeAgentStream) SendMsg(any) error            { return nil }
func (s *fakeAgentStream) RecvMsg(any) error            { return nil }

func (s *fakeAgentStream) Send(message *pb.ServerMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, message)
	if message.GetWelcome() != nil && !s.welcomed {
		s.welcomed = true
		close(s.welcome)
	}
	return nil
}

func (s *fakeAgentStream) Recv() (*pb.AgentMessage, error) {
	s.mu.Lock()
	hello := s.hello
	s.hello = nil
	s.mu.Unlock()
	if hello != nil {
		return hello, nil
	}
	<-s.release
	return nil, io.EOF
}

func waitForWelcome(t *testing.T, stream *fakeAgentStream) {
	t.Helper()
	select {
	case <-stream.welcome:
	case <-time.After(5 * time.Second):
		t.Fatal("the panel never welcomed the Agent")
	}
}

// Disconnect ends the live stream of a node, so the reinstalled Agent is not
// refused as a duplicate connection, and the stale entry is not left behind.
func TestDisconnectEndsLiveStreamAndAllowsReconnect(t *testing.T) {
	p := newRecoveryPanel(t)
	n, leaf := enrolledAgentNode(t, p)
	control := &Control{Repository: p.Repository, Authority: p.authority}
	first := newFakeAgentStream(t, leaf, n.ID, "hello-1")
	ended := make(chan error, 1)
	go func() { ended <- control.Connect(first) }()
	waitForWelcome(t, first)
	duplicate := newFakeAgentStream(t, leaf, n.ID, "hello-2")
	refused := make(chan error, 1)
	go func() { refused <- control.Connect(duplicate) }()
	if err := <-refused; status.Code(err) != codes.AlreadyExists {
		t.Fatalf("second stream for one node: %v", err)
	}
	control.Disconnect(n.ID)
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("disconnected stream ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect did not end the live stream")
	}
	reconnected := newFakeAgentStream(t, leaf, n.ID, "hello-3")
	resumed := make(chan error, 1)
	go func() { resumed <- control.Connect(reconnected) }()
	waitForWelcome(t, reconnected)
	control.Disconnect(n.ID)
	select {
	case err := <-resumed:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reconnected stream ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnect did not end the reconnected stream")
	}
}
