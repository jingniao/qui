package node

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/panel/connections"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
	"proxycontrol/pkg/contracts/traffic"
)

// HeartbeatInterval is how often the Agent is asked to report. Live connection
// snapshots age with it: the panel keeps a snapshot only for a few intervals,
// so a node that stops reporting leaves no stale count behind.
const HeartbeatInterval = 15 * time.Second

type Control struct {
	pb.UnimplementedAgentControlServer
	Repository  *Repository
	CoreUpdates interface {
		PendingCoreUpdate(context.Context, string) (*pb.CoreUpdateCommand, error)
		CoreUpdateResult(context.Context, string, *pb.CoreUpdateResult) error
		CloseStaleCoreUpdates(context.Context, string) error
	}
	AgentUpdates interface {
		PendingAgentUpdate(context.Context, string) (*pb.AgentUpdateCommand, error)
		AgentUpdateResult(context.Context, string, *pb.AgentUpdateResult) error
		AgentUpdateReserved(context.Context, string) (bool, error)
	}
	Authority       *pki.Authority
	mu              sync.Mutex
	connected       map[string]bool
	Notify          func(string)
	DashboardNotify func()
	// Connections holds the live per-node connection snapshots. Optional; when
	// nil connection status messages are accepted and ignored.
	Connections *connections.Store
	// ConnectionsRecorded is called after a fresh snapshot is stored, so the
	// per-user observer can re-evaluate its limit. Optional and nil-safe.
	ConnectionsRecorded func(context.Context, string)
	DeploymentNotify    func(int64)
	Deployment          interface {
		Pending(context.Context, string) (*pb.ApplyCommand, error)
		Result(context.Context, string, *pb.ApplyResult) error
		PendingRollback(context.Context, string) (*pb.RollbackCommand, error)
	}
	HealthRecorder interface {
		RecordHealth(context.Context, string, *pb.OutboundHealthBatch) error
	}
	AccessRecorder interface {
		RecordAccess(context.Context, string, *pb.AccessBatch, bool) (string, error)
		RecordTraffic(context.Context, string, *pb.TrafficBatch, bool) (string, error)
	}
}

func certificate(ctx context.Context) (credentials.TLSInfo, error) {
	client, ok := peer.FromContext(ctx)
	if !ok {
		return credentials.TLSInfo{}, ErrIdentity
	}
	info, ok := client.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.PeerCertificates) == 0 || len(info.State.VerifiedChains) == 0 {
		return info, ErrIdentity
	}
	return info, nil
}
func RequireCertificate(ctx context.Context) error {
	_, err := certificate(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, "client certificate required")
	}
	return nil
}
func UnaryAuthentication(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if info.FullMethod != pb.AgentControl_Enroll_FullMethodName {
		if err := RequireCertificate(ctx); err != nil {
			return nil, err
		}
	}
	return handler(ctx, request)
}
func StreamAuthentication(service any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := RequireCertificate(stream.Context()); err != nil {
		return err
	}
	return handler(service, stream)
}
func EnrollmentTLS(base *tls.Config, ca []byte) *tls.Config {
	result := base.Clone()
	// Node authentication has its own CA lifecycle. Do not retain roots from
	// the server TLS configuration after the panel identity module retires them.
	result.ClientCAs = x509.NewCertPool()
	result.ClientCAs.AppendCertsFromPEM(ca)
	result.ClientAuth = tls.VerifyClientCertIfGiven
	return result
}

