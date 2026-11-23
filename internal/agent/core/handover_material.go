package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/scheduler"
)

const CoreRuntimeFormat = "core-runtime-v1"

// MaxHandoverMaterialBytes is a file-format boundary, not a runtime truncation.
const MaxHandoverMaterialBytes = 16 << 20

type RuntimeMaterialState string

const (
	RuntimePrepared     RuntimeMaterialState = "prepared"
	RuntimeActive       RuntimeMaterialState = "active"
	RuntimeExitObserved RuntimeMaterialState = "exit_observed"
)

type PrivatePort struct {
	Port    int    `json:"port"`
	Purpose string `json:"purpose"`
}

// LogCheckpoint points immediately after a complete line. No parser pending
// state or sing-box connection/source mapping is persisted (Owner D3).
type LogCheckpoint struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

type RuntimeMaterial struct {
	Format                string                      `json:"format"`
	State                 RuntimeMaterialState        `json:"state"`
	Run                   CoreRun                     `json:"run"`
	RunSHA256             string                      `json:"run_sha256"`
	RenderMetadataVersion int                         `json:"render_metadata_version"`
	Rendered              RenderedConfig              `json:"rendered"`
	ConfigSHA256          string                      `json:"config_sha256"`
	PrivatePorts          []PrivatePort               `json:"private_ports"`
	ClashSecret           string                      `json:"clash_secret"`
	ProbePorts            map[string]int              `json:"probe_ports"`
	ExecutablePath        string                      `json:"executable_path"`
	ExecutableSHA256      string                      `json:"executable_sha256"`
	ExecutableVersion     string                      `json:"executable_version"`
	Arguments             []string                    `json:"arguments"`
	Environment           map[string]string           `json:"environment"`
	DependencySHA256      map[string]string           `json:"dependency_sha256"`
	StartedAt             time.Time                   `json:"started_at"`
	OutboundHealth        map[string]scheduler.Health `json:"outbound_health"`
	HealthObservedAt      time.Time                   `json:"health_observed_at"`
	SeenCounterSources    []string                    `json:"seen_counter_sources"`
	LastSampleAt          time.Time                   `json:"last_sample_at"`
	Logs                  []LogCheckpoint             `json:"logs"`
	GateFree              bool                        `json:"gate_free"`
	ExitObservedAt        time.Time                   `json:"exit_observed_at"`
	TailStatus            string                      `json:"tail_status"`
}

func CoreRuntimePath(directory string) string {
	return filepath.Join(directory, "core-runtime.json.enc")
}
func HandoverSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func CoreRunSHA256(run CoreRun) (string, error) {
	if err := validateCoreRun(&run); err != nil {
		return "", err
	}
	data, err := json.Marshal(run)
	if err != nil {
		return "", err
	}
	return HandoverSHA256(data), nil
}

// RuntimeMaterialSHA256 hashes canonical plaintext, so re-encryption with a new
// nonce does not change the authenticated receipt's material identity.
func RuntimeMaterialSHA256(material RuntimeMaterial) (string, error) {
	if err := material.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(material)
	if err != nil {
		return "", err
	}
	defer clear(data)
	return HandoverSHA256(data), nil
}

func validSHA256(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && value == hex.EncodeToString(data)
}

