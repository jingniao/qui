package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ErrRuntimeRestore reports a retained revision that decodes and verifies but
// cannot be started with the installed core, for example a sing-box revision
// with Hysteria2 inbounds after the node switched to Xray. The revision is
// intact, so the caller keeps the Agent alive with the core stopped and waits
// for a revision the installed core can run instead of crash-looping.
var ErrRuntimeRestore = errors.New("retained revision could not be started with the installed core")

type Manifest struct {
	Revision        int64   `json:"revision"`
	Digest          string  `json:"digest"`
	Successful      []int64 `json:"successful"`
	SuccessReceipts []int64 `json:"success_receipts,omitempty"`
}
type Receipt struct {
	Digest string           `json:"digest"`
	Result core.ApplyResult `json:"result"`
}
type Reconciler struct {
	Directory, NodeID string
	Adapter           core.Adapter
	PrepareStorage    func(context.Context, desiredstate.State) error
	NetworkAvailable  networkAvailable
	// HostNetworkDirectory is the helper spool directory of the server-wide
	// enforced source. An empty value, or a directory that does not exist, means
	// the helper is not installed, which is the normal state of an install that
	// predates the feature.
	HostNetworkDirectory string
	// ControlAddress is the panel control address the enforced-source proof dials.
	ControlAddress string
	// HostNetworkSync replaces the whole helper interaction. It is a test seam;
	// nil selects syncHostNetwork.
	HostNetworkSync        func(ctx context.Context, network desiredstate.Network) error
	RequireSuccessReceipts bool
	mu                     sync.Mutex
	manifest               Manifest
	vault                  *vault.Vault
	renderedDigest         string
	coreUpdateHold         bool
	coreUpdateHoldInvalid  bool
	controlOnly            bool
}

func New(directory, nodeID string, adapter core.Adapter) (*Reconciler, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	manifest, err := readCurrentManifest(directory)
	if err != nil {
		return nil, err
	}
	var configurationVault *vault.Vault
	if manifest == nil {
		configurationVault, err = vault.Open(filepath.Join(directory, "configuration.key"))
	} else {
		key, keyErr := readInspectionFile(filepath.Join(directory, "configuration.key"), 32)
		if keyErr != nil {
			return nil, keyErr
		}
		configurationVault, err = vault.FromKey(key)
		clear(key)
	}
	if err != nil {
		return nil, err
	}
	result := &Reconciler{Directory: directory, NodeID: nodeID, Adapter: adapter, vault: configurationVault}
	if manifest != nil {
		result.manifest = *manifest
	}
	result.loadReleaseHold()
	return result, nil
}

