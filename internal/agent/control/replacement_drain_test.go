package control

import (
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

func TestReplacementDrainRequiresAgentServiceAtNewEnrollmentEndpoint(t *testing.T) {
	requested := make(chan struct{}, 1)
	web := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requested <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "web server")
	}))
	web.EnableHTTP2 = true
	web.StartTLS()
	defer web.Close()
	root, target := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "identity")
	authority, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	csrBytes, err := identity.CSR(dir)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := pki.ParseCSR(csrBytes)
	if err != nil {
		t.Fatal(err)
	}
	cert, _, err := authority.IssueNode(csr, "node")
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.SaveEnrollment(dir, "node", cert, authority.PEM); err != nil {
		t.Fatal(err)
	}
	state, err := identity.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(target, "replacement-ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: web.Certificate().Raw}), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(target, "replacement-endpoint"), []byte(web.Listener.Addr().String()), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	q, err := spool.Open(ctx, filepath.Join(root, "spool"), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	counter := traffic.Counter{CoreEpoch: "epoch", Revision: 1, CredentialID: "cred", UserID: "user", DeviceID: "device", Protocol: "socks5", UploadBytes: 16, ObservedAt: time.Now().UTC()}
	if err = q.SaveTraffic(ctx, []traffic.Counter{counter}); err != nil {
		t.Fatal(err)
	}
	old := config.Agent{ControlAddress: "127.0.0.1:1", ServerName: "old.invalid", DataDirectory: root}
	if err = uploadReplacement(ctx, old, target, "token", "1.7.3", state, q); err == nil {
		t.Fatal("ordinary HTTPS server authorized deletion")
	}
	select {
	case <-requested:
	default:
		t.Fatal("the new TLS enrollment endpoint was not contacted")
	}
	stats, err := q.Stats(ctx)
	if err != nil || stats.Batches != 1 || state.LastAcked != 0 {
		t.Fatal("non-Agent endpoint advanced the real queue", stats, state.LastAcked, err)
	}
}

func TestReplacementPreflightRejectsCustomEnrollmentTrust(t *testing.T) {
	c := config.DefaultAgent()
	c.DataDirectory = "/var/lib/proxy-agent"
	c.TLS.ClientCA = filepath.Join(t.TempDir(), "existing-ca.crt")
	if err := os.WriteFile(c.TLS.ClientCA, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckReplacementConfig(c); err == nil || !strings.Contains(err.Error(), "tls.client_ca") {
		t.Fatal(err)
	}
	got, err := os.ReadFile(c.TLS.ClientCA)
	if err != nil || string(got) != "retained" {
		t.Fatal(string(got), err)
	}
}

type drainPeer struct {
	*controlStream
	fault string
	last  *pb.AgentMessage
}

func (p *drainPeer) Header() (metadata.MD, error) {
	return metadata.Pairs(pb.ReplacementDrain, "ready"), nil
}
func (p *drainPeer) Trailer() metadata.MD {
	if p.fault == "final" {
		return nil
	}
	return metadata.Pairs(pb.ReplacementDrain, "confirmed")
}
func (p *drainPeer) Send(m *pb.AgentMessage) error { p.last = m; return p.controlStream.Send(m) }
func (p *drainPeer) Recv() (*pb.ServerMessage, error) {
	if p.last == nil {
		return nil, io.EOF
	}
	reply, err := p.controlStream.Recv()
	if err != nil {
		return nil, err
	}
	if hello := p.last.GetHello(); hello != nil {
		reply.GetWelcome().NodeTrafficSupported = true
		reply.GetWelcome().TrafficRetirementSupported = hello.TrafficRetirementSupported
	}
	if batch := p.last.GetTrafficBatch(); batch != nil {
		reply.GetAck().TrafficQueueId = batch.QueueId
		reply.GetAck().TrafficDeliverySeq = batch.DeliverySeq
		if p.fault == "wrong queue" {
			reply.GetAck().TrafficQueueId = "other"
		}
	}
	if p.last.GetTrafficConfirmation() != nil && p.fault == "confirmation" {
		reply.GetAck().AgentSequence++
	}
	return reply, nil
}
func (p *drainPeer) CloseSend() error { p.last = nil; return nil }

func TestReplacementDrainRequiresExactAccountingAndFinalPanelConfirmation(t *testing.T) {
	for _, scenario := range []string{"success", "wrong queue", "confirmation", "quarantined", "final"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			state := &identity.State{NodeID: "node", Directory: t.TempDir()}
			q, err := spool.OpenBound(ctx, filepath.Join(root, "spool"), state.NodeID, 0, filepath.Join(state.Directory, "spool-store.json.enc"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer q.Close()
			if err = q.PrepareTrafficDelivery(ctx); err != nil {
				t.Fatal(err)
			}
			if err = q.EnableTrafficDelivery(ctx); err != nil {
				t.Fatal(err)
			}
			err = q.SaveTraffic(ctx, []traffic.Counter{{CoreEpoch: "epoch", Revision: 1, CredentialID: "credential", UserID: "user", DeviceID: "device", Protocol: "socks5", UploadBytes: 237, ObservedAt: time.Now().UTC()}})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "quarantined" {
				batch, err := q.PendingTraffic(ctx)
				if err != nil || batch == nil {
					t.Fatal(batch, err)
				}
				for i := 0; i < spool.TrafficRejectLimit; i++ {
					if _, err = q.RejectTraffic(ctx, batch.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			peer := &drainPeer{controlStream: newControlStream(ctx), fault: scenario}
			err = drainStream(ctx, peer, state, q, "1.7.3")
			if (scenario == "success") != (err == nil) {
				t.Fatalf("result: %v", err)
			}
			stats, statsErr := q.Stats(ctx)
			if statsErr != nil {
				t.Fatal(statsErr)
			}
			if scenario == "quarantined" || scenario == "wrong queue" {
				if stats.Batches != 1 {
					t.Fatalf("pending batch lost: %+v", stats)
				}
			} else if stats.Batches != 0 {
				t.Fatalf("panel-accepted batch still pending: %+v", stats)
			}
			if scenario == "success" {
				if len(peer.sent) != 4 || peer.sent[1].GetTrafficBatch() == nil || peer.sent[2].GetTrafficConfirmation().ConfirmedSeq != 1 || peer.sent[3].GetTrafficConfirmation().ConfirmedSeq != 1 {
					t.Fatalf("unexpected uploads: %v", peer.sent)
				}
				if state.LastAcked != 3 {
					t.Fatalf("persistent sequence %d", state.LastAcked)
				}
			}
		})
	}
}

func TestReplacementDrainRequiresThisCollectorShutdown(t *testing.T) {
	ctx := context.Background()
	q, err := spool.Open(ctx, t.TempDir(), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	start := time.Now().UTC()
	checkpoint, err := q.BeginCollectorLoss(ctx, strings.Repeat("aa", 16), start)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.ConfirmCollectorClosed(ctx, start); err == nil {
		t.Fatal("accepted live collector")
	}
	if _, err = q.CheckpointCollectorLoss(ctx, checkpoint.ProcessID, checkpoint.Generation, spool.CollectorLossCounts{}, false, true); err != nil {
		t.Fatal(err)
	}
	if err = q.ConfirmCollectorClosed(ctx, start); err != nil {
		t.Fatal(err)
	}
	if err = q.ConfirmCollectorClosed(ctx, time.Now().Add(time.Second)); err == nil {
		t.Fatal("accepted previous shutdown")
	}
}
