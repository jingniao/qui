package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/platform/config"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/agentstorage"
)

// CheckReplacementConfig also runs before the installer stops a live Agent.
func CheckReplacementConfig(c config.Agent) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.DataDirectory != "/var/lib/proxy-agent" {
		return errors.New("replacement requires the fixed Agent data directory")
	}
	if c.TLS.ClientCA != "/etc/proxy-agent/tls/ca.crt" {
		return errors.New("replacement enrollment requires tls.client_ca to be /etc/proxy-agent/tls/ca.crt")
	}
	root := c.DataDirectory
	// The new enrollment replaces only its own identity files. A separately
	// configured CA or executable must survive with the retained config.
	for _, reference := range []string{c.TLS.ClientCA, c.Updates.CA, c.CorePath} {
		if reference == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(reference)
		if err != nil {
			return err
		}
		if resolved == root || strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			return fmt.Errorf("retained configuration references Agent data scheduled for replacement: %s", reference)
		}
	}
	if c.TLS.Certificate != filepath.Join(root, "identity", "client.crt") || c.TLS.Key != filepath.Join(root, "identity", "client.key") {
		return errors.New("replacement requires the installed Agent identity paths")
	}
	return nil
}

// DrainTelemetry uses the stopped Agent's identity and accounting sequence. It
// does not construct a runtime, collector or command executor.
func DrainTelemetry(ctx context.Context, c config.Agent, token, version, tokenFile string, detach func(context.Context) error) (resultErr error) {
	if err := CheckReplacementConfig(c); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	root := c.DataDirectory
	for _, path := range []string{root, filepath.Join(root, "identity"), filepath.Join(root, "spool")} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if resolved != path {
			return fmt.Errorf("replacement refuses symlink: %s", path)
		}
	}
	supervisor, err := lifecycle.TrySupervisor(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, supervisor.Close()) }()
	owner, err := lifecycle.Try(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	for _, name := range []string{"", "xray", "sing-box", "gost"} {
		marker, _, err := core.LoadCoreRun(filepath.Join(root, "runtime", name))
		if err != nil {
			return err
		}
		if marker != nil {
			return fmt.Errorf("unconfirmed core exit in runtime/%s", name)
		}
	}
	state, err := identity.Load(filepath.Join(root, "identity"))
	if err != nil {
		return err
	}
	floor, found, err := storage.Check(root, state.NodeID, agentstorage.Current(version))
	if err != nil {
		return err
	}
	if !found {
		return errors.New("original Agent storage contract missing")
	}
	witness := filepath.Join(state.Directory, "spool-store.json.enc")
	for _, path := range []string{witness, filepath.Join(root, "spool", "queue.db"), filepath.Join(root, "spool", "spool.key")} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("original telemetry file required: %s", path)
		}
	}
	q, err := spool.OpenBound(ctx, filepath.Join(root, "spool"), state.NodeID, 0, witness, nil)
	if err != nil {
		return err
	}
	defer func() {
		if q != nil {
			resultErr = errors.Join(resultErr, q.Close())
		}
	}()
	tokenInfo, err := os.Stat(tokenFile)
	if err != nil {
		return err
	}
	if err = q.ConfirmCollectorClosed(ctx, tokenInfo.ModTime()); err != nil {
		return err
	}
	if floor.Formats.Spool.Write >= agentstorage.TrafficDeliveryFormat {
		if err = q.PrepareTrafficDelivery(ctx); err != nil {
			return err
		}
	}
	if err := uploadReplacement(ctx, c, filepath.Dir(tokenFile), token, version, state, q); err != nil {
		return err
	}
	err = q.Close()
	q = nil
	if err != nil {
		return err
	}
	// The installer detaches the fixed root while both lifetime locks are
	// still held. A restarted Agent cannot append between the ACK and rename.
	return detach(ctx)
}

// Upload to the exact address, server name and trust anchor that enrollment
// will use. A different panel cannot confirm the bound old identity and token.
func uploadReplacement(ctx context.Context, c config.Agent, targetDir, token, version string, state *identity.State, q *spool.Spool) error {
	endpoint, err := os.ReadFile(filepath.Join(targetDir, "replacement-endpoint"))
	if err != nil {
		return err
	}
	c.ControlAddress = strings.TrimSpace(string(endpoint))
	c.ServerName, _, err = net.SplitHostPort(c.ControlAddress)
	if err != nil {
		return err
	}
	c.TLS.ClientCA = filepath.Join(targetDir, "replacement-ca.crt")
	tlsConfig, err := transport(c, true, state.NodeID)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(c.ControlAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return err
	}
	defer conn.Close()
	digest := sha256.Sum256([]byte(token))
	ctx = metadata.AppendToOutgoingContext(ctx, pb.ReplacementDrain, hex.EncodeToString(digest[:]))
	stream, err := pb.NewAgentControlClient(conn).Connect(ctx)
	if err != nil {
		return err
	}
	return drainStream(ctx, stream, state, q, version)
}

