//go:build linux

package handovercli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"proxycontrol/internal/agent/agenthost"
	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
)

type localState struct {
	config config.Agent
	nodeID string
	key    *vault.Vault
}

func loadLocal(c config.Agent) (*localState, error) {
	node, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		return nil, err
	}
	if _, err := identity.LoadKeyPairForNode(node.Directory, node.NodeID); err != nil {
		return nil, err
	}
	// Enrolled control workers use DataDirectory/identity, exactly as transport
	// does; the pre-enrollment TLS paths are not a second node identity.
	key, err := os.ReadFile(filepath.Join(c.DataDirectory, "configuration.key"))
	if err != nil {
		return nil, err
	}
	defer clear(key)
	v, err := vault.FromKey(key)
	if err != nil {
		return nil, err
	}
	return &localState{config: c, nodeID: node.NodeID, key: v}, nil
}

func digestFile(path string) (result string, resultErr error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

// lockOwner obtains the actual lifecycle owner from the kernel, never from a
// pidfile or process name. Absence is distinct from inaccessible lock evidence.
func lockOwner(directory, name string) (int, error) {
	var stat unix.Stat_t
	if err := unix.Stat(filepath.Join(directory, name), &stat); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	data, err := os.ReadFile("/proc/locks")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[1] != "FLOCK" || fields[3] != "WRITE" {
			continue
		}
		parts := strings.Split(fields[5], ":")
		if len(parts) != 3 {
			return 0, errors.New("invalid /proc/locks device identity")
		}
		major, e1 := strconv.ParseUint(parts[0], 16, 32)
		minor, e2 := strconv.ParseUint(parts[1], 16, 32)
		inode, e3 := strconv.ParseUint(parts[2], 10, 64)
		if err := errors.Join(e1, e2, e3); err != nil {
			return 0, err
		}
		if major == uint64(unix.Major(uint64(stat.Dev))) && minor == uint64(unix.Minor(uint64(stat.Dev))) && inode == stat.Ino {
			pid, err := strconv.Atoi(fields[4])
			if err != nil || pid <= 0 {
				return 0, errors.Join(core.ErrHandoverConflict, err)
			}
			return pid, nil
		}
	}
	return 0, nil
}

func processIdentity(pid int) (core.ProcessIdentity, error) {
	token, err := core.ObserveProcessIdentity(pid)
	if err != nil {
		return core.ProcessIdentity{}, err
	}
	if core.ClassifyRecordedProcess(pid, token) != core.ProcessAlive {
		return core.ProcessIdentity{}, core.ErrHandoverConflict
	}
	ns, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/pid", pid))
	if err != nil {
		return core.ProcessIdentity{}, err
	}
	if ns != core.ObserveOSEnvironment().NamespaceID {
		return core.ProcessIdentity{}, core.ErrHandoverConflict
	}
	return core.ProcessIdentity{PID: pid, Token: token}, nil
}

func probePidfd(process core.ProcessIdentity) (resultErr error) {
	fd, err := unix.PidfdOpen(process.PID, 0)
	if err != nil {
		return errors.Join(core.ErrHandoverUnsupported, err)
	}
	defer func() { resultErr = errors.Join(resultErr, unix.Close(fd)) }()
	if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
		return errors.Join(core.ErrHandoverUnsupported, err)
	}
	if core.ClassifyRecordedProcess(process.PID, process.Token) != core.ProcessAlive {
		return core.ErrHandoverConflict
	}
	return nil
}

func canonical(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

func processConfig(pid int) (string, bool, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return "", false, err
	}
	args := bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0})
	path, managed := "", false
	for i := 1; i < len(args); i++ {
		arg := string(args[i])
		if arg == "--managed-worker" || arg == "--managed-worker=true" {
			managed = true
		}
		if arg == "--config" && i+1 < len(args) {
			i++
			path = string(args[i])
		} else if strings.HasPrefix(arg, "--config=") {
			path = strings.TrimPrefix(arg, "--config=")
		}
	}
	if path == "" {
		return "", managed, nil
	}
	if !filepath.IsAbs(path) {
		cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		if err != nil {
			return "", managed, err
		}
		path = filepath.Join(cwd, path)
	}
	path, err = canonical(path)
	return path, managed, err
}

