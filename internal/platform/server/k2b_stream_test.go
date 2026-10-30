package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"google.golang.org/protobuf/types/known/timestamppb"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestK2bTuningFixedOperationOverAuthenticatedStream(t *testing.T) {
	app := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	n, token, _, err := app.nodeRepository.CreateEnrollment(ctx, "probe stream", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	address, stop := startControl(t, app)
	defer stop()
	dir := t.TempDir()
	csr, err := identity.CSR(dir)
	if err != nil {
		t.Fatal(err)
	}
	response, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, nil)).Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "probe-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = identity.SaveEnrollment(dir, response.NodeId, response.CertificatePem, response.CaPem); err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, &pair)).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{}}}); err != nil {
		t.Fatal(err)
	}
	if w, e := stream.Recv(); e != nil || w.GetWelcome() == nil {
		t.Fatal(w, e)
	}

	owner := budgetOwner(t, app)
	seq := uint64(0)
	send := func(payload any) {
		t.Helper()
		seq++
		m := &pb.AgentMessage{NodeId: n.ID, MessageId: fmt.Sprint(seq), Sequence: seq, Timestamp: timestamppb.Now()}
		switch p := payload.(type) {
		case *pb.Capabilities:
			m.Payload = &pb.AgentMessage_Capabilities{Capabilities: p}
		case *pb.Heartbeat:
			m.Payload = &pb.AgentMessage_Heartbeat{Heartbeat: p}
		case *pb.TuningResult:
			m.Payload = &pb.AgentMessage_TuningResult{TuningResult: p}
		}
		if err := stream.Send(m); err != nil {
			t.Fatal(err)
		}
		if a, e := stream.Recv(); e != nil || a.GetAck() == nil || a.GetAck().AgentSequence != seq {
			t.Fatal(a, e)
		}
	}
	send(&pb.Capabilities{TuningSupported: true, Host: &pb.HostInfo{CpuModel: "fixture"}})
	send(&pb.Heartbeat{})
	path := "/api/v1/nodes/" + n.ID + "/tuning"
	body := map[string]any{"settings": map[string]string{"net.core.somaxconn": "128"}, "clear": false, "confirmNodeId": n.ID}
	if w := owner.call("PUT", path, body); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := owner.call("PUT", path, body); w.Code != 409 {
		t.Fatal("busy", w.Code, w.Body.String())
	}
	send(&pb.Heartbeat{})
	command, e := stream.Recv()
	if e != nil || command.GetTuningCommand() == nil {
		t.Fatal(command, e)
	}
	c := command.GetTuningCommand()
	// The panel must continue accepting heartbeats while the helper is working.
	send(&pb.Heartbeat{})
	send(&pb.TuningResult{RequestId: c.RequestId, Status: "applied", Keys: []*pb.TuningKeyResult{{Key: "net.core.somaxconn", Value: "128", Applied: true}}})
	var status string
	if err = app.Database.QueryRow("SELECT status FROM node_tuning WHERE node_id=?", n.ID).Scan(&status); err != nil || status != "applied" {
		t.Fatal(status, err)
	}
	if w := owner.call("PUT", path, body); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	send(&pb.Heartbeat{})
	command, e = stream.Recv()
	if e != nil || command.GetTuningCommand() == nil || command.GetTuningCommand().RequestId == c.RequestId {
		t.Fatal(command, e)
	}
	stream.CloseSend()
	for deadline := time.Now().Add(3 * time.Second); ; {
		if err = app.Database.QueryRow("SELECT status FROM node_tuning WHERE node_id=?", n.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "unconfirmed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pending after stream drop", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Reconnect must not resend the unconfirmed operation, even after another
	// heartbeat. Cancel the receive after observing a quiet stream.
	reconnectCtx, reconnectCancel := context.WithCancel(ctx)
	defer reconnectCancel()
	reconnected, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, &pair)).Connect(reconnectCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err = reconnected.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "reconnect", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{LastAckedSequence: seq}}}); err != nil {
		t.Fatal(err)
	}
	if welcome, e := reconnected.Recv(); e != nil || welcome.GetWelcome() == nil {
		t.Fatal(welcome, e)
	}
	seq++
	if err = reconnected.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "reconnected-heartbeat", Sequence: seq, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}); err != nil {
		t.Fatal(err)
	}
	if ack, e := reconnected.Recv(); e != nil || ack.GetAck() == nil {
		t.Fatal(ack, e)
	}
	received := make(chan *pb.ServerMessage, 1)
	go func() { m, _ := reconnected.Recv(); received <- m }()
	select {
	case m := <-received:
		t.Fatal("unexpected command after reconnect", m)
	case <-time.After(150 * time.Millisecond):
	}
	reconnectCancel()
}

func TestK2bAutomaticRenewalAtThirtyDaysWithSixtyRemaining(t *testing.T) {
	app := testServer(t)
	n, c, csr := nearExpiryNode(t, app)
	directory := filepath.Join(c.DataDirectory, "identity")
	pair, err := tls.LoadX509KeyPair(filepath.Join(directory, "client.crt"), filepath.Join(directory, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	leaf.NotBefore = time.Now().Add(-30 * 24 * time.Hour)
	leaf.NotAfter = time.Now().Add(60 * 24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, leaf, app.Authority.Certificate, leaf.PublicKey, app.Authority.Key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err = os.WriteFile(filepath.Join(directory, "client.crt"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Database.Exec("UPDATE node_identities SET certificate_pem=?,fingerprint=?,expires_at=? WHERE node_id=?", string(encoded), pki.Fingerprint(leaf), leaf.NotAfter.UTC().Format(time.RFC3339Nano), n.ID); err != nil {
		t.Fatal(err)
	}
	observation, err := app.nodeRepository.CertificateStatus(context.Background(), n.ID, app.Authority)
	if err != nil || observation.Active == nil || observation.Active.Status != "renewal_due" {
		t.Fatal(observation, err)
	}
	address, stop := startControl(t, app)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := pb.NewAgentControlClient(renewalClient(t, c, address)).RenewCertificate(ctx, &pb.CertificateRequest{CsrPem: csr})
	if err != nil || !response.GetExpiresAt().AsTime().After(leaf.NotAfter) {
		t.Fatal(response, err)
	}
}
