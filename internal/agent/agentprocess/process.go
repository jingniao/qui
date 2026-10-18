package agentprocess

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"proxycontrol/internal/agent/updater"
	"proxycontrol/pkg/contracts/agentstorage"
)

type Process struct {
	command    *exec.Cmd
	input      io.WriteCloser
	done       chan struct{}
	waitErr    error
	closeInput sync.Once
	degraded   bool
	stopping   atomic.Bool
}

var ErrStopUnconfirmed = updater.ErrProcessStopUnconfirmed

// Start launches only a digest-derived, immutable local Agent image. Callers
// must obtain image from authenticated installation history or signed
// preflight; it must never be constructed from an unauthenticated request.
// configPath is the locally configured Agent file, never a control message.
func Start(ctx context.Context, directory string, image updater.Image, configPath, nodeID string, revision int64, logs io.Writer) (*Process, error) {
	return start(ctx, ctx, directory, image, configPath, nodeID, revision, logs, false, false)
}

// StartRetained can keep authenticated control online for an already confirmed
// image whose core is protected. It must not be used to commit a new release.
func StartRetained(ctx context.Context, directory string, image updater.Image, configPath, nodeID string, revision int64, logs io.Writer, agentUpdates ...bool) (*Process, error) {
	return start(ctx, ctx, directory, image, configPath, nodeID, revision, logs, true, len(agentUpdates) > 0 && agentUpdates[0], len(agentUpdates) > 1 && agentUpdates[1])
}

func StartControlOnly(ctx context.Context, directory string, image updater.Image, configPath, nodeID string, revision int64, required agentstorage.Contract, logs io.Writer) (*Process, error) {
	probe, err := updater.ProbeAgentImage(ctx, directory, image, required)
	if err != nil || !probe.ControlOnly || probe.Contract.AgentRecoveryProtocol != 1 {
		return nil, errors.Join(ErrHandshake, err)
	}
	return start(ctx, ctx, directory, image, configPath, nodeID, revision, logs, true, true, true, true)
}

func start(ctx, lifetime context.Context, directory string, image updater.Image, configPath, nodeID string, revision int64, logs io.Writer, allowDegraded, agentUpdates bool, recovery ...bool) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := updater.ResolveImage(ctx, directory, "proxy-agent", image)
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		return nil, ErrHandshake
	}
	configPath, err = filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	request := Request{Kind: "agent-start-v1", Nonce: hex.EncodeToString(nonce), NodeID: nodeID, Revision: revision, Version: image.Version, AllowDegraded: allowDegraded, AgentUpdates: agentUpdates}
	request.AgentRecovery = len(recovery) > 0 && recovery[0]
	request.ControlOnly = len(recovery) > 1 && recovery[1]
	probe, err := updater.ProbeAgentImage(ctx, directory, image, agentstorage.Current(image.Version))
	if err != nil {
		return nil, err
	}
	request.ConnectionFormats = agentstorage.SupportsConnectionFormats(probe.Contract)
	request.SuccessReceiptFormats = probe.Contract.Formats.Configuration.ReadMax >= 2
	if request.Validate() != nil {
		return nil, ErrHandshake
	}
	command := exec.Command(path, "--managed-worker", "--config", configPath)
	command.Stderr = logs
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err := command.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	p := &Process{command: command, input: input, done: make(chan struct{})}
	go func() { p.waitErr = command.Wait(); close(p.done) }()
	ready := make(chan error, 1)
	go func() {
		if err := json.NewEncoder(input).Encode(request); err != nil {
			ready <- err
			return
		}
		var response Ready
		if readFrame(bufio.NewReader(io.LimitReader(output, MaxHandshakeBytes+1)), &response) != nil || (response.Kind != "agent-ready-v1" && !(allowDegraded && response.Kind == "agent-degraded-v1")) || request.ControlOnly && response.Kind != "agent-degraded-v1" || response.Nonce != request.Nonce || response.NodeID != nodeID || response.Revision != revision || response.Version != image.Version {
			ready <- ErrHandshake
			return
		}
		p.degraded = response.Kind == "agent-degraded-v1"
		ready <- nil
	}()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case err = <-ready:
	case <-p.done:
		err = errors.Join(ErrHandshake, p.waitErr)
	case <-startup.Done():
		err = startup.Err()
	}
	if err == nil {
		select {
		case <-p.done:
			err = errors.Join(ErrHandshake, p.waitErr)
		default:
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		stopCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		stopErr := p.Stop(stopCtx)
		if !p.Stopped() {
			return p, errors.Join(err, stopErr, ErrStopUnconfirmed)
		}
		return nil, errors.Join(err, stopErr)
	}
	// EOF is the only lifetime control. A dead parent closes the anonymous
	// pipe too, so the worker cancels and drains without a shell or signal API.
	go func() {
		select {
		case <-lifetime.Done():
			stopCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			_ = p.Stop(stopCtx)
		case <-p.done:
		}
	}()
	return p, nil
}

func (p *Process) Done() <-chan struct{} { return p.done }

func (p *Process) Degraded() bool { return p.degraded }

func (p *Process) Stopping() bool { return p.stopping.Load() }

func (p *Process) Stopped() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

func (p *Process) Wait() error { <-p.done; return p.waitErr }

func (p *Process) Stop(ctx context.Context) error {
	p.stopping.Store(true)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p.closeInput.Do(func() { _ = p.input.Close() })
	select {
	case <-p.done:
		return p.waitErr
	case <-ctx.Done():
		killErr := p.command.Process.Kill()
		reap := time.NewTimer(5 * time.Second)
		defer reap.Stop()
		select {
		case <-p.done:
			return errors.Join(ctx.Err(), killErr, p.waitErr)
		case <-reap.C:
			return errors.Join(ErrStopUnconfirmed, ctx.Err(), killErr)
		}
	}
}
