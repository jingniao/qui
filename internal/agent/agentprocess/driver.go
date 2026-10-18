package agentprocess

import (
	"context"
	"errors"
	"io"

	"proxycontrol/internal/agent/updater"
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
	process, err := start(ctx, d.lifetime, d.directory, image, d.configPath, d.nodeID, d.revision, d.logs, false, d.agentUpdates, d.agentRecovery)
	if process != nil {
		d.process = process
	}
	if err != nil {
		return err
	}
	d.process, d.current = process, image
	return nil
}
func (d *Driver) Stop(ctx context.Context) error {
	if d.process == nil {
		return nil
	}
	process := d.process
	err := process.Stop(ctx)
	if process.Stopped() {
		d.process = nil
	}
	return err
}

func (d *Driver) Process() *Process { return d.process }

func (d *Driver) UnconfirmedStop() bool {
	return d.process != nil && d.process.Stopping() && !d.process.Stopped()
}

func (d *Driver) EnableAgentUpdates() { d.agentUpdates = true }

func (d *Driver) EnableAgentRecovery() { d.agentUpdates = true; d.agentRecovery = true }