func target(ctx context.Context, path string, s *localState) (string, error) {
	c := s.config
	active, err := agenthost.Active(c.DataDirectory)
	if err != nil {
		return "", err
	}
	if !active {
		executable, err := os.Executable()
		if err != nil {
			return "", err
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil {
			return "", err
		}
		return digestFile(executable)
	}
	pid, err := lockOwner(c.DataDirectory, ".agent-supervisor.lock")
	if err != nil {
		return "", err
	}
	status, err := agenthost.LocalStatus(path, pid == 0)
	if err != nil {
		return "", err
	}
	if status.NodeID != s.nodeID {
		return "", core.ErrHandoverConflict
	}
	image, err := updater.ResolveImage(ctx, filepath.Join(c.DataDirectory, "releases", "agent-installed"), "proxy-agent", status.Current)
	if err != nil {
		return "", err
	}
	return digestFile(image)
}

func sourceAgent(path string, c config.Agent) (core.ProcessIdentity, error) {
	pid, err := lockOwner(c.DataDirectory, ".agent-owner.lock")
	if err != nil {
		return core.ProcessIdentity{}, err
	}
	if pid == 0 {
		return core.ProcessIdentity{}, errors.New("source Agent lifecycle owner is absent")
	}
	command, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return core.ProcessIdentity{}, err
	}
	if !runtimeCommand(command) {
		return core.ProcessIdentity{}, conflict("lifecycle owner is an operator command, not a running Agent")
	}
	process, err := processIdentity(pid)
	if err != nil {
		return process, err
	}
	expected, err := canonical(path)
	if err != nil {
		return process, err
	}
	actual, managed, err := processConfig(pid)
	if err != nil {
		return process, err
	}
	active, err := agenthost.Active(c.DataDirectory)
	if err != nil {
		return process, err
	}
	if actual != expected || managed != active {
		return process, core.ErrHandoverConflict
	}
	if core.ClassifyRecordedProcess(pid, process.Token) != core.ProcessAlive {
		return process, core.ErrHandoverConflict
	}
	return process, nil
}

func runtimeCommand(data []byte) bool {
	args := bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0})
	if len(args) == 0 || len(args[0]) == 0 {
		return false
	}
	for i := 1; i < len(args); i++ {
		arg := string(args[i])
		switch {
		case arg == "--config":
			i++
			if i == len(args) {
				return false
			}
		case strings.HasPrefix(arg, "--config="), arg == "--managed-worker", arg == "--managed-worker=true", arg == "--supervise", arg == "--supervise=true":
		default:
			return false
		}
	}
	return true
}

// The lifecycle lock prevents new workers from opening state. Also inspect
// runtime command lines, so a worker between lock release and process exit is
// not mistaken for a stopped worker. Helpers/operators are excluded explicitly.
func confirmWorkersExited(configPath string, c config.Agent) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	expected, err := canonical(configPath)
	if err != nil {
		return err
	}
	dataRoot, err := canonical(c.DataDirectory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dataAbsent := errors.Is(err, os.ErrNotExist)
	active, err := agenthost.Active(c.DataDirectory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		args := bytes.Split(bytes.TrimSuffix(data, []byte{0}), []byte{0})
		if len(args) == 0 || len(args[0]) == 0 {
			continue
		}
		base := filepath.Base(string(args[0]))
		if !strings.HasPrefix(base, "proxy-agent") {
			continue
		}
		if !runtimeCommand(data) {
			continue
		}
		actual, managed, err := processConfig(pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if actual == "" || active && !managed {
			continue
		}
		matches := actual == expected
		if !matches && !dataAbsent {
			other, err := configuration.LoadAgent(actual)
			if err != nil {
				return err
			}
			otherRoot, err := canonical(other.DataDirectory)
			if err != nil {
				return err
			}
			matches = otherRoot == dataRoot
		}
		if !matches {
			continue
		}
		token, err := core.ObserveProcessIdentity(pid)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if core.ClassifyRecordedProcess(pid, token) != core.ProcessExited {
			return fmt.Errorf("Agent process %d remains alive or unknown", pid)
		}
	}
	return nil
}
