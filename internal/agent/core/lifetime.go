package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/vault"
)

// ErrCoreLifecycleUnconfirmed is shared by both cores. It reports that a
// previously started child core's exit could not be proven by this parent
// process, so the data plane must stay closed even when the user budget is
// absent. It is an exit-observation refusal, never a settlement.
var ErrCoreLifecycleUnconfirmed = errors.New("previous core process exit is unconfirmed; refusing to open the data plane")

// MaxCoreRunBytes bounds the durable lifetime marker. The marker holds no
// consumption, no budget and no credential; it only binds one authenticated
// child process identity to the node/core/epoch that started it.
const MaxCoreRunBytes = 4096

const coreRunName = "core-run.json.enc"

func CoreRunPath(directory string) string { return filepath.Join(directory, coreRunName) }

// CoreRunObservation is the durable answer to "must the data plane stay closed".
// CoreRunAbsent means no unresolved marker (absent, or bound to this live child).
// CoreRunExited means the recorded child's exit is proven and directory cleanup is
// still pending. CoreRunBlocked means the marker cannot be proven resolved.
type CoreRunObservation int

const (
	CoreRunAbsent CoreRunObservation = iota
	CoreRunExited
	CoreRunBlocked
)

// CoreRunMarkerVersion is the marker content version this build writes. Version
// 1 markers carry no OS environment; version 2 markers bind the host, boot and
// PID namespace the child was observed in. A reader that predates the version
// field rejects a version 2 marker (DisallowUnknownFields), so it can never
// silently misread the newer exit interpretation.
const (
	coreRunMarkerVersionV1 = 1
	CoreRunMarkerVersion   = 2
)

// ProcessIdentity is the durable, typed identity of one child core process. A
// zero PID or empty Token is deliberately unverifiable, so a crash between the
// pre-start marker and the post-start binding can never be read as a clean exit.
type ProcessIdentity struct {
	PID   int    `json:"pid"`
	Token string `json:"token"`
}

// CoreRun is separate from AccountingRun: consumption settlement and process
// exit observation stay independent. It is written at every core start even when
// no user budget exists, so a fresh Agent/Worker cannot open the data plane
// without a provable answer for the previously recorded child.
type CoreRun struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	NodeID      string          `json:"node_id"`
	Core        string          `json:"core"`
	Epoch       string          `json:"epoch"`
	Revision    int64           `json:"revision"`
	Environment OSEnvironment   `json:"environment"`
	Process     ProcessIdentity `json:"process"`
}

func validateCoreRun(run *CoreRun) error {
	if run.Kind != "core-run-marker-v1" || run.NodeID == "" || len(run.NodeID) > 128 || run.Epoch == "" || len(run.Epoch) > 128 || run.Revision < 1 || run.Process.Token == "" || len(run.Process.Token) > 128 {
		return errors.New("invalid core run marker")
	}
	// A marker written by a newer build than this one carries fields this reader
	// cannot interpret; refuse rather than guess the exit interpretation.
	if run.Version != coreRunMarkerVersionV1 && run.Version != CoreRunMarkerVersion {
		return errors.New("unsupported core run marker version")
	}
	if len(run.Environment.HostID) > 256 || len(run.Environment.BootID) > 256 || len(run.Environment.NamespaceID) > 256 {
		return errors.New("invalid core run marker environment")
	}
	// The core identity is a fixed, closed set. A marker that names another core
	// is still exit material for the recorded child, never a marker this adapter
	// may silently overwrite because the name differs.
	if run.Core != "xray" && run.Core != "sing-box" {
		return errors.New("invalid core run marker core identity")
	}
	if run.Process.PID < 0 {
		return errors.New("invalid core run process identity")
	}
	return nil
}

