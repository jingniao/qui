package control

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"
	"proxycontrol/internal/agent/agenthost"
	"proxycontrol/internal/agent/capability"
	"proxycontrol/internal/agent/collector"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/agent/supervisor"
	"proxycontrol/internal/platform/config"
	access "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"proxycontrol/pkg/contracts/traffic"
)

// NewRuntime, when set, overrides the proxy core adapter the Agent runs. It is
// a test seam owned by the agent wiring: production leaves it nil and the Agent
// builds the real Xray or sing-box adapter from CoreName and CorePath.
var NewRuntime func(nodeID, directory string) core.Runtime

func transport(c config.Agent, enrolled bool, retainedNode ...string) (*tls.Config, error) {
	result, err := c.TLS.ServerTrustConfig(c.ServerName)
	if err != nil {
		return nil, err
	}
	if enrolled {
		loadPair := func() (tls.Certificate, error) {
			if len(retainedNode) > 0 {
				return identity.LoadKeyPairForNode(filepath.Join(c.DataDirectory, "identity"), retainedNode[0])
			}
			return identity.LoadKeyPair(filepath.Join(c.DataDirectory, "identity"))
		}
		pair, err := loadPair()
		if err != nil {
			return nil, err
		}
		result.Certificates = []tls.Certificate{pair}
		result.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			current, err := loadPair()
			return &current, err
		}
	}
	return result, nil
}

func Enroll(ctx context.Context, c config.Agent, token, version string) (resultErr error) {
	if err := c.Validate(); err != nil {
		return err
	}
	active, err := agenthost.Active(c.DataDirectory)
	if err != nil {
		return err
	}
	if active {
		return errors.New("cannot enroll over supervised Agent state")
	}
	supervisor, err := lifecycle.TrySupervisor(c.DataDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, supervisor.Close()) }()
	owner, err := lifecycle.Try(c.DataDirectory)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	c.DataDirectory = owner.Directory()
	directory := filepath.Join(c.DataDirectory, "identity")
	_, loadErr := identity.Load(directory)
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	tlsConfig, err := transport(c, false)
	if err != nil {
		return err
	}
	controlDigest, err := identity.EnrollmentControlDigest(c.ControlAddress, c.ServerName, c.TLS.ClientCA)
	if err != nil {
		return err
	}
	tokenDigest := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(token))))
	if found, err := identity.ResumeEnrollmentResult(ctx, c.DataDirectory, tokenDigest, controlDigest); found || err != nil {
		return err
	}
	if loadErr == nil {
		return errors.New("node is already enrolled")
	}
	if err := identity.PrepareEnrollmentAttempt(ctx, c.DataDirectory, tokenDigest, controlDigest); err != nil {
		return err
	}
	csr, err := identity.CSR(directory)
	if err != nil {
		return err
	}
	connection, err := grpc.NewClient(c.ControlAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return err
	}
	defer connection.Close()
	timeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	result, err := pb.NewAgentControlClient(connection).Enroll(timeout, &pb.EnrollRequest{EnrollmentToken: strings.TrimSpace(token), CsrPem: csr, Hostname: hostname, OperatingSystem: runtime.GOOS + "/" + runtime.GOARCH, AgentVersion: version})
	if err != nil {
		return fmt.Errorf("enrollment failed: %w", err)
	}
	if result == nil || result.ExpiresAt == nil || result.ExpiresAt.CheckValid() != nil {
		return errors.New("登记结果缺原有效期，保留私钥")
	}
	if err := identity.RetainEnrollmentResult(ctx, c.DataDirectory, tokenDigest, controlDigest, result.NodeId, result.CertificatePem, result.CaPem, result.ExpiresAt.AsTime()); err != nil {
		return err
	}
	found, err := identity.ResumeEnrollmentResult(ctx, c.DataDirectory, tokenDigest, controlDigest)
	if err != nil {
		return err
	}
	if !found {
		return errors.New("已保存原登记结果不可读，保留材料")
	}
	return nil
}

func Run(ctx context.Context, c config.Agent, logger *slog.Logger, versions ...string) error {
	return run(ctx, c, logger, nil, versions...)
}

