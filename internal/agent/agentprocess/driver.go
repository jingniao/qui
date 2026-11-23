package agentprocess

import (
	"context"
	"errors"
	"io"
	"path/filepath"

	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/agentstorage"
)

// Driver implements the existing durable installation transaction with real
// Agent processes. Its owner serializes all calls and retains the authenticated
// release History through commit/rollback. Hold is process-local: on parent
// death the worker drains via EOF, and pending History selects rollback.
type Driver struct {
	directory     string
	configPath    string
	nodeID        string
	revision      int64
	required      agentstorage.Contract
	current       updater.Image
	images        map[string]updater.Image
	process       *Process
	logs          io.Writer
	held          bool
	lifetime      context.Context
	agentUpdates  bool
	agentRecovery bool
	handover      *core.HandoverRequest
	handoverKey   *vault.Vault
	stopFailed    bool
}

var _ updater.Driver = (*Driver)(nil)

// NewDriver receives only authenticated current/target image identities. The
// caller stages and verifies signed candidates before constructing this fixed
// scope. There is no path or argument supplied by the control protocol.
func NewDriver(ctx context.Context, directory, configPath, nodeID string, revision int64, current updater.Image, target *updater.Image, required agentstorage.Contract, logs io.Writer) (*Driver, error) {
	if nodeID == "" || revision < 1 || configPath == "" {
		return nil, ErrHandshake
	}
	d := &Driver{directory: directory, configPath: configPath, nodeID: nodeID, revision: revision, required: required, current: current, images: map[string]updater.Image{}, logs: logs, held: true, lifetime: ctx}
	images := []updater.Image{current}
	if target != nil {
		images = append(images, *target)
	}
	for _, image := range images {
		probe, err := updater.ProbeAgentImage(ctx, directory, image, required)
		if err != nil || probe.Contract.ManagedProtocol != 1 {
			return nil, errors.Join(ErrHandshake, err)
		}
		path, err := updater.ResolveImage(ctx, directory, "proxy-agent", image)
		if err != nil {
			return nil, err
		}
		d.images[path] = image
	}
	return d, nil
}

func (d *Driver) Component() string { return "proxy-agent" }
func (d *Driver) NodeID() string    { return d.nodeID }
func (d *Driver) Revision() int64   { return d.revision }
func (d *Driver) Current(ctx context.Context) (string, string, error) {
	path, err := updater.ResolveImage(ctx, d.directory, d.Component(), d.current)
	return path, d.current.Version, err
}
func (d *Driver) Check(ctx context.Context, path, version string) error {
	image, ok := d.images[path]
	if !ok || image.Version != version {
		return ErrHandshake
	}
	probe, err := updater.ProbeAgentImage(ctx, d.directory, image, d.required)
	if err != nil || probe.Contract.ManagedProtocol != 1 || d.agentUpdates && probe.Contract.AgentUpdateProtocol != 1 || d.agentRecovery && probe.Contract.AgentRecoveryProtocol != 1 {
		return errors.Join(ErrHandshake, err)
	}
	return nil
}
func (d *Driver) Hold() error { d.held = true; return nil }
func (d *Driver) Release() error {
	if d.process == nil || d.process.Stopping() {
		return ErrHandshake
	}
	select {
	case <-d.process.Done():
		return errors.Join(ErrHandshake, d.process.Wait())
	default:
		d.held = false
		return nil
	}
}
func (d *Driver) Activate(ctx context.Context, path string) error {
	if d.stopFailed {
		return ErrStopUnconfirmed
	}
	if !d.held {
		return ErrHandshake
	}
	image, ok := d.images[path]
	if !ok {
		return ErrHandshake
	}
	if err := d.Check(ctx, path, image.Version); err != nil {
		return err
	}
	if err := d.Stop(ctx); err != nil {
		return err
	}
	request := d.handover
	d.handover = nil // Authorization may be exercised for the candidate only once.
	if request != nil {
		if image.SHA256 != request.TargetAgentSHA256 {
			return core.ErrHandoverConflict
		}
		if err := d.confirmHandover(*request, core.HandoverDetached, nil); err != nil {
			return err
		}
	} else if err := ConfirmCoresStopped(filepath.Dir(filepath.Dir(d.directory))); err != nil {
		d.stopFailed = true
		return err
	}
	process, err := start(ctx, d.lifetime, d.directory, image, d.configPath, d.nodeID, d.revision, d.logs, false, d.agentUpdates, d.agentRecovery)
	if process != nil {
		d.process = process
	}
	if err != nil {
		if errors.Is(err, ErrStopUnconfirmed) {
			d.stopFailed = true
		}
		return err
	}
	if request != nil {
		// start has already waited for the existing authenticated Ready frame.
		// Require its durable adoption evidence before Installer can commit.
		if err := d.confirmHandover(*request, core.HandoverAdopted, process); err != nil {
			return err
		}
	}
	d.process, d.current = process, image
	return nil
}
func (d *Driver) Stop(ctx context.Context) error {
	if d.process == nil {
		return nil
	}
	process := d.process
	err := process.StopWithoutHandover(ctx)
	if err == nil {
		err = ConfirmCoresStopped(filepath.Dir(filepath.Dir(d.directory)))
	}
	if err != nil {
		d.stopFailed = true
	}
	if process.Stopped() {
		d.process = nil
	}
	return err
}

func (d *Driver) Process() *Process { return d.process }

func (d *Driver) UnconfirmedStop() bool {
	return d.stopFailed || d.process != nil && d.process.Stopping() && !d.process.Stopped()
}

func (d *Driver) EnableCoreHandover(request core.HandoverRequest, key *vault.Vault) {
	d.handover, d.handoverKey = &request, key
}

func (d *Driver) confirmHandover(request core.HandoverRequest, state core.HandoverState, process *Process) error {
	if err := core.AuthorizeHandoverRequest(request, core.HandoverAuthorization{NodeID: d.nodeID, Environment: core.ObserveOSEnvironment(), SourceAgent: request.SourceAgent, TargetAgentSHA256: request.TargetAgentSHA256, TransactionID: request.TransactionID, Scope: "worker"}); err != nil {
		return err
	}
	receipt, err := core.LoadHandoverReceipt(filepath.Dir(filepath.Dir(d.directory)), d.handoverKey)
	if err != nil {
		return err
	}
	if err := receipt.MatchRequest(request); err != nil {
		return err
	}
	if receipt.State != state {
		return core.ErrHandoverConflict
	}
	if process != nil {
		token, err := core.ObserveProcessIdentity(process.command.Process.Pid)
		if err != nil {
			return err
		}
		if receipt.TargetAgent != (core.ProcessIdentity{PID: process.command.Process.Pid, Token: token}) {
			return core.ErrHandoverConflict
		}
	}
	return nil
}

func (d *Driver) EnableAgentUpdates() { d.agentUpdates = true }

func (d *Driver) EnableAgentRecovery() { d.agentUpdates = true; d.agentRecovery = true }