// ResolveCoreRun answers whether the recorded child, observed from the current
// OS environment, is proven exited, still alive, or unresolved. It never
// signals or kills a process and never treats an unknown fact as an exit.
//
//   - A marker that predates the environment ABI (version 0/1, or a legacy
//     marker with an empty environment) carries no host/boot/namespace fact, so
//     a fresh parent can never resolve it. Its original bytes stay; the owning
//     parent that holds and waits on the child may still clear it under its own
//     observation.
//   - Version 2 requires a full, well-formed host/boot/namespace fact on both
//     the recorded and current side. A missing or malformed fact is unresolved.
//   - A different host is never probed: the recorded child may run there, so the
//     answer is unresolved.
//   - The same host on a different boot proves the original boot ended, so the
//     recorded child is proven exited even when no process identity was bound.
//   - A different namespace on the same boot cannot be observed from here, so
//     the answer is unresolved.
//   - Only a same-host, same-boot, same-namespace marker is probed by its bound
//     process identity; a PID 0 start-window crash stays unresolved.
func ResolveCoreRun(run *CoreRun, current OSEnvironment) ProcessState {
	if run == nil {
		return ProcessUnknown
	}
	if run.Version != CoreRunMarkerVersion {
		return ProcessUnknown
	}
	recorded := run.Environment
	if !completeEnvironment(recorded) || !completeEnvironment(current) {
		return ProcessUnknown
	}
	if recorded.HostID != current.HostID {
		return ProcessUnknown
	}
	if recorded.BootID != current.BootID {
		return ProcessExited
	}
	if recorded.NamespaceID != current.NamespaceID {
		return ProcessUnknown
	}
	if run.Process.PID <= 0 || run.Process.Token == "" {
		return ProcessUnknown
	}
	return ClassifyRecordedProcess(run.Process.PID, run.Process.Token)
}

// LoadCoreRun never creates key material for an unresolved marker. Absence is a
// real absence; damaged or unauthenticated material is an error, not a clean run.
func LoadCoreRun(directory string) (*CoreRun, *vault.Vault, error) {
	data, err := readAccountingMaterial(CoreRunPath(directory), MaxCoreRunBytes, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, errors.New("core run marker unavailable")
	}
	keepData := false
	defer func() {
		if !keepData {
			clear(data)
		}
	}()
	keyData, err := readAccountingMaterial(filepath.Join(directory, "accounting.key"), 32, true)
	if err != nil {
		return nil, nil, errors.New("core run marker key unavailable; restore the original key")
	}
	defer clear(keyData)
	key, err := vault.FromKey(keyData)
	if err != nil {
		return nil, nil, err
	}
	plaintext, err := key.Open(data)
	if err != nil {
		return nil, nil, errors.New("core run marker authentication failed")
	}
	defer clear(plaintext)
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var run CoreRun
	if err := decoder.Decode(&run); err != nil {
		return nil, nil, errors.New("invalid core run marker")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, nil, errors.New("invalid core run marker")
	}
	// A marker written before the version field existed carries version 0. Keep
	// its original bytes and interpret it as version 1; the empty environment is
	// never fabricated into a boot/host fact.
	if run.Version == 0 {
		run.Version = coreRunMarkerVersionV1
	}
	if err := validateCoreRun(&run); err != nil {
		return nil, nil, err
	}
	keepData = true
	return &run, key, nil
}

func SaveCoreRun(directory string, key *vault.Vault, run CoreRun) error {
	if key == nil {
		return errors.New("original core run marker key unavailable")
	}
	// The writer always records the current marker version; callers never choose
	// a version, so a v1 code path cannot silently re-emit the new format.
	run.Version = CoreRunMarkerVersion
	if err := validateCoreRun(&run); err != nil {
		return err
	}
	data, err := json.Marshal(run)
	if err != nil {
		return err
	}
	defer clear(data)
	if len(data) > MaxCoreRunBytes-64 {
		return errors.New("core run marker exceeds size limit")
	}
	sealed, err := key.Seal(data)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".core-run-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(sealed); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), CoreRunPath(directory)); err != nil {
		return err
	}
	// A synced temporary inode alone cannot preserve the marker directory entry
	// after power loss. Failure must prevent starting the core.
	return syncAccountingDirectory(directory)
}

// RemoveCoreRun is only called after the exact recorded child's exit was
// observed by the owning parent. It never writes a settlement.
func RemoveCoreRun(directory string) error {
	err := os.Remove(CoreRunPath(directory))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncAccountingDirectory(directory)
}

// AccountingKeyForSave returns the original encryption key used for the core-run
// marker. A new key is created only for a genuinely fresh node directory that
// holds no protected core-run marker. A missing key beside existing protected
// material is an error: the marker must never be silently re-keyed.
func AccountingKeyForSave(directory string) (*vault.Vault, error) {
	keyData, err := readAccountingMaterial(filepath.Join(directory, "accounting.key"), 32, true)
	if err == nil {
		defer clear(keyData)
		return vault.FromKey(keyData)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("existing core run key unavailable; restore the original key")
	}
	if _, err := os.Lstat(CoreRunPath(directory)); err == nil {
		return nil, errors.New("core run key missing while a core run marker exists; restore the original key")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("core run material could not be inspected")
	}
	return vault.Open(filepath.Join(directory, "accounting.key"))
}
