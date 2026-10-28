package server

import (
	"context"
	"crypto/tls"
	"google.golang.org/protobuf/types/known/timestamppb"
	"path/filepath"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/panel/node"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/devicegate"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
	"time"
)

func TestDeviceGateNegotiationOldAgentsAndReconnect(t *testing.T) {
	for _, mode := range []struct {
		name              string
		hello, capability bool
	}{{"old", false, false}, {"oldHelloNewCore", false, true}, {"probeUnsupported", true, false}, {"supported", true, true}} {
		t.Run(mode.name, func(t *testing.T) {
			f := newQuotaFixture(t)
			u, c := f.addUser(t, "stream_gate_user", "")
			revision := f.publish(t)
			if _, err := f.app.Database.Exec("UPDATE users SET device_limit=1 WHERE id=?", u.ID); err != nil {
				t.Fatal(err)
			}
			snapshot := traffic.ConnectionSnapshot{NodeID: f.nodeID, Revision: revision, CoreEpoch: "e", ObservedAt: time.Now(), Scope: traffic.ConnectionScope, ObservationContractVersion: 2, DeviceGateSupported: true, Connections: 2, Accounts: []traffic.ConnectionAccount{{CredentialID: c.ID, Connections: 2, AddressesMeasured: true, AddressKeys: []string{"198.51.100.17", "198.51.101.91"}}}}
			if err := f.app.connectionStore.Record(f.nodeID, snapshot); err != nil {
				t.Fatal(err)
			}
			f.app.deviceObserver.Record(f.ctx, f.nodeID)
			address, stop := startControl(t, f.app)
			defer stop()
			nodes := &node.Repository{Database: f.app.Database}
			_, token, _, err := nodes.ReissueEnrollment(f.ctx, f.nodeID, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			dir := t.TempDir()
			csr, err := identity.CSR(dir)
			if err != nil {
				t.Fatal(err)
			}
			response, err := pb.NewAgentControlClient(trustedClient(t, address, f.app.Authority.PEM, nil)).Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "gate-test"})
			if err != nil {
				t.Fatal(err)
			}
			if err := identity.SaveEnrollment(dir, response.NodeId, response.CertificatePem, response.CaPem); err != nil {
				t.Fatal(err)
			}
			pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"))
			if err != nil {
				t.Fatal(err)
			}
			client := pb.NewAgentControlClient(trustedClient(t, address, f.app.Authority.PEM, &pair))
			sessionCtx, sessionCancel := context.WithCancel(ctx)
			defer sessionCancel()
			stream, err := client.Connect(sessionCtx)
			if err != nil {
				t.Fatal(err)
			}
			hello := func(last uint64) {
				if err := stream.Send(&pb.AgentMessage{NodeId: f.nodeID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{LastAckedSequence: last, DeviceGateSupported: mode.hello}}}); err != nil {
					t.Fatal(err)
				}
				welcome, err := stream.Recv()
				if err != nil || welcome.GetWelcome() == nil || welcome.GetWelcome().DeviceGateSupported != mode.hello {
					t.Fatal("wrong negotiation", err)
				}
			}
			hello(0)
			caps := func(sequence uint64) {
				if err := stream.Send(&pb.AgentMessage{NodeId: f.nodeID, MessageId: "caps", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: &pb.Capabilities{CoreName: "xray", DeviceGateSupported: mode.capability, ObservationContractVersion: 2}}}); err != nil {
					t.Fatal(err)
				}
				ack, err := stream.Recv()
				if err != nil || ack.GetAck() == nil || ack.GetAck().AgentSequence != sequence {
					t.Fatal("missing capability ack", err)
				}
			}
			caps(1)
			gate := func() {
				command, err := stream.Recv()
				if err != nil || command.GetDeviceGateCommand() == nil {
					t.Fatal("missing gate refresh", err)
				}
				r, err := devicegate.Decode(command.GetDeviceGateCommand().RequestJson)
				if err != nil || len(r.Entries) != 1 || len(r.Entries[0].Keys) != 1 {
					t.Fatal("wrong gate payload")
				}
			}
			if mode.hello && mode.capability {
				gate()
			}
			if err := stream.Send(&pb.AgentMessage{NodeId: f.nodeID, MessageId: "heartbeat", Sequence: 2, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{CurrentRevision: revision, SuccessfulRevision: revision}}}); err != nil {
				t.Fatal(err)
			}
			ack, err := stream.Recv()
			if err != nil || ack.GetAck() == nil {
				t.Fatal("old Agent received a gate or lost its heartbeat", err)
			}
			if mode.hello && mode.capability {
				gate()
				sessionCancel()
				deadline := time.Now().Add(2 * time.Second)
				for {
					n, err := nodes.Get(ctx, f.nodeID)
					if err == nil && n.Status == "offline" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("session did not disconnect")
					}
					time.Sleep(10 * time.Millisecond)
				}
				stream, err = client.Connect(ctx)
				if err != nil {
					t.Fatal(err)
				}
				hello(2)
				caps(3)
				gate()
			}
		})
	}
}