func (c *Control) Enroll(ctx context.Context, request *pb.EnrollRequest) (*pb.EnrollResponse, error) {
	if request == nil || len(request.EnrollmentToken) == 0 || len(request.EnrollmentToken) > 128 || len(request.Hostname) > 128 || len(request.OperatingSystem) > 256 || len(request.AgentVersion) > 64 {
		return nil, status.Error(codes.InvalidArgument, "invalid enrollment metadata")
	}
	client, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "TLS required")
	}
	if _, ok := client.AuthInfo.(credentials.TLSInfo); !ok {
		return nil, status.Error(codes.Unauthenticated, "TLS required")
	}
	csr, err := pki.ParseCSR(request.CsrPem)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid signed ECDSA P-256 CSR")
	}
	if original, found, err := c.Repository.ReplayEnrollment(ctx, request, csr, c.Authority); found || err != nil {
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "original enrollment cannot be resumed")
		}
		return original, nil
	}
	id, err := c.Repository.EnrollmentNode(ctx, Hash(request.EnrollmentToken))
	if errors.Is(err, ErrToken) {
		return nil, status.Error(codes.Unauthenticated, ErrToken.Error())
	}
	if err != nil {
		return nil, status.Error(codes.Internal, "enrollment unavailable")
	}
	encoded, cert, err := c.Authority.IssueNode(csr, id)
	if err != nil {
		return nil, status.Error(codes.Internal, "certificate issuance failed")
	}
	if err := c.Repository.ConsumeEnrollmentOriginal(ctx, Hash(request.EnrollmentToken), id, request, csr, encoded, c.Authority.PEM, cert); err != nil {
		if errors.Is(err, ErrToken) {
			// A concurrent identical request may have committed the original.
			if original, found, replayErr := c.Repository.ReplayEnrollment(ctx, request, csr, c.Authority); found && replayErr == nil {
				return original, nil
			}
			return nil, status.Error(codes.Unauthenticated, ErrToken.Error())
		}
		return nil, status.Error(codes.Internal, "enrollment failed")
	}
	return &pb.EnrollResponse{NodeId: id, CertificatePem: encoded, CaPem: c.Authority.PEM, ExpiresAt: timestamppb.New(cert.NotAfter)}, nil
}