// RunWithStartup reports local readiness only after restoring the retained
// configuration and checking its actual core. Central reachability is not a
// readiness requirement: nodes must keep working while the panel is offline.
type Startup struct {
	NodeID                string
	Revision              int64
	AllowDegraded         bool
	AgentUpdates          bool
	AgentRecovery         bool
	ControlOnly           bool
	ConnectionFormats     bool
	SuccessReceiptFormats bool
	Ready                 func(string, int64, bool) error
}

func RunWithStartup(ctx context.Context, c config.Agent, logger *slog.Logger, startup Startup, version string) error {
	if startup.Ready == nil || startup.NodeID == "" || startup.Revision < 0 || startup.ControlOnly && (!startup.AllowDegraded || !startup.AgentRecovery) {
		return errors.New("managed Agent requires a startup confirmation")
	}
	return run(ctx, c, logger, &startup, version)
}

func run(ctx context.Context, c config.Agent, logger *slog.Logger, startup *Startup, versions ...string) (resultErr error) {
	if err := c.Validate(); err != nil {
		return err
	}
	if startup == nil {
		active, err := agenthost.Active(c.DataDirectory)
		if err != nil {
			return err
		}
		if active {
			return errors.New("Agent supervision is active; start through the configured executable entrypoint")
		}
	}
	owner, err := lifecycle.Try(c.DataDirectory)
	if err != nil {
		return err
	}
	// Registered first, released last: a replacement cannot open state until
	// every worker and the core have stopped and the durable queue is closed.
	defer func() { resultErr = errors.Join(resultErr, owner.Close()) }()
	c.DataDirectory = owner.Directory()
	state, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("node is not enrolled; use --enroll --token-file with its one-time enrollment code")
	}
	if err != nil {
		return err
	}
	agentVersion := "dev"
	if startup != nil && startup.NodeID != state.NodeID {
		return errors.New("managed Agent node identity mismatch")
	}
	if len(versions) > 0 {
		agentVersion = versions[0]
	}
	storageContract := agentstorage.Current(agentVersion)
	requiredStorage, _, err := storage.Check(c.DataDirectory, state.NodeID, storageContract)
	if err != nil {
		return err
	}
	tlsConfig, err := transport(c, true, state.NodeID)
	if err != nil {
		return err
	}
	connection, err := grpc.NewClient(c.ControlAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 20 * time.Second, Timeout: 10 * time.Second}))
	if err != nil {
		return err
	}
	defer func() { connection.Close() }()
	installation, err := openCoreInstallation(ctx, c, state.NodeID)
	if err != nil {
		return fmt.Errorf("load installed core release: %w", err)
	}
	defer installation.Close()
	if installation != nil && installation.path != "" {
		c.CorePath = installation.path
	}
	var adapter core.Runtime
	if NewRuntime != nil {
		adapter = NewRuntime(state.NodeID, filepath.Join(c.DataDirectory, "runtime"))
	} else if c.CoreName == "sing-box" {
		adapter = &singbox.Adapter{NodeID: state.NodeID, Path: c.CorePath, Directory: filepath.Join(c.DataDirectory, "runtime")}
	} else {
		adapter = &xray.Adapter{NodeID: state.NodeID, Path: c.CorePath, Directory: filepath.Join(c.DataDirectory, "runtime")}
	}
	coreClosed := false
	closeCore := func() {
		if !coreClosed {
			coreClosed = true
			resultErr = errors.Join(resultErr, adapter.Close())
		}
	}
	defer closeCore()
	controlOnly := startup != nil && startup.ControlOnly
	if _, err := storage.Ensure(c.DataDirectory, state.NodeID, storageContract); err != nil {
		return err
	}
	// Raising the durable core-run lifetime marker ABI would strand an older
	// fallback image if that image cannot read the new write. Raise the floor only
	// after every retained image (original baseline, current and previous signed
	// installations) and the running program have proven they can read it. A
	// control-only worker is itself possibly an older image and must never write an
	// unreadable floor.
	if controlOnly {
		logger.Info("control-only worker: core accounting format floor left unchanged")
	} else if requiredStorage.Formats.CoreAccounting.Write >= agentstorage.CoreAccountingFormat {
		// The floor already writes the current ABI; every retained image was proven
		// readable when it was raised, so no per-start probe is needed.
	} else if err := agenthost.CheckCoreAccountingActivation(ctx, c.DataDirectory, state.NodeID, agentVersion); err != nil {
		// The retained fallback images cannot read the newer marker ABI. Preserve
		// the old write declaration, do not raise the floor and do not open a data
		// plane. When the supervisor negotiated a degraded/recovery start the node
		// stays manageable through control-only; otherwise refuse, so a fallback
		// image is never stranded by an irreversible write.
		if startup == nil || !startup.AllowDegraded || !startup.AgentRecovery {
			return fmt.Errorf("core accounting format cannot be raised safely: %w", err)
		}
		logger.Warn("core accounting format floor cannot be raised; starting control-only", "error", err)
		controlOnly = true
	} else if _, err := storage.ActivateCoreAccountingFormat(c.DataDirectory, state.NodeID, storageContract); err != nil {
		return err
	}
	newManager := reconcile.New
	if controlOnly {
		newManager = reconcile.NewControlOnly
	}
	manager, err := newManager(c.DataDirectory, state.NodeID, adapter)
	if err != nil {
		return err
	}
	// The enforced-source helper lives in the node's own state directory and is
	// driven with a fixed argument list; it is absent on installs that predate
	// the feature, which the Reconciler treats as "nothing to enforce".
	manager.HostNetworkDirectory = filepath.Join(c.DataDirectory, "host-network")
	manager.ControlAddress = c.ControlAddress
	if startup != nil && startup.Revision != manager.Current() {
		return errors.New("managed Agent retained revision mismatch")
	}
	manager.RequireSuccessReceipts = startup == nil || startup.SuccessReceiptFormats
	if manager.RequireSuccessReceipts {
		manager.PrepareStorage = func(ctx context.Context, desired desiredstate.State) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, err := storage.ActivateSuccessReceiptFormat(c.DataDirectory, state.NodeID, storageContract)
			return err
		}
	}
	telemetryQueue, queueErr := spool.OpenBound(ctx, filepath.Join(c.DataDirectory, "spool"), state.NodeID, 0, filepath.Join(state.Directory, "spool-store.json.enc"), nil)
	hooks := core.RuntimeHooks{}
	var collectorStatus func() *pb.CollectorStatus
	if queueErr != nil {
		logger.Error("telemetry queue unavailable; proxy remains blocked")
	} else {
		defer func() { resultErr = errors.Join(resultErr, telemetryQueue.Close()) }()
		hooks.EnableStats = true
		hooks.TrafficSink = telemetryQueue.SaveTraffic
		runtimeCollector, err := collector.NewRuntime(telemetryQueue, logger)
		if err != nil {
			return err
		}
		collectorCtx, collectorCancel := context.WithCancel(context.Background())
		go runtimeCollector.Run(collectorCtx)
		// Cancel and join the collector on every path, including the
		// core-start failures below, so its goroutine never outlives run.
		defer func() { collectorCancel(); runtimeCollector.Wait() }()
		hooks.AccessSink = runtimeCollector.Submit
		hooks.AccessRejected = runtimeCollector.Reject
		collectorStatus = runtimeCollector.Status
		defer closeCore()
	}
	adapter.ConfigureRuntime(hooks)
	if err := repairCoreInstallation(ctx, c, installation, manager); err != nil {
		if !errors.Is(err, core.ErrReleaseRecovery) && !errors.Is(err, core.ErrCoreStopUnconfirmed) {
			return fmt.Errorf("roll back interrupted core installation: %w", err)
		}
		logger.Error("proxy remains stopped pending core exit confirmation; control connection continues")
	}
	if err := manager.Restore(ctx); err != nil {
		if !errors.Is(err, core.ErrReleaseRecovery) && !errors.Is(err, core.ErrCoreStopUnconfirmed) {
			return fmt.Errorf("restore previous core configuration: %w", err)
		}
		logger.Error("proxy remains stopped pending release recovery or core exit confirmation; control connection continues")
	}
	installation.Close()
	supervisorCtx, supervisorCancel := context.WithCancel(ctx)
	supervisorDone := make(chan struct{})
	go func() { defer close(supervisorDone); supervisor.Run(supervisorCtx, adapter, manager, logger) }()
	defer func() { supervisorCancel(); <-supervisorDone }()
	statsCtx, statsCancel := context.WithCancel(ctx)
	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-statsCtx.Done():
				return
			case <-ticker.C:
				if !adapter.SupportsAccountTelemetry() {
					continue
				}
				if err := adapter.SampleTraffic(statsCtx); err != nil && statsCtx.Err() == nil {
					logger.Error("traffic sample could not be persisted")
				}
			}
		}
	}()
	defer func() { statsCancel(); <-statsDone }()
	updates, err := newCoreUpdates(ctx, c, state.NodeID, manager)
	if err != nil {
		return err
	}
	defer updates.Close()
	var agentUpdates *agentUpdates
	if startup != nil && startup.AgentUpdates {
		agentUpdates, err = newAgentUpdates(ctx, c, state.NodeID, startup.AgentRecovery)
		if err != nil {
			return err
		}
	}
	client := pb.NewAgentControlClient(connection)
	version := "dev"
	if len(versions) > 0 {
		version = versions[0]
	}
	capabilities, detectErr := adapter.DetectCapabilities(ctx)
	if capabilities == nil {
		capabilities = &pb.Capabilities{}
	}
	if detectErr != nil {
		logger.Info("proxy core not available", "error", detectErr)
	}
	if startup != nil {
		healthy := !controlOnly
		if manager.Current() > 0 {
			health, err := adapter.Health(ctx)
			if err != nil || !health.Healthy {
				healthy = false
				if !startup.AllowDegraded {
					return errors.Join(errors.New("managed Agent core is not ready"), err)
				}
			}
		}
		if err := startup.Ready(state.NodeID, manager.Current(), healthy); err != nil {
			return err
		}
	}
	sampler := capability.NewSampler(c.DataDirectory)
	sampler.CollectorStatus = collectorStatus
	if sources, ok := adapter.(interface {
		TrafficSourceStatus() *pb.TrafficSourceStatus
	}); ok {
		sampler.TrafficSourceStatus = sources.TrafficSourceStatus
	}
	renew := renewalCheck(state, func() pb.AgentControlClient { return pb.NewAgentControlClient(connection) })
	retry := time.Second
	for ctx.Err() == nil {
		streamContext, cancel := context.WithCancel(ctx)
		err := session(streamContext, client, state, logger, sampler, capabilities, version, manager, adapter, telemetryQueue, updates, agentUpdates, renew)
		cancel()
		if errors.Is(err, errCertificateRenewed) {
			connection.Close()
			connection, err = grpc.NewClient(c.ControlAddress, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 20 * time.Second, Timeout: 10 * time.Second}))
			if err != nil {
				return err
			}
			client = pb.NewAgentControlClient(connection)
			retry = time.Second
			continue
		}
		if ctx.Err() != nil {
			break
		}
		logger.Warn("control channel disconnected; retaining local state", "error", err, "retry_after", retry)
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		retry = min(30*time.Second, retry*2)
	}
	return nil
}

