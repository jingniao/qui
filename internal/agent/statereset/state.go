// Package statereset implements the fixed, root-owned legacy discard journal.
// It never opens the old telemetry spool and never uploads or archives it.
package statereset

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v3"
	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/lifecycle"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/agentstorage"
)

const Directory = "/var/lib/proxycontrol-install/agent-state-reset"
const DataDirectory = "/var/lib/proxy-agent"
const ConfigPath = "/etc/proxy-agent/config.yaml"

type state struct {
	ResetID             string `json:"resetId"`
	NodeID              string `json:"nodeId"`
	PreviousFingerprint string `json:"previousFingerprint"`
	TargetSHA256        string `json:"targetSha256"`
	AgentVersion        string `json:"agentVersion"`
	Phase               string `json:"phase"`
	CSR                 []byte `json:"csr"`
}

func atomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".reset-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func save(directory string, s *state) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomic(filepath.Join(directory, "state.json"), data)
}
func read(directory string) (*state, error) {
	data, err := os.ReadFile(filepath.Join(directory, "state.json"))
	if err != nil {
		return nil, err
	}
	var s state
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err = d.Decode(&s); err != nil {
		return nil, err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("invalid reset journal")
	}
	return &s, nil
}

func safeTree(directory string) error {
	clean, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return err
	}
	if clean != resolved {
		return errors.New("reset refuses aliased directory")
	}
	return filepath.WalkDir(clean, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("reset refuses symlink: %s", path)
		}
		return nil
	})
}