func drainStream(ctx context.Context, stream grpc.BidiStreamingClient[pb.AgentMessage, pb.ServerMessage], state *identity.State, q *spool.Spool, version string) error {
	delivery := spool.TrafficDeliveryState{}
	retirement := q.TrafficDeliverySupported()
	var err error
	if retirement {
		delivery, err = q.TrafficDeliveryState(ctx)
		if err != nil {
			return err
		}
	}
	sequence := uint64(0)
	send := func(message *pb.AgentMessage) error {
		message.MessageId, err = newID()
		if err != nil {
			return err
		}
		message.NodeId = state.NodeID
		message.Timestamp = timestamppb.Now()
		message.Sequence = sequence
		return stream.Send(message)
	}
	hello := &pb.Hello{AgentVersion: version, LastAckedSequence: state.LastAcked, NodeTrafficSupported: true, TrafficRetirementSupported: retirement, TrafficQueueId: delivery.QueueID, TrafficConfirmedSeq: uint64(delivery.Confirmed), TrafficIssuedSeq: uint64(delivery.Issued)}
	if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_Hello{Hello: hello}}); err != nil {
		return err
	}
	header, err := stream.Header()
	if err != nil {
		return err
	}
	if values := header.Get(pb.ReplacementDrain); len(values) != 1 || values[0] != "ready" {
		return errors.New("panel did not acknowledge the bound replacement")
	}
	reply, err := stream.Recv()
	if err != nil {
		return err
	}
	w := reply.GetWelcome()
	if reply.GetNodeId() != state.NodeID || w == nil || !w.NodeTrafficSupported || w.TrafficRetirementSupported != retirement || w.LastAcceptedSequence < state.LastAcked || w.LastAcceptedSequence >= math.MaxInt64 {
		return errors.New("invalid drain welcome or telemetry protocol")
	}
	if retirement {
		if err = q.EnableTrafficDelivery(ctx); err != nil {
			return err
		}
	}
	sequence = w.LastAcceptedSequence
	if err = state.Save(sequence); err != nil {
		return err
	}
	exchange := func(message *pb.AgentMessage, accessID, trafficID string) error {
		if sequence >= math.MaxInt64 {
			return errors.New("persistent message sequence exhausted")
		}
		sequence++
		if err := send(message); err != nil {
			return err
		}
		response, err := stream.Recv()
		if err != nil {
			return err
		}
		ack := response.GetAck()
		traffic := message.GetTrafficBatch()
		if response.GetNodeId() != state.NodeID || ack == nil || ack.AgentSequence != sequence || ack.AccessBatchId != accessID || ack.TrafficBatchId != trafficID || ack.TrafficQueueId != traffic.GetQueueId() || ack.TrafficDeliverySeq != traffic.GetDeliverySeq() {
			return errors.New("panel did not confirm the exact final telemetry message")
		}
		return state.Save(sequence)
	}
	confirm := func() error {
		d, err := q.TrafficDeliveryState(ctx)
		if err != nil {
			return err
		}
		return exchange(&pb.AgentMessage{Payload: &pb.AgentMessage_TrafficConfirmation{TrafficConfirmation: &pb.TrafficConfirmation{QueueId: d.QueueID, ConfirmedSeq: uint64(d.Confirmed)}}}, "", "")
	}
	for {
		batch, _, err := q.PendingTrafficForPeer(ctx, true)
		if err != nil {
			return err
		}
		if batch == nil {
			break
		}
		data, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		payload := &pb.TrafficBatch{BatchJson: data}
		if retirement {
			d, err := q.TrafficEnvelope(ctx, batch.ID)
			if err != nil {
				return err
			}
			payload.QueueId, payload.DeliverySeq = d.QueueID, uint64(d.Sequence)
		}
		if err = exchange(&pb.AgentMessage{Payload: &pb.AgentMessage_TrafficBatch{TrafficBatch: payload}}, "", batch.ID); err != nil {
			return err
		}
		if err = q.AckTraffic(ctx, batch.ID, data); err != nil {
			return err
		}
		if retirement {
			if err = confirm(); err != nil {
				return err
			}
		}
	}
	for {
		batch, _, err := q.PendingRetry(ctx)
		if err != nil {
			return err
		}
		if batch == nil {
			break
		}
		data, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		if err = exchange(&pb.AgentMessage{Payload: &pb.AgentMessage_AccessBatch{AccessBatch: &pb.AccessBatch{BatchJson: data}}}, batch.ID, ""); err != nil {
			return err
		}
		if err = q.Ack(ctx, batch.ID, data); err != nil {
			return err
		}
	}
	stats, err := q.Stats(ctx)
	if err != nil {
		return err
	}
	if stats.Batches != 0 {
		return fmt.Errorf("%d telemetry batches remain, including quarantined data", stats.Batches)
	}
	if retirement {
		if err = confirm(); err != nil {
			return err
		}
	}
	if err = stream.CloseSend(); err != nil {
		return err
	}
	if _, err = stream.Recv(); !errors.Is(err, io.EOF) {
		return errors.Join(errors.New("replacement stream did not finish"), err)
	}
	if values := stream.Trailer().Get(pb.ReplacementDrain); len(values) != 1 || values[0] != "confirmed" {
		return errors.New("panel did not confirm final drain")
	}
	return nil
}