func (c *Control) Connect(stream grpc.BidiStreamingServer[pb.AgentMessage, pb.ServerMessage]) error {
	ctx := stream.Context()
	info, err := certificate(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, "node certificate required")
	}
	cert := info.State.PeerCertificates[0]
	id, err := pki.NodeID(cert)
	if err != nil {
		return status.Error(codes.Unauthenticated, "invalid node certificate")
	}
	fingerprint := pki.Fingerprint(cert)
	if err := c.Repository.ValidateIdentity(ctx, id, fingerprint); err != nil {
		return status.Error(codes.Unauthenticated, "node certificate revoked or unknown")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.NodeId != id || first.GetHello() == nil || first.Sequence != 0 || first.MessageId == "" || len(first.MessageId) > 128 || first.Timestamp == nil || first.Timestamp.CheckValid() != nil || len(first.GetHello().AgentVersion) > 64 {
		return status.Error(codes.InvalidArgument, "first message must be Hello for authenticated node")
	}
	c.mu.Lock()
	if c.connected == nil {
		c.connected = map[string]bool{}
	}
	if c.connected[id] {
		c.mu.Unlock()
		return status.Error(codes.AlreadyExists, "node already connected")
	}
	c.connected[id] = true
	c.mu.Unlock()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c.Repository.Offline(cleanup, id)
		if c.Notify != nil {
			c.Notify(id)
		}
		c.mu.Lock()
		delete(c.connected, id)
		c.mu.Unlock()
	}()
	last, err := c.Repository.HandshakeSequence(ctx, id, first.GetHello().LastAckedSequence)
	if err != nil {
		return status.Error(codes.FailedPrecondition, "original node sequence or accepted message unavailable")
	}
	if first.GetHello().LastAckedSequence > last {
		return status.Error(codes.FailedPrecondition, "central sequence older than node state")
	}
	if err := c.Repository.AgentVersion(ctx, id, first.GetHello().AgentVersion); err != nil {
		return status.Error(codes.Internal, "node version persistence failed")
	}
	restartSupported := first.GetHello().CoreRestartSupported
	if _, err := c.Repository.Database.ExecContext(ctx, "UPDATE nodes SET core_restart_supported=? WHERE id=?", restartSupported, id); err != nil {
		return status.Error(codes.Internal, "restart capability persistence failed")
	}
	healthCheckSupported := first.GetHello().HealthCheckSupported
	if err := c.Repository.healthCapability(ctx, id, healthCheckSupported); err != nil {
		return status.Error(codes.Internal, "health capability persistence failed")
	}
	updateSupported := first.GetHello().CoreUpdateSupported && c.CoreUpdates != nil
	agentUpdateSupported := first.GetHello().AgentUpdateSupported && c.AgentUpdates != nil
	agentRecoverySupported := agentUpdateSupported && first.GetHello().AgentRecoverySupported
	if err := c.Repository.AgentUpdateSupport(ctx, id, agentUpdateSupported, agentRecoverySupported); err != nil {
		return status.Error(codes.Internal, "Agent update capability persistence failed")
	}
	if err := c.Repository.CoreUpdateSupport(ctx, id, updateSupported); err != nil {
		return status.Error(codes.Internal, "core update capability persistence failed")
	}
	// Closing a stale update waits until this connection stored fresh
	// capabilities; see coreUpdateCapabilitiesStored.
	coreUpdates := &coreUpdateSession{}
	serverSequence := uint64(0)
	send := func(payload any) error {
		serverSequence++
		messageID, err := RandomID()
		if err != nil {
			return err
		}
		message := &pb.ServerMessage{NodeId: id, MessageId: messageID, Sequence: serverSequence, Timestamp: timestamppb.Now()}
		switch p := payload.(type) {
		case *pb.Welcome:
			message.Payload = &pb.ServerMessage_Welcome{Welcome: p}
		case *pb.Ack:
			message.Payload = &pb.ServerMessage_Ack{Ack: p}
		case *pb.HealthCheckCommand:
			message.Payload = &pb.ServerMessage_HealthCheckCommand{HealthCheckCommand: p}
		case *pb.ApplyCommand:
			message.Payload = &pb.ServerMessage_ApplyCommand{ApplyCommand: p}
		case *pb.RollbackCommand:
			message.Payload = &pb.ServerMessage_RollbackCommand{RollbackCommand: p}
		case *pb.CoreUpdateCommand:
			message.Payload = &pb.ServerMessage_CoreUpdateCommand{CoreUpdateCommand: p}
		case *pb.AgentUpdateCommand:
			message.Payload = &pb.ServerMessage_AgentUpdateCommand{AgentUpdateCommand: p}
		}
		return stream.Send(message)
	}
	if err := send(&pb.Welcome{LastAcceptedSequence: last, HeartbeatIntervalSeconds: uint32(HeartbeatInterval / time.Second), CoreUpdateSupported: updateSupported, AgentUpdateSupported: agentUpdateSupported, AgentRecoverySupported: agentRecoverySupported, HealthCheckSupported: healthCheckSupported, CoreRestartSupported: restartSupported, NodeCaMigrationPlanDigest: "", NodeCaReconfirmationPlanDigest: ""}); err != nil {
		return err
	}
	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := c.Repository.ValidateIdentity(ctx, id, fingerprint); err != nil {
			return status.Error(codes.Unauthenticated, "node certificate revoked or disabled")
		}
		if err := validateMessage(id, message); err != nil {
			return status.Error(codes.InvalidArgument, err.Error())
		}
		lastSequence, err := c.Repository.LastSequence(ctx, id)
		if err != nil {
			return status.Error(codes.Internal, "node sequence unavailable")
		}
		if message.Sequence > lastSequence+1 {
			return status.Error(codes.FailedPrecondition, "agent sequence gap")
		}
		if err := c.Repository.CheckAcceptedMessage(ctx, id, message); err != nil {
			return status.Error(codes.FailedPrecondition, "original accepted message conflicts or is unavailable")
		}
		accessBatchID := ""
		trafficBatchID := ""
		if batch := message.GetAccessBatch(); batch != nil {
			if c.AccessRecorder == nil {
				return status.Error(codes.FailedPrecondition, "access recorder unavailable")
			}
			accessBatchID, err = c.AccessRecorder.RecordAccess(ctx, id, batch, message.Sequence > lastSequence)
			if err != nil {
				return status.Error(codes.FailedPrecondition, "access batch rejected")
			}
		}
		if batch := message.GetTrafficBatch(); batch != nil {
			if c.AccessRecorder == nil {
				return status.Error(codes.FailedPrecondition, "traffic recorder unavailable")
			}
			trafficBatchID, err = c.AccessRecorder.RecordTraffic(ctx, id, batch, message.Sequence > lastSequence)
			if err != nil {
				return status.Error(codes.FailedPrecondition, "traffic batch rejected")
			}
		}
		if status := message.GetConnectionStatus(); status != nil && c.Connections != nil {
			// A live connection snapshot is advisory: keep it if it is valid,
			// ignore it otherwise, but never fail the stream over it. The
			// envelope size is already bounded by validateMessage.
			if snapshot, decodeErr := traffic.DecodeConnections(status.SnapshotJson); decodeErr == nil && snapshot.NodeID == id {
				if c.Connections.Record(id, snapshot) == nil && c.ConnectionsRecorded != nil {
					c.ConnectionsRecorded(ctx, id)
				}
			}
		}
		if result := message.GetApplyResult(); result != nil {
			if c.Deployment == nil {
				return status.Error(codes.FailedPrecondition, "deployment unavailable")
			}
			if err := c.Deployment.Result(ctx, id, result); err != nil {
				return status.Error(codes.FailedPrecondition, "deployment result rejected")
			}
		}
		if batch := message.GetOutboundHealth(); batch != nil {
			// Health observations remain independent from executable receipts.
			if c.HealthRecorder == nil {
				return status.Error(codes.FailedPrecondition, "outbound health recorder unavailable")
			}
			if err := c.HealthRecorder.RecordHealth(ctx, id, batch); err != nil {
				return status.Error(codes.InvalidArgument, "invalid outbound health batch")
			}
		}
		if result := message.GetCoreUpdateResult(); result != nil {
			if !updateSupported {
				return status.Error(codes.FailedPrecondition, "core updates unavailable")
			}
			if err := c.CoreUpdates.CoreUpdateResult(ctx, id, result); err != nil {
				return status.Error(codes.FailedPrecondition, "core update result rejected")
			}
		}
		if result := message.GetAgentUpdateResult(); result != nil {
			if !agentUpdateSupported {
				return status.Error(codes.FailedPrecondition, "Agent updates unavailable")
			}
			if err := c.AgentUpdates.AgentUpdateResult(ctx, id, result); err != nil {
				return status.Error(codes.FailedPrecondition, "Agent update result rejected")
			}
		}
		if result := message.GetHealthCheckResult(); result != nil {
			if !healthCheckSupported {
				return status.Error(codes.FailedPrecondition, "health checks unsupported")
			}
			if err := c.Repository.recordHealthCheck(ctx, id, result); err != nil {
				return status.Error(codes.FailedPrecondition, "health observation rejected")
			}
		}
		accepted, err := c.Repository.Record(ctx, id, message)
		if errors.Is(err, ErrSequence) {
			return status.Error(codes.FailedPrecondition, err.Error())
		}
		if err != nil {
			return status.Error(codes.Internal, "message persistence failed")
		}
		if updateSupported {
			if err := c.coreUpdateCapabilitiesStored(ctx, id, message, coreUpdates); err != nil {
				return status.Error(codes.Internal, "stale core update closure failed")
			}
		}
		if c.Notify != nil && (message.GetHeartbeat() != nil || message.GetAgentUpdateResult() != nil || message.GetHealthCheckResult() != nil) {
			c.Notify(id)
		}
		if result := message.GetApplyResult(); result != nil && c.DeploymentNotify != nil {
			c.DeploymentNotify(result.Revision)
		}
		if c.DashboardNotify != nil && message.Sequence > lastSequence && (message.GetAccessBatch() != nil || message.GetTrafficBatch() != nil || message.GetConnectionStatus() != nil) {
			c.DashboardNotify()
		}

		if err := send(&pb.Ack{AgentSequence: accepted, AccessBatchId: accessBatchID, TrafficBatchId: trafficBatchID}); err != nil {
			return err
		}
		// Agent requests reserve the node before other executable commands.
		if agentUpdateSupported && message.GetAgentUpdateResult() == nil {
			command, err := c.AgentUpdates.PendingAgentUpdate(ctx, id)
			if err != nil {
				return status.Error(codes.Internal, "Agent update command unavailable")
			}
			if command != nil {
				request, err := release.DecodeAgentUpdateRequest(command.RequestJson)
				if err != nil {
					return status.Error(codes.Internal, "invalid Agent command")
				}
				if request.Operation == "recover" && !agentRecoverySupported {
					continue
				}
				if err := send(command); err != nil {
					return err
				}
				continue
			}
		}
		if c.AgentUpdates != nil {
			reserved, err := c.AgentUpdates.AgentUpdateReserved(ctx, id)
			if err != nil {
				return status.Error(codes.Internal, "Agent update reservation unavailable")
			}
			if reserved {
				continue
			}
		}
		if healthCheckSupported {
			command, err := c.Repository.pendingHealthCheck(ctx, id)
			if err != nil {
				return status.Error(codes.Internal, "health command unavailable")
			}
			if command != nil {
				if err := send(command); err != nil {
					return err
				}
				continue
			}
		}
		if c.Deployment != nil && message.GetApplyResult() == nil {
			if updateSupported && message.GetCoreUpdateResult() == nil {
				command, err := c.CoreUpdates.PendingCoreUpdate(ctx, id)
				if err != nil {
					return status.Error(codes.Internal, "core update command unavailable")
				}
				if command != nil {
					if err := send(command); err != nil {
						return err
					}
					continue
				}
			}
			rollback, err := c.Deployment.PendingRollback(ctx, id)
			if err != nil {
				return status.Error(codes.Internal, "pending rollback unavailable")
			}
			if rollback != nil {
				if rollback.RestartCurrent && !restartSupported {
					if err := c.Deployment.Result(ctx, id, &pb.ApplyResult{CommandId: rollback.CommandId, Revision: rollback.Revision, Code: "restart_unsupported"}); err != nil {
						return status.Error(codes.FailedPrecondition, "unsupported restart receipt failed")
					}
					continue
				}
				if err := send(rollback); err != nil {
					return err
				}
				continue
			}
			command, err := c.Deployment.Pending(ctx, id)
			if err != nil {
				return status.Error(codes.Internal, "pending deployment unavailable")
			}
			if command != nil {
				if err := send(command); err != nil {
					return err
				}
			}
		}
	}
}

