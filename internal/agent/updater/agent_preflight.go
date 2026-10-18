package updater

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/release"
)

var ErrAgentPreflight = errors.New("signed Agent storage compatibility preflight failed")

type AgentPreflight struct {
	ControlOnly bool                  `json:"control_only,omitempty"`
	Image       Image                 `json:"image"`
	Contract    agentstorage.Contract `json:"contract"`
}
type boundedProbeOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    int
	overflow bool
}

func (w *boundedProbeOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(data) > w.limit-w.data.Len() {
		w.overflow = true
		return 0, ErrAgentPreflight
	}
	return w.data.Write(data)
}

// PreflightAgent only runs a verified, fixed local Agent image with the fixed
// information-only flag. It neither passes node paths/secrets nor changes the
// running process, signed acceptance sequence or installation journal.
func PreflightAgent(parent context.Context, directory string, candidate Candidate, policy release.Policy, current agentstorage.Contract) (AgentPreflight, error) {
	if candidate.Manifest.Component != "proxy-agent" || candidate.Manifest.OS != runtime.GOOS || candidate.Manifest.Architecture != runtime.GOARCH || candidate.Manifest != candidate.Signed.Manifest || candidate.SHA256 != candidate.Manifest.SHA256 || current.Validate() != nil {
		return AgentPreflight{}, ErrAgentPreflight
	}
	policy.Now = time.Now().UTC()
	if err := candidate.Signed.Verify(policy); err != nil {
		return AgentPreflight{}, errors.Join(ErrAgentPreflight, err)
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	image := imageOf(candidate.Manifest)
	_, err := MaterializeImage(ctx, directory, candidate.Path, "proxy-agent", image)
	if err != nil {
		return AgentPreflight{}, errors.Join(ErrAgentPreflight, err)
	}
	return ProbeAgentImage(ctx, directory, image, current)
}

// ProbeAgentImage rechecks a fixed image from authenticated installation
// history, including original local baselines whose sequence is zero. It does
// not authenticate a release or accept a digest supplied by a remote caller.
func ProbeAgentImage(parent context.Context, directory string, image Image, current agentstorage.Contract) (AgentPreflight, error) {
	if current.Validate() != nil {
		return AgentPreflight{}, ErrAgentPreflight
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	path, err := ResolveImage(ctx, directory, "proxy-agent", image)
	if err != nil {
		return AgentPreflight{}, errors.Join(ErrAgentPreflight, err)
	}
	stdout, stderr, err := runAgentProbe(ctx, path, "--storage-contract")
	if err != nil || len(stderr) != 0 {
		return AgentPreflight{}, ErrAgentPreflight
	}
	descriptor, err := agentstorage.Decode(stdout)
	if err != nil || descriptor.AgentVersion != image.Version || descriptor.OS != runtime.GOOS || descriptor.Architecture != runtime.GOARCH {
		return AgentPreflight{}, ErrAgentPreflight
	}
	extended, diagnostic, probeErr := runAgentProbe(ctx, path, "--storage-contract", "--storage-contract-version=2")
	if probeErr == nil {
		if len(diagnostic) != 0 {
			return AgentPreflight{}, ErrAgentPreflight
		}
		next, err := agentstorage.Decode(extended)
		if err != nil {
			return AgentPreflight{}, ErrAgentPreflight
		}
		base, nextBase := descriptor, next
		base.AgentRecoveryProtocol = 0
		nextBase.AgentRecoveryProtocol = 0
		if base != nextBase {
			return AgentPreflight{}, ErrAgentPreflight
		}
		descriptor = next
	} else if !legacyProbeFlag(probeErr, extended, diagnostic) || ctx.Err() != nil {
		return AgentPreflight{}, ErrAgentPreflight
	}
	// Verify the image again after the probe; the report never replaces signed
	// byte identity. Callers must still resolve the image at the real handoff.
	if _, err := ResolveImage(ctx, directory, "proxy-agent", image); err != nil {
		return AgentPreflight{}, errors.Join(ErrAgentPreflight, err)
	}
	controlOnly := false
	if descriptor.AgentRecoveryProtocol == 1 {
		out, diagnostic, err := runAgentProbe(ctx, path, "--control-only-contract")
		if err == nil {
			if len(diagnostic) != 0 || string(out) != "proxy-agent-control-only-v1 "+image.Version+"\n" {
				return AgentPreflight{}, ErrAgentPreflight
			}
			controlOnly = true
		} else if !unknownProbeFlag(err, out, diagnostic, "control-only-contract") || ctx.Err() != nil {
			return AgentPreflight{}, ErrAgentPreflight
		}
	}
	if _, err := ResolveImage(ctx, directory, "proxy-agent", image); err != nil {
		return AgentPreflight{}, err
	}
	descriptor, err = agentstorage.WithRequiredWriter(descriptor, current)
	if err != nil {
		return AgentPreflight{}, ErrAgentPreflight
	}
	return AgentPreflight{Image: image, Contract: descriptor, ControlOnly: controlOnly}, nil
}

func runAgentProbe(ctx context.Context, path string, args ...string) ([]byte, []byte, error) {
	stdout := &boundedProbeOutput{limit: agentstorage.MaxContractBytes}
	stderr := &boundedProbeOutput{limit: 8192}
	command := exec.CommandContext(ctx, path, args...)
	command.WaitDelay = 2 * time.Second
	command.Stdout, command.Stderr = stdout, stderr
	command.Env = []string{}
	for _, name := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "LANG"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	err := command.Run()
	if stdout.overflow || stderr.overflow {
		return nil, nil, ErrAgentPreflight
	}
	return stdout.data.Bytes(), stderr.data.Bytes(), err
}

func legacyProbeFlag(err error, stdout, stderr []byte) bool {
	return unknownProbeFlag(err, stdout, stderr, "storage-contract-version")
}

func unknownProbeFlag(err error, stdout, stderr []byte, name string) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 2 && len(stdout) == 0 && strings.HasPrefix(string(stderr), "flag provided but not defined: -"+name+"\n")
}
