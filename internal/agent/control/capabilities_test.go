package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/capability"
	"proxycontrol/internal/agent/identity"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
	"proxycontrol/pkg/contracts/traffic"
)

// controlStream answers the Agent like a panel that accepts everything: the Hello
// is answered with a Welcome and every other message with its own Ack. Every
// message the Agent sends is recorded so a test can read what it reported.
// controlPacket is one answer of the fake panel: a message or the error that
// ended the stream.
type controlPacket struct {
	message *pb.ServerMessage
	err     error
}

type controlStream struct {
	grpc.ClientStream
	ctx      context.Context
	incoming chan controlPacket
	// rejectTraffic names the traffic batch the fake panel rejects: the batch is
	// received and the stream then ends with the panel's rejection code.
	rejectTraffic string
	mu            sync.Mutex
	sent          []*pb.AgentMessage
}

func newControlStream(ctx context.Context) *controlStream {
	return &controlStream{ctx: ctx, incoming: make(chan controlPacket, 16)}
}

func (c *controlStream) Send(message *pb.AgentMessage) error {
	c.mu.Lock()
	c.sent = append(c.sent, message)
	c.mu.Unlock()
	ack := &pb.Ack{AgentSequence: message.Sequence}
	if batch := message.GetTrafficBatch(); batch != nil {
		decoded, err := traffic.Decode(batch.BatchJson)
		if err != nil {
			return err
		}
		ack.TrafficBatchId = decoded.ID
		if decoded.ID == c.rejectTraffic && c.rejectTraffic != "" {
			c.incoming <- controlPacket{err: status.Error(codes.FailedPrecondition, "traffic batch rejected")}
			return nil
		}
	}
	if message.GetHello() != nil {
		c.incoming <- controlPacket{message: &pb.ServerMessage{NodeId: message.NodeId, MessageId: "welcome", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.ServerMessage_Welcome{Welcome: &pb.Welcome{LastAcceptedSequence: 0, HeartbeatIntervalSeconds: 1, CoreUpdateSupported: true}}}}
		return nil
	}
	c.incoming <- controlPacket{message: &pb.ServerMessage{NodeId: message.NodeId, MessageId: "ack", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.ServerMessage_Ack{Ack: ack}}}
	return nil
}

func (c *controlStream) Recv() (*pb.ServerMessage, error) {
	select {
	case packet := <-c.incoming:
		return packet.message, packet.err
	case <-c.ctx.Done():
		return nil, io.EOF
	}
}

func (c *controlStream) CloseSend() error             { return nil }
func (c *controlStream) Context() context.Context     { return c.ctx }
func (c *controlStream) Header() (metadata.MD, error) { return nil, nil }
func (c *controlStream) Trailer() metadata.MD         { return nil }
func (c *controlStream) SendMsg(any) error            { return errors.New("unused") }
func (c *controlStream) RecvMsg(any) error            { return errors.New("unused") }

func (c *controlStream) firstCapabilities() *pb.Capabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, message := range c.sent {
		if capabilities := message.GetCapabilities(); capabilities != nil {
			return capabilities
		}
	}
	return nil
}

// controlClient is the panel side of the control stream, enough for one session.
type controlClient struct{ stream *controlStream }

func (c controlClient) Enroll(context.Context, *pb.EnrollRequest, ...grpc.CallOption) (*pb.EnrollResponse, error) {
	return nil, errors.New("unused")
}
func (c controlClient) Connect(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[pb.AgentMessage, pb.ServerMessage], error) {
	return c.stream, nil
}
func (c controlClient) RenewCertificate(context.Context, *pb.CertificateRequest, ...grpc.CallOption) (*pb.CertificateRenewal, error) {
	return nil, errors.New("unused")
}
func (c controlClient) ConfirmCATrust(context.Context, *pb.CATrustConfirmation, ...grpc.CallOption) (*pb.CATrustAcknowledgement, error) {
	return nil, errors.New("unused")
}
func (c controlClient) PrepareNodeRootRetirement(context.Context, *pb.NodeRootRetirementRequest, ...grpc.CallOption) (*pb.NodeRootRetirementResponse, error) {
	return nil, errors.New("unused")
}
func (c controlClient) ConfirmNodeRootRetirement(context.Context, *pb.NodeRootRetirementConfirmation, ...grpc.CallOption) (*pb.NodeRootRetirementAcknowledgement, error) {
	return nil, errors.New("unused")
}

