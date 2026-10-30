package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"proxycontrol/internal/agent/identity"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

func TestK3bNodeCountersRequireIndependentNegotiationAndAckExactly(t *testing.T) {
	for _, negotiated := range []bool{false, true} {
		t.Run(fmt.Sprint(negotiated), func(t *testing.T) {
			app := testServer(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			n, token, _, err := app.nodeRepository.CreateEnrollment(ctx, "multi stream", time.Hour)
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
			enrolled, err := pb.NewAgentControlClient(trustedClient(t, address, app.Authority.PEM, nil)).Enroll(ctx, &pb.EnrollRequest{EnrollmentToken: token, CsrPem: csr, Hostname: "host", OperatingSystem: "linux/amd64", AgentVersion: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if err := identity.SaveEnrollment(dir, enrolled.NodeId, enrolled.CertificatePem, enrolled.CaPem); err != nil {
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
			if err := stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: "hello", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{MultiCoreSupported: true, NodeTrafficSupported: negotiated}}}); err != nil {
				t.Fatal(err)
			}
			welcome, err := stream.Recv()
			if err != nil || welcome.GetWelcome() == nil || !welcome.GetWelcome().MultiCoreSupported || welcome.GetWelcome().NodeTrafficSupported != negotiated {
				t.Fatal(welcome, err)
			}
			state := desiredstate.State{SchemaVersion: 1, NodeID: n.ID, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
			rev, err := app.deployments.Create(ctx, state, "owner", "baseline")
			if err != nil {
				t.Fatal(err)
			}
			if err := app.deployments.Deploy(ctx, rev.ID); err != nil {
				t.Fatal(err)
			}
			if err := app.deployments.Result(ctx, n.ID, &pb.ApplyResult{Revision: rev.ID, Success: true}); err != nil {
				t.Fatal(err)
			}
			batch := traffic.Batch{ID: "node-bytes", NodeID: n.ID, Counters: []traffic.Counter{{Kind: "node", Core: "xray", CoreEpoch: "native.node", Revision: rev.ID, CredentialID: "node", UserID: "node", DeviceID: "node", UploadBytes: 123, DownloadBytes: 456, ObservedAt: time.Now().Add(-time.Second)}}}
			raw, _ := json.Marshal(batch)
			for i := 1; i <= 2; i++ {
				if err := stream.Send(&pb.AgentMessage{NodeId: n.ID, MessageId: fmt.Sprint(i), Sequence: uint64(i), Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_TrafficBatch{TrafficBatch: &pb.TrafficBatch{BatchJson: raw}}}); err != nil {
					t.Fatal(err)
				}
				ack, err := stream.Recv()
				if !negotiated {
					if status.Code(err) != codes.FailedPrecondition {
						t.Fatal(ack, err)
					}
					return
				}
				if err != nil || ack.GetAck() == nil || ack.GetAck().TrafficBatchId != batch.ID {
					t.Fatal(ack, err)
				}
			}
			var up, down, count int64
			if err := app.Database.QueryRow("SELECT sum(upload_bytes),sum(download_bytes),count(*) FROM core_traffic_samples").Scan(&up, &down, &count); err != nil || up != 123 || down != 456 || count != 1 {
				t.Fatal(up, down, count, err)
			}
			if err := app.Database.QueryRow("SELECT count(*) FROM user_traffic_cursors").Scan(&count); err != nil || count != 0 {
				t.Fatal("node bytes reached user cursors", count, err)
			}
			stream.CloseSend()
		})
	}
}