func (m RuntimeMaterial) Validate() error {
	digest, err := CoreRunSHA256(m.Run)
	if err != nil {
		return errors.Join(ErrHandoverInvalid, err)
	}
	if m.Format != CoreRuntimeFormat || m.Run.Version != CoreRunMarkerVersion || m.RunSHA256 != digest || m.RenderMetadataVersion < 1 || !validSHA256(m.ConfigSHA256) || m.ConfigSHA256 != HandoverSHA256(m.Rendered.Data) || len(m.Rendered.Data) == 0 || m.Rendered.NodeID != m.Run.NodeID || m.Rendered.Revision != m.Run.Revision || len(m.Rendered.Cores) != 0 || m.ExecutablePath == "" || !validSHA256(m.ExecutableSHA256) || m.ExecutableVersion == "" {
		return ErrHandoverInvalid
	}
	switch m.State {
	case RuntimePrepared:
	case RuntimeActive, RuntimeExitObserved:
		if m.Run.Process.PID <= 0 || !completeEnvironment(m.Run.Environment) || m.StartedAt.IsZero() {
			return ErrHandoverInvalid
		}
	default:
		return ErrHandoverInvalid
	}
	if m.State == RuntimeExitObserved {
		if m.ExitObservedAt.IsZero() || m.TailStatus != "unavailable" {
			return ErrHandoverInvalid
		}
	} else if !m.ExitObservedAt.IsZero() || m.TailStatus != "" {
		return ErrHandoverInvalid
	}
	if m.Rendered.AccessContext != nil && (m.Rendered.AccessContext.NodeID != m.Run.NodeID || m.Rendered.AccessContext.Revision != m.Run.Revision) {
		return ErrHandoverInvalid
	}
	if len(m.OutboundHealth) != 0 && m.HealthObservedAt.IsZero() {
		return ErrHandoverInvalid
	}
	for name, health := range m.OutboundHealth {
		if name == "" || health.CheckedAt.IsZero() {
			return ErrHandoverInvalid
		}
	}
	ports := map[int]bool{}
	purposes := map[string]bool{}
	for _, port := range m.PrivatePorts {
		if port.Port < 1 || port.Port > 65535 || port.Purpose == "" || ports[port.Port] || purposes[port.Purpose] {
			return ErrHandoverInvalid
		}
		ports[port.Port], purposes[port.Purpose] = true, true
	}
	if len(m.Rendered.PrivatePorts) != len(ports) {
		return ErrHandoverInvalid
	}
	seenPorts := map[int]bool{}
	for _, port := range m.Rendered.PrivatePorts {
		if !ports[port] || seenPorts[port] {
			return ErrHandoverInvalid
		}
		seenPorts[port] = true
	}
	for name, port := range m.ProbePorts {
		if name == "" || !ports[port] {
			return ErrHandoverInvalid
		}
	}
	for path, digest := range m.DependencySHA256 {
		if path == "" || !validSHA256(digest) {
			return ErrHandoverInvalid
		}
	}
	seen := map[string]bool{}
	for _, source := range m.SeenCounterSources {
		if source == "" || seen[source] || m.LastSampleAt.IsZero() {
			return ErrHandoverInvalid
		}
		seen[source] = true
	}
	return validateLogCheckpoints(m.Logs)
}

func validateLogCheckpoints(logs []LogCheckpoint) error {
	seen := map[string]bool{}
	for _, log := range logs {
		if log.Path == "" || !filepath.IsAbs(log.Path) || log.Inode == 0 || log.Offset < 0 || seen[log.Path] {
			return ErrHandoverInvalid
		}
		seen[log.Path] = true
	}
	return nil
}

func runtimeKey(directory string) (*vault.Vault, error) {
	data, err := readAccountingMaterial(filepath.Join(directory, "accounting.key"), 32, true)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	return vault.FromKey(data)
}

// SaveRuntimeMaterial requires the original accounting.key and exact durable
// marker. It never creates/rekeys a missing key or fills in missing facts.
func SaveRuntimeMaterial(directory string, material RuntimeMaterial) error {
	if err := material.Validate(); err != nil {
		return err
	}
	run, _, err := LoadCoreRun(directory)
	if err != nil {
		return errors.Join(ErrHandoverInvalid, err)
	}
	// Prepared material precedes Claim; an existing marker must still match.
	if run != nil && *run != material.Run || run == nil && material.State != RuntimePrepared {
		return ErrHandoverConflict
	}
	key, err := runtimeKey(directory)
	if err != nil {
		return errors.Join(ErrHandoverInvalid, err)
	}
	var previous RuntimeMaterial
	err = loadEncryptedMaterial(CoreRuntimePath(directory), key, &previous)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := previous.Validate(); err != nil {
			return err
		}
		expected := previous.Run
		if previous.State == RuntimePrepared && expected.Process.PID == 0 {
			expected.Process = material.Run.Process
		}
		if expected != material.Run || previous.State != RuntimePrepared && material.State == RuntimePrepared || previous.State == RuntimeExitObserved && material.State != RuntimeExitObserved {
			return ErrHandoverConflict
		}
	}
	if material.State == RuntimeExitObserved && ResolveCoreRun(&material.Run, ObserveOSEnvironment()) != ProcessExited {
		return ErrCoreStopUnconfirmed
	}
	return saveEncryptedMaterial(CoreRuntimePath(directory), key, material)
}

func LoadRuntimeMaterial(directory string) (*RuntimeMaterial, error) {
	return loadRuntimeMaterial(directory, false)
}

// LoadRuntimeMaterialForStop also accepts the authenticated launch window in
// which Bind has saved the process identity but activation has not saved it yet.
// Such material is never eligible for adoption.
func LoadRuntimeMaterialForStop(directory string) (*RuntimeMaterial, error) {
	return loadRuntimeMaterial(directory, true)
}

func runtimeMaterialMatchesRun(material RuntimeMaterial, run CoreRun, stopping bool) bool {
	expected := material.Run
	if stopping && material.State == RuntimePrepared && expected.Process.PID == 0 && run.Process.PID > 0 {
		expected.Process = run.Process
	}
	return expected == run
}

