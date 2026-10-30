package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
	"time"
)

func TestTCPProbeFixedOperationOverAuthenticatedStream(t *testing.T) {
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
	if err = stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{TcpProbeSupported: true}}}); err != nil {
		t.Fatal(err)
	}
	if w, e := stream.Recv(); e != nil || w.GetWelcome() == nil {
		t.Fatal(w, e)
	}
	c := &pb.TCPProbeCommand{Id: "probe-stream", Addresses: []string{"1.1.1.1"}, Port: 443, Attempts: 3, TimeoutMs: 100, Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	raw, _ := json.Marshal(c)
	if _, err = app.Database.Exec("INSERT INTO probe_targets(id,label,host,port,allow_non_public) VALUES('stream-target','label','1.1.1.1',443,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Database.Exec("INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,result_json,created_at,deadline,status) VALUES(?,?,?, ?,NULL,?,?,'queued')", c.Id, n.ID, "stream-target", string(raw), time.Now().UTC().Format(time.RFC3339Nano), c.Deadline.AsTime().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "heartbeat", Sequence: 1, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{}}}); err != nil {
		t.Fatal(err)
	}
	if a, e := stream.Recv(); e != nil || a.GetAck() == nil {
		t.Fatal(a, e)
	}
	command, e := stream.Recv()
	if e != nil || command.GetTcpProbeCommand() == nil || command.GetTcpProbeCommand().Addresses[0] != "1.1.1.1" {
		t.Fatal(command, e)
	}
	if err = stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "result", Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_TcpProbeResult{TcpProbeResult: &pb.TCPProbeResult{Id: c.Id, Attempted: 3, Lost: 3, Code: "complete"}}}); err != nil {
		t.Fatal(err)
	}
	if a, e := stream.Recv(); e != nil || a.GetAck() == nil || a.GetAck().AgentSequence != 2 {
		t.Fatal(a, e)
	}
	var status string
	if err = app.Database.QueryRow("SELECT status FROM node_tcp_probes WHERE id=?", c.Id).Scan(&status); err != nil || status != "complete" {
		t.Fatal(status, err)
	}
}