// NewControlOnly is for a worker whose supervisor has retained an unresolved
// Agent recovery. This instance cannot activate the proxy core; resuming data
// service requires a newly authenticated worker, not a remote unhold command.
func NewControlOnly(directory, nodeID string, adapter core.Adapter) (*Reconciler, error) {
	r, err := New(directory, nodeID, adapter)
	if err != nil {
		return nil, err
	}
	r.controlOnly = true
	return r, nil
}
func digest(data []byte) string      { hash := sha256.Sum256(data); return hex.EncodeToString(hash[:]) }
func (r *Reconciler) Current() int64 { r.mu.Lock(); defer r.mu.Unlock(); return r.manifest.Revision }
func (r *Reconciler) Apply(ctx context.Context, state desiredstate.State) (core.ApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := core.ApplyResult{Revision: state.Revision}
	if r.coreUpdateHold || r.controlOnly {
		return result, core.ErrReleaseRecovery
	}
	if state.NodeID != r.NodeID {
		return result, errors.New("DesiredState belongs to another node")
	}
	if err := state.Validate(); err != nil {
		return result, err
	}
	if err := r.verifyRequiredSuccessReceipt(r.manifest); err != nil {
		return result, err
	}
	desired, err := json.Marshal(state)
	if err != nil {
		return result, err
	}
	hash := digest(desired)
	revisionDir := filepath.Join(r.Directory, "revisions", strconv.FormatInt(state.Revision, 10))
	if data, err := os.ReadFile(filepath.Join(revisionDir, "result.json")); err == nil {
		var receipt Receipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return result, err
		}
		if receipt.Digest != hash {
			return result, errors.New("immutable revision changed")
		}
		if !receipt.Result.Success {
			return receipt.Result, errors.New(receipt.Result.Code)
		}
	}
	if state.Revision == r.manifest.Revision {
		if hash != r.manifest.Digest {
			return result, errors.New("immutable revision changed")
		}
		original, err := r.readSuccessReceipt(state.Revision, hash)
		if err != nil {
			return result, err
		}
		if original == nil && r.manifest.requiresSuccessReceipt(state.Revision) {
			return result, errors.New("required original successful apply receipt is missing")
		}
		health, err := r.Adapter.Health(ctx)
		if err == nil && health.Healthy {
			if original != nil {
				return *original, nil
			}
			return core.ApplyResult{Revision: state.Revision, Success: true, Code: "success", Message: "revision already applied"}, nil
		}
	}
	if state.Revision < r.manifest.Revision {
		return result, errors.New("older revision requires explicit rollback")
	}
	if state.Revision != r.manifest.Revision {
		if _, err := os.Lstat(r.successReceiptPath(state.Revision)); err == nil {
			return result, errors.New("retained success without matching committed manifest requires recovery")
		} else if !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
	}
	// A fresh installation has no current.json, and startup refuses retained
	// revisions without one. Commit the empty manifest before the first
	// revision directory can appear, so a failed first apply stays restartable.
	if err := r.ensureCurrentManifest(); err != nil {
		return result, err
	}
	// The outbound source address and interface are node-local facts, not
	// syntax: they are checked before rendering, and a missing address or
	// interface is a temporary failure that the next attempt retries.
	if state.Network != nil {
		available := r.NetworkAvailable
		if available == nil {
			available = verifyNetworkAvailable
		}
		if err := available(*state.Network); err != nil {
			result.Code = networkUnavailableCode
			result.Message = err.Error()
			return result, err
		}
	}
	// Re-applying the retained revision is the one case where a render or
	// validation failure must not be cached: the revision directory is the only
	// copy of the configuration that can still be restored, and the installed
	// core may simply be unable to render it (for example sing-box to Xray). It
	// is reported without touching disk so the next revision still applies.
	retainedRevision := state.Revision == r.manifest.Revision
	rendered, err := r.Adapter.Render(ctx, state)
	if err != nil {
		result.Code = "render_failed"
		result.Message = err.Error()
		if !retainedRevision {
			if saveErr := recordPermanentFailure(revisionDir, hash, result); saveErr != nil {
				return result, saveErr
			}
		}
		return result, err
	}
	if err := r.Adapter.Validate(ctx, rendered); err != nil {
		result.Code = "validation_failed"
		result.Message = err.Error()
		if !retainedRevision {
			if saveErr := recordPermanentFailure(revisionDir, hash, result); saveErr != nil {
				return result, saveErr
			}
		}
		return result, err
	}
	if r.PrepareStorage != nil {
		if err := r.PrepareStorage(ctx, state); err != nil {
			return result, err
		}
	}
	if err := os.MkdirAll(revisionDir, 0700); err != nil {
		return result, err
	}
	if existing, err := r.readRevision(state.Revision); err == nil && digest(existing) != hash {
		return result, errors.New("saved revision is immutable")
	}
	sealedDesired, err := r.vault.Seal(desired)
	if err != nil {
		return result, err
	}
	if err := writeFile(filepath.Join(revisionDir, "desired.json.enc"), sealedDesired); err != nil {
		return result, err
	}
	sealedCore, err := r.vault.Seal(rendered.Data)
	if err != nil {
		return result, err
	}
	if err := writeFile(filepath.Join(revisionDir, "core.json.enc"), sealedCore); err != nil {
		return result, err
	}
	result, err = r.Adapter.Apply(ctx, rendered)
	if err != nil {
		// The retained revision is the only copy a later startup can restore,
		// so re-applying it reports the failure without touching its directory:
		// a removed directory, or one holding only a failure receipt, would turn
		// the next Restore into a fatal read error and bring the crash loop back.
		if retainedRevision {
			return result, err
		}
		if permanentFailure(result.Code) {
			if saveErr := recordPermanentFailure(revisionDir, hash, result); saveErr != nil {
				return result, saveErr
			}
			return result, err
		}
		// A stop, start, health or rollback failure may succeed on retry, so it
		// is not cached. Remove the whole revision directory: its sealed desired
		// and core snapshots must not linger after a failed apply.
		if removeErr := os.RemoveAll(revisionDir); removeErr != nil {
			return result, removeErr
		}
		return result, err
	}
	// The server-wide enforced source must mirror the revision that is about to
	// be committed, so the helper is driven before the manifest: a failure
	// restores the previous runtime and the previous rules on the host and is
	// retried like any other temporary failure.
	if err := r.applyHostNetwork(ctx, state.Network); err != nil {
		result.Success = false
		result.Code = hostNetworkFailedCode
		result.Message = err.Error()
		if r.manifest.Revision > 0 {
			if rollbackErr := r.restoreRuntimeLocked(ctx, r.manifest.Revision); rollbackErr != nil {
				result.Code = "rollback_failed"
				return result, errors.Join(err, rollbackErr)
			}
			result.RolledBack = true
		}
		// The retained revision keeps its directory here too, for the same
		// reason as a failed apply above.
		if !retainedRevision {
			if removeErr := os.RemoveAll(revisionDir); removeErr != nil {
				return result, errors.Join(err, removeErr)
			}
		}
		return result, err
	}
	previous := r.manifest
	successful := append([]int64(nil), previous.Successful...)
	exists := false
	for _, id := range successful {
		if id == state.Revision {
			exists = true
		}
	}
	if !exists {
		successful = append(successful, state.Revision)
	}
	sort.Slice(successful, func(i, j int) bool { return successful[i] > successful[j] })
	if len(successful) > 5 {
		successful = successful[:5]
	}
	next := Manifest{Revision: state.Revision, Digest: hash, Successful: successful}
	for _, revision := range successful {
		if previous.requiresSuccessReceipt(revision) || revision == state.Revision && r.RequireSuccessReceipts {
			next.SuccessReceipts = append(next.SuccessReceipts, revision)
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return result, err
	}
	// Persist the original successful apply receipt before committing the new
	// successful manifest. Replayed commands must return the original result
	// after restart.
	err = r.writeSuccessReceipt(hash, result)
	if err == nil {
		err = writeFile(filepath.Join(r.Directory, "current.json"), data)
	}
	if err != nil {
		result.Success = false
		result.Code = "persistence_failed"
		result.Message = "could not persist successful revision"
		if previous.Revision > 0 {
			rollbackErr := r.restoreRevision(ctx, previous.Revision)
			if rollbackErr != nil {
				result.Code = "rollback_failed"
				return result, fmt.Errorf("persist successful revision and rollback failed")
			}
			result.Success = false
			result.RolledBack = true
		}
		receipt, encodeErr := json.Marshal(Receipt{Digest: hash, Result: result})
		if encodeErr != nil {
			return result, encodeErr
		}
		if saveErr := writeFile(filepath.Join(revisionDir, "result.json"), receipt); saveErr != nil {
			return result, errors.New("failed to persist application failure result")
		}
		return result, errors.New("could not persist successful revision")
	}
	r.manifest = next
	r.renderedDigest = digest(rendered.Data)
	retained := map[int64]bool{}
	for _, id := range next.Successful {
		retained[id] = true
	}
	for _, id := range previous.Successful {
		if !retained[id] {
			path := filepath.Join(r.Directory, "revisions", strconv.FormatInt(id, 10))
			if err := os.RemoveAll(path); err != nil {
				return result, errors.New("applied configuration but could not prune old revision")
			}
		}
	}
	return result, nil
}