func session(ctx context.Context, client pb.AgentControlClient, state *identity.State, logger *slog.Logger, sampler *capability.Sampler, capabilities *pb.Capabilities, version string, manager *reconcile.Reconciler, healthSource interface {
	OutboundHealth() map[string]scheduler.Health
}, telemetryQueue *spool.Spool, updates *coreUpdates, agents *agentUpdates, renewals ...func(context.Context) (bool, error)) error {
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	defer stream.CloseSend()
	type packet struct {
		message *pb.ServerMessage
		err     error
	}
	incoming := make(chan packet, 1)
	go func() {
		for {
			message, err := stream.Recv()
			select {
			case incoming <- packet{message: message, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	receive := func() (*pb.ServerMessage, error) {
		select {
		case received := <-incoming:
			return received.message, received.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	messageID, err := newID()
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.AgentMessage{NodeId: state.NodeID, MessageId: messageID, Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Hello{Hello: &pb.Hello{LastAckedSequence: state.LastAcked, AgentVersion: version, HealthCheckSupported: true, CoreRestartSupported: true, BudgetRetirementSupported: false, CoreUpdateSupported: updates != nil, AgentUpdateSupported: agents != nil, AgentRecoverySupported: agents != nil && agents.recovery}}}); err != nil {
		return err
	}
	welcome, err := receive()
	if err != nil {
		return err
	}
	if welcome.NodeId != state.NodeID || welcome.GetWelcome() == nil {
		return errors.New("invalid Welcome")
	}
	sequence := welcome.GetWelcome().LastAcceptedSequence
	if sequence >= uint64(1<<63-1) {
		return errors.New("central sequence is outside the usable persistent range")
	}
	if sequence < state.LastAcked {
		return errors.New("central state is older than persisted agent state")
	}
	if err := state.Save(sequence); err != nil {
		return err
	}
	healthResults := []*pb.HealthCheckResult{}
	var lastHealthResult *pb.HealthCheckResult
	healthCheck := func(command *pb.HealthCheckCommand) {
		if lastHealthResult != nil && lastHealthResult.Id == command.Id && lastHealthResult.Revision == command.Revision {
			if len(healthResults) == 0 {
				healthResults = append(healthResults, lastHealthResult)
			}
			return
		}
		result := performHealthCheck(ctx, command, manager.Current(), healthSource, welcome.GetWelcome().HealthCheckSupported)
		if result.Healthy && manager.Current() != command.Revision {
			result.Healthy = false
			result.Code = "revision_changed"
		}
		lastHealthResult = result
		healthResults = append(healthResults, result)
	}
	results := []*pb.ApplyResult{}
	updateResults := []*pb.CoreUpdateResult{}
	agentResults := []*pb.AgentUpdateResult{}
	agentUpdate := func(message *pb.ServerMessage) error {
		if agents == nil || !welcome.GetWelcome().AgentUpdateSupported || message.NodeId != state.NodeID {
			return errors.New("Agent update not negotiated")
		}
		result, err := agents.submit(message.GetAgentUpdateCommand(), welcome.GetWelcome().AgentRecoverySupported)
		if err != nil {
			return err
		}
		if result != nil {
			agentResults = append(agentResults, result...)
		}
		return nil
	}
	var updateReplies <-chan *pb.CoreUpdateResult
	if updates != nil {
		updateReplies = updates.results
	}
	update := func(message *pb.ServerMessage) error {
		if updates == nil || !welcome.GetWelcome().CoreUpdateSupported || message.NodeId != state.NodeID {
			return errors.New("core update not negotiated")
		}
		return updates.submit(message.GetCoreUpdateCommand())
	}
	apply := func(command *pb.ApplyCommand) {
		desired, err := desiredstate.Decode(command.DesiredStateJson)
		result := &pb.ApplyResult{Revision: desired.Revision, Code: "compile_failed"}
		if err == nil {
			applied, applyErr := manager.Apply(ctx, desired)
			result.Success = applied.Success
			result.RolledBack = applied.RolledBack
			result.Code = applied.Code
			if applyErr != nil && result.Code == "" {
				result.Code = "apply_failed"
			}
		}
		results = append(results, result)
	}
	rollback := func(command *pb.RollbackCommand) {
		var success bool
		var code string
		if command.RestartCurrent {
			if welcome.GetWelcome().CoreRestartSupported {
				success, code, _ = manager.RestartCommand(ctx, command.CommandId, command.Revision)
			} else {
				code = "restart_unsupported"
			}
		} else {
			success, code, _ = manager.RollbackCommand(ctx, command.CommandId, command.Revision)
		}
		results = append(results, &pb.ApplyResult{Revision: command.Revision, CommandId: command.CommandId, Success: success, Code: code})
	}
	send := func(message *pb.AgentMessage) error {
		accessBatchID := ""
		trafficBatchID := ""
		if payload := message.GetAccessBatch(); payload != nil {
			batch, err := access.DecodeBatch(payload.BatchJson)
			if err != nil {
				return err
			}
			accessBatchID = batch.ID
		}
		if payload := message.GetTrafficBatch(); payload != nil {
			batch, err := traffic.Decode(payload.BatchJson)
			if err != nil {
				return err
			}
			trafficBatchID = batch.ID
		}
		if sequence >= uint64(1<<63-1) {
			return errors.New("persistent message sequence exhausted")
		}
		sequence++
		message.NodeId = state.NodeID
		message.Sequence = sequence
		message.Timestamp = timestamppb.Now()
		var err error
		message.MessageId, err = newID()
		if err != nil {
			return err
		}
		if err := stream.Send(message); err != nil {
			return err
		}
		var response *pb.ServerMessage
		for {
			response, err = receive()
			if err != nil {
				return err
			}
			if command := response.GetHealthCheckCommand(); command != nil {
				if response.NodeId != state.NodeID {
					return errors.New("health node mismatch")
				}
				healthCheck(command)
				continue
			}
			if response.GetRollbackCommand() != nil {
				rollback(response.GetRollbackCommand())
				continue
			}
			if response.GetCoreUpdateCommand() != nil {
				if err := update(response); err != nil {
					return err
				}
				continue
			}
			if response.GetAgentUpdateCommand() != nil {
				if err := agentUpdate(response); err != nil {
					return err
				}
				continue
			}
			if response.GetApplyCommand() != nil {
				apply(response.GetApplyCommand())
				continue
			}
			break
		}
		if response.NodeId != state.NodeID || response.GetAck() == nil || response.GetAck().AgentSequence != sequence {
			return errors.New("invalid sequence acknowledgment")
		}
		ack := response.GetAck()
		if ack.AccessBatchId != accessBatchID || ack.TrafficBatchId != trafficBatchID {
			return errors.New("acknowledgment does not match the sent telemetry identity")
		}
		if (accessBatchID != "" || trafficBatchID != "") && telemetryQueue == nil {
			return errors.New("telemetry queue unavailable before acknowledgment persistence")
		}
		if err := state.Save(sequence); err != nil {
			return err
		}
		if accessBatchID != "" {
			if response.GetAck().AccessBatchId != accessBatchID {
				return errors.New("invalid access batch acknowledgement")
			}
			if telemetryQueue == nil {
				return errors.New("access queue unavailable")
			}
			return telemetryQueue.Ack(ctx, accessBatchID, message.GetAccessBatch().BatchJson)
		}
		if trafficBatchID != "" {
			if response.GetAck().TrafficBatchId != trafficBatchID {
				return errors.New("invalid traffic acknowledgement")
			}
			if telemetryQueue == nil {
				return errors.New("traffic queue unavailable")
			}
			return telemetryQueue.AckTraffic(ctx, trafficBatchID, message.GetTrafficBatch().BatchJson)
		}
		return nil
	}
	if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_Capabilities{Capabilities: capabilities}}); err != nil {
		return err
	}
	logger.Info("node control connected", "node_id", state.NodeID)
	interval := time.Duration(welcome.GetWelcome().HeartbeatIntervalSeconds) * time.Second
	if interval < time.Second || interval > time.Minute {
		return errors.New("invalid heartbeat interval")
	}
	for ctx.Err() == nil {
		for len(healthResults) > 0 {
			if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_HealthCheckResult{HealthCheckResult: healthResults[0]}}); err != nil {
				return err
			}
			healthResults = healthResults[1:]
		}
		for len(agentResults) > 0 {
			if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_AgentUpdateResult{AgentUpdateResult: agentResults[0]}}); err != nil {
				return err
			}
			agentResults = agentResults[1:]
		}
		for len(updateResults) > 0 {
			if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_CoreUpdateResult{CoreUpdateResult: updateResults[0]}}); err != nil {
				return err
			}
			updateResults = updateResults[1:]
			if source, ok := healthSource.(core.Runtime); ok {
				if fresh, err := source.DetectCapabilities(ctx); err == nil {
					if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_Capabilities{Capabilities: fresh}}); err != nil {
						return err
					}
				}
			}
		}
		for len(results) > 0 {
			result := results[0]
			if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_ApplyResult{ApplyResult: result}}); err != nil {
				return err
			}
			results = results[1:]
		}
		if len(renewals) > 0 {
			renewed, err := renewals[0](ctx)
			if err != nil {
				logger.Warn("node certificate renewal failed; retaining current identity")
			}
			if renewed {
				return errCertificateRenewed
			}
		}
		heartbeat := sampler.Heartbeat()
		heartbeat.SpoolStatus = sampleSpoolStatus(ctx, telemetryQueue)
		heartbeat.CurrentRevision = manager.Current()
		heartbeat.SuccessfulRevision = manager.Current()
		if source, ok := healthSource.(interface {
			Health(context.Context) (core.HealthStatus, error)
		}); ok {
			status, healthErr := source.Health(ctx)
			if manager.Current() > 0 || errors.Is(healthErr, core.ErrCoreStopUnconfirmed) {
				healthy := healthErr == nil && status.Healthy
				heartbeat.CoreHealthy = &healthy
				if !healthy {
					heartbeat.CoreError = "core_unhealthy"
					if errors.Is(healthErr, core.ErrCoreStopUnconfirmed) {
						heartbeat.CoreError = "core_lifecycle_unconfirmed"
					}
				}
			}
		}
		var connectionSample []byte
		// The panel keeps live counts for its in-memory connection view only; the
		// source is the core's own upstream statistics, so a core that is not
		// running or not healthy reports nothing (plan D2: observed, never
		// invented).
		if heartbeat.GetCoreHealthy() {
			if source, ok := healthSource.(interface {
				ConnectionSnapshot(context.Context) (traffic.ConnectionSnapshot, error)
			}); ok {
				sample, err := source.ConnectionSnapshot(ctx)
				if err == nil && sample.NodeID == state.NodeID && sample.Revision == heartbeat.CurrentRevision && sample.Validate() == nil {
					connectionSample, err = json.Marshal(sample)
					if err == nil {
						measured := true
						heartbeat.ActiveConnectionsMeasured = &measured
						heartbeat.ActiveConnections = sample.Connections
						heartbeat.ActiveConnectionsScope = sample.Scope
						heartbeat.ActiveConnectionsObservedAt = timestamppb.New(sample.ObservedAt)
					}
				}
			}
		}
		// Node traffic totals are the only traffic a sing-box node can report
		// (plan D5). A core that cannot report them, or is not running, leaves
		// the heartbeat fields unset: the panel then shows the total as unknown.
		if source, ok := healthSource.(interface {
			NodeTrafficTotals(context.Context) (uint64, uint64, error)
		}); ok {
			if upload, download, err := source.NodeTrafficTotals(ctx); err == nil {
				heartbeat.UploadBytes, heartbeat.DownloadBytes = upload, download
			}
		}
		if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_Heartbeat{Heartbeat: heartbeat}}); err != nil {
			return err
		}
		if len(connectionSample) > 0 {
			if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_ConnectionStatus{ConnectionStatus: &pb.ConnectionStatus{SnapshotJson: connectionSample}}}); err != nil {
				return err
			}
		}
		statuses := healthSource.OutboundHealth()
		if len(statuses) > 0 {
			batch := &pb.OutboundHealthBatch{}
			for id, status := range statuses {
				batch.Items = append(batch.Items, &pb.OutboundHealth{OutboundId: id, Healthy: status.Healthy, LatencyMs: status.LatencyMS, CheckedAt: timestamppb.New(status.CheckedAt)})
			}
			if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_OutboundHealth{OutboundHealth: batch}}); err != nil {
				return err
			}
		}
		if telemetryQueue != nil {
			for count := 0; count < 8; count++ {
				batch, err := telemetryQueue.PendingTraffic(ctx)
				if err != nil {
					logger.Error("traffic queue read failed")
					break
				}
				if batch == nil {
					break
				}
				data, err := json.Marshal(batch)
				if err != nil {
					return err
				}
				if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_TrafficBatch{TrafficBatch: &pb.TrafficBatch{BatchJson: data}}}); err != nil {
					return err
				}
			}
			for count := 0; count < 8; count++ {
				batch, err := telemetryQueue.Pending(ctx)
				if err != nil {
					logger.Error("telemetry queue read failed")
					break
				}
				if batch == nil {
					break
				}
				data, err := json.Marshal(batch)
				if err != nil {
					return err
				}
				if err := send(&pb.AgentMessage{Payload: &pb.AgentMessage_AccessBatch{AccessBatch: &pb.AccessBatch{BatchJson: data}}}); err != nil {
					return err
				}
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case result := <-updateReplies:
			timer.Stop()
			updateResults = append(updateResults, result)
			continue
		case received := <-incoming:
			timer.Stop()
			if received.err != nil {
				return received.err
			}
			if command := received.message.GetHealthCheckCommand(); command != nil {
				if received.message.NodeId != state.NodeID {
					return errors.New("health node mismatch")
				}
				healthCheck(command)
				continue
			}
			if received.message.GetRollbackCommand() != nil {
				rollback(received.message.GetRollbackCommand())
				continue
			}
			if received.message.GetCoreUpdateCommand() != nil {
				if err := update(received.message); err != nil {
					return err
				}
				continue
			}
			if received.message.GetAgentUpdateCommand() != nil {
				if err := agentUpdate(received.message); err != nil {
					return err
				}
				continue
			}
			if received.message.GetApplyCommand() != nil {
				apply(received.message.GetApplyCommand())
				continue
			}
			return errors.New("unexpected server message between heartbeats")
		case <-timer.C:
		}
	}
	return ctx.Err()
}