func loadRuntimeMaterial(directory string, stopping bool) (*RuntimeMaterial, error) {
	key, err := runtimeKey(directory)
	if err != nil {
		return nil, errors.Join(ErrHandoverInvalid, err)
	}
	var material RuntimeMaterial
	if err := loadEncryptedMaterial(CoreRuntimePath(directory), key, &material); err != nil {
		return nil, err
	}
	if err := material.Validate(); err != nil {
		return nil, err
	}
	run, _, err := LoadCoreRun(directory)
	if err != nil {
		return nil, errors.Join(ErrHandoverInvalid, err)
	}
	if run == nil || !runtimeMaterialMatchesRun(material, *run, stopping) {
		return nil, ErrHandoverConflict
	}
	return &material, nil
}

// RemoveRuntimeMaterial is an exit-only cleanup; expected binds the deletion
// to the original run even when its marker has already been removed.
func RemoveRuntimeMaterial(directory string, expected CoreRun) error {
	if ResolveCoreRun(&expected, ObserveOSEnvironment()) != ProcessExited {
		return ErrCoreStopUnconfirmed
	}
	key, err := runtimeKey(directory)
	if err != nil {
		return err
	}
	var material RuntimeMaterial
	if err := loadEncryptedMaterial(CoreRuntimePath(directory), key, &material); err != nil {
		return err
	}
	if err := material.Validate(); err != nil {
		return err
	}
	if !runtimeMaterialMatchesRun(material, expected, true) {
		return ErrHandoverConflict
	}
	return removeHandoverMaterial(CoreRuntimePath(directory))
}

// strictHandoverJSON rejects unknown fields, duplicate object members and
// trailing values, including duplicates nested inside map-valued metadata.
func strictHandoverJSON(data []byte, value any) error {
	scanner := json.NewDecoder(bytes.NewReader(data))
	var scan func() error
	scan = func() error {
		token, err := scanner.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for scanner.More() {
				token, err := scanner.Token()
				if err != nil {
					return err
				}
				name, ok := token.(string)
				if !ok || seen[name] {
					return errors.New("duplicate or invalid JSON member")
				}
				seen[name] = true
				if err := scan(); err != nil {
					return err
				}
			}
		case '[':
			for scanner.More() {
				if err := scan(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = scanner.Token()
		return err
	}
	if err := scan(); err != nil {
		return errors.Join(ErrHandoverInvalid, err)
	}
	if _, err := scanner.Token(); err != io.EOF {
		return errors.Join(ErrHandoverInvalid, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.Join(ErrHandoverInvalid, err)
	}
	return nil
}

func loadEncryptedMaterial(path string, key *vault.Vault, value any) error {
	if key == nil {
		return ErrHandoverInvalid
	}
	if err := checkHandoverDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err := checkMaterialMode(path, 0600); err != nil {
		return err
	}
	sealed, err := readAccountingMaterial(path, MaxHandoverMaterialBytes, false)
	if err != nil {
		return errors.Join(ErrHandoverInvalid, err)
	}
	defer clear(sealed)
	plain, err := key.Open(sealed)
	if err != nil {
		return errors.Join(ErrHandoverInvalid, err)
	}
	defer clear(plain)
	return strictHandoverJSON(plain, value)
}

func saveEncryptedMaterial(path string, key *vault.Vault, value any) error {
	if key == nil {
		return ErrHandoverInvalid
	}
	plain, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer clear(plain)
	sealed, err := key.Seal(plain)
	if err != nil {
		return err
	}
	defer clear(sealed)
	uid, gid, err := handoverFileOwner(filepath.Dir(path))
	if err != nil {
		return err
	}
	return writeHandoverMaterial(path, sealed, 0600, uid, gid)
}

func checkHandoverDirectory(directory string) error {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.Join(ErrHandoverInvalid, errors.New("handover directory is not an original directory"))
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

func checkMaterialMode(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		return errors.Join(ErrHandoverInvalid, errors.New("invalid handover file type or permissions"))
	}
	return nil
}

// Callers serialize with the existing lifecycle lock. Existing material is
// checked through the same bounded, single-link reader before replacement.
func writeHandoverMaterial(path string, data []byte, mode os.FileMode, uid, gid int) (resultErr error) {
	if len(data) == 0 || len(data) > MaxHandoverMaterialBytes {
		return fmt.Errorf("%w: material size", ErrHandoverInvalid)
	}
	directory := filepath.Dir(path)
	if err := checkHandoverDirectory(directory); err != nil {
		return err
	}
	if old, err := readAccountingMaterial(path, MaxHandoverMaterialBytes, false); err == nil {
		clear(old)
		if err := checkMaterialMode(path, mode); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.CreateTemp(directory, ".handover-*")
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if uid >= 0 {
		if err := file.Chown(uid, gid); err != nil {
			return err
		}
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	err = file.Close()
	closed = true
	if err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	return syncAccountingDirectory(directory)
}

func removeHandoverMaterial(path string) error {
	if err := checkHandoverDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := readAccountingMaterial(path, MaxHandoverMaterialBytes, false)
	if err != nil {
		return err
	}
	clear(data)
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncAccountingDirectory(filepath.Dir(path))
}