func validateMessage(id string, message *pb.AgentMessage) error {
	if message.NodeId != id || message.Sequence == 0 || message.Sequence > math.MaxInt64 || message.MessageId == "" || len(message.MessageId) > 128 || message.Timestamp == nil || message.Timestamp.CheckValid() != nil {
		return errors.New("invalid message envelope")
	}
	if batch := message.GetAccessBatch(); batch != nil {
		if len(batch.BatchJson) == 0 || len(batch.BatchJson) > 1<<20 {
			return errors.New("invalid access batch size")
		}
		return nil
	}
	if result := message.GetCoreUpdateResult(); result != nil {
		r, err := release.DecodeCoreUpdateResult(result.ResultJson)
		if err != nil || r.NodeID != id {
			return errors.New("invalid core update result")
		}
		return nil
	}
	if result := message.GetAgentUpdateResult(); result != nil {
		r, err := release.DecodeUpdateResult(result.ResultJson)
		if err != nil || r.NodeID != id {
			return errors.New("invalid Agent update result")
		}
		return nil
	}
	if batch := message.GetTrafficBatch(); batch != nil {
		if len(batch.BatchJson) == 0 || len(batch.BatchJson) > 1<<20 {
			return errors.New("invalid traffic batch size")
		}
		return nil
	}
	if message.GetFinalUsageBatch() != nil {
		// Old Agents may still emit final-usage batches. The concept is removed,
		// so accept and ignore the message rather than failing the stream.
		return nil
	}
	if status := message.GetConnectionStatus(); status != nil {
		// A malformed or oversized live connection snapshot must never fail the
		// stream; the handler re-decodes and ignores anything invalid. Only the
		// envelope size is enforced here.
		if len(status.SnapshotJson) > traffic.MaxConnectionSnapshotBytes {
			return errors.New("invalid connection snapshot size")
		}
		return nil
	}
	if batch := message.GetOutboundHealth(); batch != nil {
		if len(batch.Items) > 64 {
			return errors.New("outbound health batch limit exceeded")
		}
		return nil
	}
	if result := message.GetApplyResult(); result != nil {
		if result.Revision < 0 || result.Revision == 0 && result.CommandId == "" || len(result.Code) > 64 || len(result.CommandId) > 128 || result.Success && result.RolledBack {
			return errors.New("invalid deployment result")
		}
		return nil
	}
	if heartbeat := message.GetHeartbeat(); heartbeat != nil {
		if err := validateTrafficSourceStatus(heartbeat.TrafficSourceStatus); err != nil {
			return err
		}
		if collector := heartbeat.CollectorStatus; collector != nil {
			process, err := hex.DecodeString(collector.ProcessId)
			if err != nil || len(process) != 16 || hex.EncodeToString(process) != collector.ProcessId || collector.StartedAt == nil || collector.ObservedAt == nil || collector.StartedAt.CheckValid() != nil || collector.ObservedAt.CheckValid() != nil || collector.StartedAt.AsTime().After(collector.ObservedAt.AsTime()) || collector.PendingObservations > 2048 || collector.AggregatedRollups > 10000 {
				return errors.New("invalid collector observation")
			}
			if collector.Status != "ok" && collector.Status != "observation_loss" && collector.Status != "persistence_degraded" {
				return errors.New("invalid collector status")
			}
			if err := validateCollectorLossStatus(collector.PersistedLoss, collector); err != nil {
				return err
			}
			if collector.Status == "ok" && (collector.DroppedAllowedObservations != 0 || collector.DroppedBlockedObservations != 0 || collector.RejectedObservations != 0 || collector.CountersSaturated) {
				return errors.New("collector loss cannot be reported healthy")
			}
			for _, stamp := range []*timestamppb.Timestamp{collector.LastFlushAt, collector.LastFlushSuccessAt} {
				if stamp != nil && (stamp.CheckValid() != nil || stamp.AsTime().Before(collector.StartedAt.AsTime()) || stamp.AsTime().After(collector.ObservedAt.AsTime().Add(time.Second))) {
					return errors.New("invalid collector flush time")
				}
			}
		}
		if spool := heartbeat.SpoolStatus; spool != nil {
			if spool.FailureCode != "" {
				if spool.Measured {
					return errors.New("invalid spool failure classification")
				}
				switch spool.FailureCode {
				case "queue_unavailable", "capacity_unavailable", "capacity_invalid", "access_head_unavailable", "access_head_invalid", "traffic_head_unavailable", "traffic_head_invalid", "observation_timeout", "observation_interrupted":
				default:
					return errors.New("invalid spool failure classification")
				}
			}
			if spool.ObservedAt == nil || spool.ObservedAt.CheckValid() != nil {
				return errors.New("invalid spool observation time")
			}
			if spool.Measured {
				if spool.Status != "ok" && spool.Status != "near_capacity" && spool.Status != "capacity_exceeded" || spool.DiskLimit < 1 || spool.DatabaseLimit < 1 || spool.DiskLimit > 1<<30 || spool.DatabaseLimit > spool.DiskLimit || spool.DiskBytes > 1<<40 || spool.DatabaseBytes > 1<<40 || spool.PayloadBytes > 1<<40 {
					return errors.New("invalid spool capacity observation")
				}
			} else if spool.Status != "unavailable" || spool.DiskBytes != 0 || spool.PayloadBytes != 0 || spool.DiskLimit != 0 || spool.DatabaseBytes != 0 || spool.DatabaseLimit != 0 || spool.PendingBatches != 0 || spool.DroppedAllowedRollups != 0 {
				return errors.New("unmeasured spool must not contain invented counters")
			}
		}
		if heartbeat.GetActiveConnectionsMeasured() {
			if heartbeat.ActiveConnections > 100000000 || heartbeat.ActiveConnectionsScope != traffic.ConnectionScope || heartbeat.ActiveConnectionsObservedAt == nil || heartbeat.ActiveConnectionsObservedAt.CheckValid() != nil {
				return errors.New("invalid measured connections")
			}
		}
		if heartbeat.UploadBytes > 1<<50 || heartbeat.DownloadBytes > 1<<50 {
			return errors.New("invalid node traffic total")
		}
		if heartbeat.CoreError != "" && heartbeat.CoreError != "core_unhealthy" && heartbeat.CoreError != "accounting_interrupted" {
			return errors.New("invalid core health code")
		}
		if heartbeat.CoreError != "" && (heartbeat.CoreHealthy == nil || heartbeat.GetCoreHealthy()) {
			return errors.New("core health error requires an unhealthy observation")
		}
		for _, value := range []float64{heartbeat.GetCpuPercent(), heartbeat.GetMemoryPercent(), heartbeat.GetDiskPercent()} {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
				return errors.New("invalid resource percentage")
			}
		}
		if heartbeat.PublicIp != "" && net.ParseIP(heartbeat.PublicIp) == nil {
			return errors.New("invalid public IP")
		}
		if heartbeat.CurrentRevision < 0 || heartbeat.SuccessfulRevision < 0 {
			return errors.New("invalid revision")
		}
		return nil
	}
	if capabilities := message.GetCapabilities(); capabilities != nil {
		if len(capabilities.Protocols) > 16 || len(capabilities.CoreVersion) > 64 {
			return errors.New("invalid core capabilities")
		}
		if capabilities.CoreName != "" && capabilities.CoreName != "xray" && capabilities.CoreName != "sing-box" {
			return errors.New("unsupported core")
		}
		seen := map[string]bool{}
		for _, ability := range capabilities.Protocols {
			if ability == nil || !strings.Contains("|http|socks5|shadowsocks|vless|trojan|wireguard|hysteria2|tuic|tun|", "|"+ability.Protocol+"|") || seen[ability.Protocol] {
				return errors.New("invalid protocol capability")
			}
			seen[ability.Protocol] = true
		}
		return nil
	}
	if result := message.GetHealthCheckResult(); result != nil {
		if result.Id == "" || len(result.Id) > 128 || result.Revision < 1 || result.ObservedAt == nil || result.ObservedAt.CheckValid() != nil {
			return errors.New("invalid health result")
		}
		return nil
	}
	return errors.New("unsupported agent message")
}
