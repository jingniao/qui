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
	t.Parallel()
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
			checkGate := func(command *pb.ServerMessage, wantKeys int) {
				if command.GetDeviceGateCommand() == nil {
					t.Fatal("missing gate refresh")
				}
				r, err := devicegate.Decode(command.GetDeviceGateCommand().RequestJson)
				if err != nil || r.TTLSeconds != 180 {
					t.Fatal("wrong gate payload")
				}
				if wantKeys == 0 {
					if len(r.Entries) != 0 {
						t.Fatal("gate removal still contains restrictions")
					}
				} else if len(r.Entries) != 1 || r.Entries[0].CredentialID != c.ID || len(r.Entries[0].Keys) != wantKeys {
					t.Fatal("wrong gate restrictions")
				}
			}
			gate := func(wantKeys int) {
				command, err := stream.Recv()
				if err != nil {
					t.Fatal("missing gate refresh", err)
				}
				checkGate(command, wantKeys)
			}
			if mode.hello && mode.capability {
				gate(1)
			}
			sendHeartbeat := func(sequence uint64) {
				if err := stream.Send(&pb.AgentMessage{NodeId: f.nodeID, MessageId: "heartbeat", Sequence: sequence, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Heartbeat{Heartbeat: &pb.Heartbeat{CurrentRevision: revision, SuccessfulRevision: revision}}}); err != nil {
					t.Fatal(err)
				}
			}
			heartbeat := func(sequence uint64) {
				sendHeartbeat(sequence)
				ack, err := stream.Recv()
				if err != nil || ack.GetAck() == nil || ack.GetAck().AgentSequence != sequence {
					t.Fatal("unexpected gate or missing heartbeat ack", sequence, err)
				}
			}
			heartbeat(2)
			// An identical gate does not accompany every report. The next ACK is
			// a stream-order barrier: a duplicate sent after heartbeat 2 would
			// arrive before this ACK and fail immediately, without a timed read.
			// Fake-clock delivery tests cover the one-minute lease renewal.
			heartbeat(3)
			if mode.hello && mode.capability {
				// A changed restriction still takes effect on the very next report.
				if _, err := f.app.Database.Exec("UPDATE users SET device_limit=2 WHERE id=?", u.ID); err != nil {
					t.Fatal(err)
				}
				sendHeartbeat(4)
				// The server may observe the database change after ACK 3 but
				// before its subsequent gate check. Accept either ordering of the
				// changed gate and ACK 4, while requiring exactly one of each.
				gotAck, gotGate := false, false
				for range 2 {
					message, err := stream.Recv()
					if err != nil {
						t.Fatal("missing changed gate or heartbeat ack", err)
					}
					if ack := message.GetAck(); ack != nil {
						if gotAck || ack.AgentSequence != 4 {
							t.Fatal("unexpected heartbeat ack", ack.AgentSequence)
						}
						gotAck = true
					} else {
						if gotGate {
							t.Fatal("duplicate changed gate")
						}
						checkGate(message, 0)
						gotGate = true
					}
				}
				if _, err := f.app.Database.Exec("UPDATE users SET device_limit=1 WHERE id=?", u.ID); err != nil {
					t.Fatal(err)
				}
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
				hello(4)
				caps(5)
				gate(1)
			}
		})
	}
}