// permanentFailure reports whether a failed apply is cached. A rendered or
// validated configuration will not change when the same revision is sent again,
// so it is permanent; stop, start, health and rollback failures are temporary
// and are retried.
func permanentFailure(code string) bool {
	return code == "render_failed" || code == "validation_failed"
}

// recordPermanentFailure stores a permanent failure receipt. The revision
// directory is left holding only result.json: any sealed desired or core
// snapshot from an earlier attempt is removed so no secret lingers, and a
// re-sent revision is answered from the cache without re-running.
func recordPermanentFailure(revisionDir, hash string, result core.ApplyResult) error {
	if err := os.RemoveAll(revisionDir); err != nil {
		return err
	}
	if err := os.MkdirAll(revisionDir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(Receipt{Digest: hash, Result: result})
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(revisionDir, "result.json"), data)
}

// restoreRevision starts a saved revision again. Integrity failures stay plain
// (they are fatal for the Agent); only rendering, starting and host-network
// failures are marked as ErrRuntimeRestore, because a core the operator
// installed cannot run them while the saved state itself is still sound.
func (r *Reconciler) restoreRevision(ctx context.Context, revision int64) error {
	data, err := r.readRevision(revision)
	if err != nil {
		return err
	}
	state, err := desiredstate.Decode(data)
	if err != nil {
		return err
	}
	if state.NodeID != r.NodeID || state.Revision != revision {
		return errors.New("saved revision identity mismatch")
	}
	retained := r.manifest
	retained.Revision, retained.Digest = revision, digest(data)
	if err := r.verifyRequiredSuccessReceipt(retained); err != nil {
		return err
	}
	rendered, err := r.Adapter.Render(ctx, state)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRuntimeRestore, err)
	}
	if _, err := r.Adapter.Apply(ctx, rendered); err != nil {
		return fmt.Errorf("%w: %w", ErrRuntimeRestore, err)
	}
	r.renderedDigest = digest(rendered.Data)
	// A failure rollback, a manual rollback and a startup restore all end here,
	// so the helper is driven to the restored revision as well.
	if err := r.applyHostNetwork(ctx, state.Network); err != nil {
		return fmt.Errorf("%w: %w", ErrRuntimeRestore, err)
	}
	return nil
}
func (r *Reconciler) Restore(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.coreUpdateHold || r.controlOnly {
		return core.ErrReleaseRecovery
	}
	if r.manifest.Revision == 0 {
		// Nothing is retained, so nothing may stay enforced either.
		return r.applyHostNetwork(ctx, nil)
	}
	data, err := r.readRevision(r.manifest.Revision)
	if err != nil {
		return err
	}
	if digest(data) != r.manifest.Digest {
		return errors.New("current revision checksum mismatch")
	}
	return r.restoreRevision(ctx, r.manifest.Revision)
}
func (r *Reconciler) Rollback(ctx context.Context, revision int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rollbackLocked(ctx, revision)
}