func discard(journal, data string, s *state) error {
	j, err := filepath.Abs(journal)
	if err != nil {
		return err
	}
	d, err := filepath.Abs(data)
	if err != nil {
		return err
	}
	if j == d || strings.HasPrefix(j, d+string(os.PathSeparator)) || d == filepath.VolumeName(d)+string(os.PathSeparator) {
		return errors.New("reset journal must be outside old state")
	}
	if s.Phase != "sealed" && s.Phase != "deleting" {
		return errors.New("only sealed old state may be deleted")
	}
	if _, err := os.Lstat(d); err == nil {
		if err = safeTree(d); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.Phase = "deleting"
	if err = save(journal, s); err != nil {
		return err
	}
	if err = os.RemoveAll(d); err != nil {
		return err
	}
	if err = syncDirectory(filepath.Dir(d)); err != nil {
		return err
	}
	s.Phase = "deleted"
	return save(journal, s)
}

func prepare(directory string, c config.Agent, target, version string) (*state, error) {
	if old, err := read(directory); err == nil {
		if old.TargetSHA256 != target || old.AgentVersion != version {
			return nil, errors.New("pending reset belongs to another target; finish it first")
		}
		return old, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := safeTree(c.DataDirectory); err != nil {
		return nil, err
	}
	// Shared host certificate material is outside the discarded data root and is retained.
	if info, err := os.Lstat("/etc/proxy-agent/certs"); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("shared certificate directory is symlinked")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, path := range []string{c.TLS.ClientCA, c.Updates.CA} {
		if path != "" && strings.HasPrefix(filepath.Clean(path), c.DataDirectory+string(os.PathSeparator)) {
			return nil, errors.New("connection trust cannot reside in discarded Agent state")
		}
	}
	if _, err := c.TLS.ServerTrustConfig(c.ServerName); err != nil {
		return nil, err
	}
	if c.Updates.Enabled() {
		if _, err := c.Updates.TrustConfig(); err != nil {
			return nil, err
		}
	}
	old, err := identity.Load(filepath.Join(c.DataDirectory, "identity"))
	if err != nil {
		return nil, err
	}
	pair, err := identity.LoadKeyPairForNode(old.Directory, old.NodeID)
	if err != nil {
		return nil, err
	}
	if pair.Leaf == nil {
		return nil, errors.New("old identity has no verified certificate")
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	csr, err := identity.CSR(filepath.Join(directory, "identity"))
	if err != nil {
		return nil, err
	}
	// Re-serialize the effective operator configuration; no old runtime state,
	// enrollment tokens, journal or local telemetry enters the new installation.
	document := struct {
		config.Agent `yaml:",inline"`
		Cores        []configuration.Core `yaml:"cores"`
	}{c, configuration.Cores(c)}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return nil, err
	}
	if err = atomic(filepath.Join(directory, "config.yaml"), encoded); err != nil {
		return nil, err
	}
	s := &state{ResetID: hex.EncodeToString(nonce[:]), NodeID: old.NodeID, PreviousFingerprint: pki.Fingerprint(pair.Leaf), TargetSHA256: target, AgentVersion: version, Phase: "prepared", CSR: csr}
	if err = save(directory, s); err != nil {
		return nil, err
	}
	return s, nil
}

func request(ctx context.Context, c config.Agent, s *state, newIdentity bool) (*pb.EnrollResponse, error) {
	trust, err := c.TLS.ServerTrustConfig(c.ServerName)
	if err != nil {
		return nil, err
	}
	identityDir := filepath.Join(c.DataDirectory, "identity")
	if newIdentity {
		identityDir = filepath.Join(Directory, "identity")
	}
	pair, err := identity.LoadKeyPairForNode(identityDir, s.NodeID)
	if err != nil {
		return nil, err
	}
	trust.Certificates = []tls.Certificate{pair}
	conn, err := grpc.NewClient(c.ControlAddress, grpc.WithTransportCredentials(credentials.NewTLS(trust)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return pb.NewAgentControlClient(conn).ResetLegacyState(ctx, &pb.LegacyStateResetRequest{ResetId: s.ResetID, CsrPem: s.CSR, TargetSha256: s.TargetSHA256, AgentVersion: s.AgentVersion})
}

// Run is intentionally a fixed-path root CLI, not a general deletion facility.
func Run(ctx context.Context, operation, configPath, target, version string) (string, error) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return "", errors.New("legacy state reset requires Linux root")
	}
	if configPath != ConfigPath {
		return "", errors.New("legacy state reset requires the fixed installed config")
	}
	if err := os.MkdirAll(Directory, 0700); err != nil {
		return "", err
	}
	if err := safeTree(Directory); err != nil {
		return "", err
	}
	lock, err := filelock.Try(filepath.Join(Directory, "lock"))
	if err != nil {
		return "", err
	}
	defer lock.Close()
	s, err := read(Directory)
	if operation == "status" {
		if errors.Is(err, os.ErrNotExist) {
			return "pending", nil
		}
		if err != nil {
			return "", err
		}
		return s.Phase, nil
	}
	c, err := configuration.LoadAgent(configPath)
	if err != nil {
		return "", err
	}
	if c.DataDirectory != DataDirectory {
		return "", errors.New("legacy state reset requires /var/lib/proxy-agent")
	}
	if operation == "prepare" {
		decoded, e := hex.DecodeString(target)
		if e != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != target {
			return "", errors.New("invalid reset target digest")
		}
		executable, e := os.Executable()
		if e != nil {
			return "", e
		}
		data, e := os.ReadFile(executable)
		if e != nil {
			return "", e
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != target {
			return "", errors.New("reset target does not match executing binary")
		}
		s, err = prepare(Directory, c, target, version)
		if err != nil {
			return "", err
		}
		return s.Phase, nil
	}
	if s == nil {
		return "", errors.New("reset has not been prepared")
	}
	if operation == "complete" {
		if s.Phase == "complete" {
			return "complete", nil
		}
		if s.Phase != "rebuilt" {
			return "", errors.New("reset has not rebuilt new state")
		}
		for {
			response, err := request(ctx, c, s, true)
			if err != nil {
				return "", err
			}
			if response.StateResetStatus == "success" || response.StateResetStatus == "not_needed" {
				break
			}
			if response.StateResetStatus != "pending" && response.StateResetStatus != "queued" {
				return "", fmt.Errorf("reset redeployment remains %s", response.StateResetStatus)
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Second):
			}
		}
		s.Phase = "complete"
		if err = save(Directory, s); err != nil {
			return "", err
		}
		return s.Phase, nil
	}
	supervisor, err := lifecycle.TrySupervisor(c.DataDirectory)
	if err != nil {
		return "", err
	}
	defer supervisor.Close()
	worker, err := lifecycle.Try(c.DataDirectory)
	if err != nil {
		return "", err
	}
	defer worker.Close()
	switch operation {
	case "seal":
		if s.Phase != "prepared" {
			return s.Phase, nil
		}
		response, err := request(ctx, c, s, false)
		if err != nil {
			return "", err
		}
		if response.NodeId != s.NodeID {
			return "", errors.New("reset response changed node")
		}
		if err = identity.SaveEnrollment(filepath.Join(Directory, "identity"), s.NodeID, response.CertificatePem, response.CaPem); err != nil {
			return "", err
		}
		s.Phase = "sealed"
		if err = save(Directory, s); err != nil {
			return "", err
		}
	case "rebuild":
		if s.Phase == "rebuilt" || s.Phase == "complete" {
			return s.Phase, nil
		}
		if s.Phase == "sealed" || s.Phase == "deleting" {
			if err = discard(Directory, DataDirectory, s); err != nil {
				return "", err
			}
		}
		if s.Phase != "deleted" {
			return "", errors.New("reset was not sealed")
		}
		if err = os.MkdirAll(filepath.Join(DataDirectory, "identity"), 0700); err != nil {
			return "", err
		}
		for _, name := range []string{"client.key", "client.crt", "node-ca.crt", "identity.json"} {
			material, err := os.ReadFile(filepath.Join(Directory, "identity", name))
			if err != nil {
				return "", err
			}
			if err = atomic(filepath.Join(DataDirectory, "identity", name), material); err != nil {
				return "", err
			}
		}
		configData, err := os.ReadFile(filepath.Join(Directory, "config.yaml"))
		if err != nil {
			return "", err
		}
		if err = atomic(ConfigPath, configData); err != nil {
			return "", err
		}
		current := agentstorage.Current(version)
		current.Formats.Spool.Write = agentstorage.TrafficDeliveryFormat
		if _, err = storage.Ensure(DataDirectory, s.NodeID, current); err != nil {
			return "", err
		}
		s.Phase = "rebuilt"
		if err = save(Directory, s); err != nil {
			return "", err
		}
	default:
		return "", errors.New("unknown legacy state reset operation")
	}
	return s.Phase, nil
}