// A core update that commits or rolls back changes the running binary, so the
// capabilities kept for later sessions must describe that binary instead of the
// core detected at process start.
func TestCoreUpdateRefreshesCapabilities(t *testing.T) {
	for _, test := range []struct {
		name     string
		rollback bool
		want     string
	}{
		{name: "committed image", want: "26.3.28"},
		{name: "rolled back image", rollback: true, want: "26.3.27"},
	} {
		t.Run(test.name, func(t *testing.T) {
			adapter := &fakeCoreRuntime{version: "26.3.27", baselineVersion: "26.3.27"}
			worker, cfg, _, original := updateHarness(t, adapter)
			archive, digest, binaryDigest := updateArchive(t, original, "refreshed-core")
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
			defer server.Close()
			worker.source = updateSource(t, server)
			installed := filepath.Join(cfg.DataDirectory, "releases", "installed", installedImageName(binaryDigest))
			// The archive reports 26.3.28; the running image keeps reporting
			// 26.3.27 until the update switches to the downloaded one.
			adapter.version = "26.3.28"
			adapter.imageVersions = map[string]string{"refreshed-core": "26.3.28"}
			if test.rollback {
				adapter.badPath = installed
			}
			if before := worker.capabilities.Snapshot().GetCoreVersion(); before != "26.3.27" {
				t.Fatalf("initial capabilities report %q", before)
			}
			request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_refresh", NodeID: "node-update", Revision: 1, Sequence: 1, Component: "xray", Version: "26.3.28", URL: updateAssetURL, SHA256: digest}
			if err := worker.submit(updateRequest(request)); err != nil {
				t.Fatal(err)
			}
			result := awaitResult(t, worker)
			if test.rollback && result.Status != "rolled_back" || !test.rollback && result.Status != "success" {
				t.Fatalf("unexpected result: %+v", result)
			}
			if got := worker.capabilities.Snapshot().GetCoreVersion(); got != test.want {
				t.Fatalf("capabilities after %s report %q, want %q", test.name, got, test.want)
			}
		})
	}
}

// The next session must send the refreshed capabilities instead of the ones
// detected at process start.
func TestLaterSessionReportsRefreshedCapabilities(t *testing.T) {
	adapter := &fakeCoreRuntime{version: "26.3.27", baselineVersion: "26.3.27"}
	worker, _, manager, original := updateHarness(t, adapter)
	archive, digest, _ := updateArchive(t, original, "next-session")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	defer server.Close()
	worker.source = updateSource(t, server)
	adapter.version = "26.3.28"
	adapter.imageVersions = map[string]string{"next-session": "26.3.28"}
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: "upd_session", NodeID: "node-update", Revision: 1, Sequence: 1, Component: "xray", Version: "26.3.28", URL: updateAssetURL, SHA256: digest}
	if err := worker.submit(updateRequest(request)); err != nil {
		t.Fatal(err)
	}
	if result := awaitResult(t, worker); result.Status != "success" {
		t.Fatalf("update did not commit: %+v", result)
	}
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newControlStream(ctx)
	state := &identity.State{NodeID: "node-update", Directory: directory}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	done := make(chan error, 1)
	go func() {
		done <- session(ctx, controlClient{stream: stream}, state, logger, capability.NewSampler(directory), worker.capabilities, "test", manager, adapter, nil, worker, nil)
	}()
	deadline := time.After(10 * time.Second)
	for stream.firstCapabilities() == nil {
		select {
		case <-deadline:
			t.Fatal("session never reported its capabilities")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := stream.firstCapabilities().GetCoreVersion(); got != "26.3.28" {
		t.Fatalf("session reported a stale core version %q", got)
	}
	cancel()
	<-done
}