func (r *Reconciler) rollbackLocked(ctx context.Context, revision int64) error {
	if r.coreUpdateHold || r.controlOnly {
		return core.ErrReleaseRecovery
	}
	allowed := false
	for _, success := range r.manifest.Successful {
		if success == revision {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("rollback target is not a retained successful revision")
	}
	data, err := r.readRevision(revision)
	if err != nil {
		return err
	}
	next := r.manifest
	next.Revision = revision
	next.Digest = digest(data)
	manifest, err := json.Marshal(next)
	if err != nil {
		return err
	}
	previous := r.manifest.Revision
	if err := r.restoreRevision(ctx, revision); err != nil {
		if restoreErr := r.restoreRuntimeLocked(ctx, previous); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	if err := writeFile(filepath.Join(r.Directory, "current.json"), manifest); err != nil {
		if restoreErr := r.restoreRuntimeLocked(ctx, previous); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	r.manifest = next
	return nil
}

func (r *Reconciler) restoreRuntimeLocked(ctx context.Context, revision int64) error {
	if revision > 0 {
		return r.restoreRevision(ctx, revision)
	}
	closer, ok := r.Adapter.(interface{ Close() error })
	if !ok {
		return errors.New("core cannot restore empty state")
	}
	if err := closer.Close(); err != nil {
		return err
	}
	// The empty state enforces nothing, so the helper clears the table.
	return r.applyHostNetwork(ctx, nil)
}
func (r *Reconciler) readRevision(revision int64) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(r.Directory, "revisions", strconv.FormatInt(revision, 10), "desired.json.enc"))
	if err != nil {
		return nil, err
	}
	return r.vault.Open(data)
}

func writeFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".revision-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	var renameErr error
	for attempt := 0; attempt < 6; attempt++ {
		renameErr = os.Rename(temporary, path)
		if renameErr == nil {
			// Flush the directory entry so a crash cannot lose the rename of
			// current.json or a revision result. No-op on Windows.
			return vault.SyncDir(filepath.Dir(path))
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return renameErr
}

func (r *Reconciler) ensureCurrentManifest() error {
	path := filepath.Join(r.Directory, "current.json")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.manifest.Revision != 0 {
		return errors.New("current manifest is missing for a committed revision")
	}
	data, err := json.Marshal(Manifest{})
	if err != nil {
		return err
	}
	return writeFile(path, data)
}
