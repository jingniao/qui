package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"proxycontrol/internal/platform/vault"
)

const (
	HandoverRequestPath          = "/run/proxy-agent-handover/request.json"
	HandoverWindow               = 300 * time.Second
	HandoverProtocolVersion      = 1
	CodeHandoverUnsupported      = "core_handover_unsupported"
	CodeHandoverPermission       = "core_handover_permission"
	CodeHandoverInvalid          = "core_handover_invalid"
	CodeHandoverExpired          = "core_handover_expired"
	CodeHandoverConflict         = "core_handover_conflict"
	CodeHandoverConfigChanged    = "core_handover_config_changed"
	CodeCoreExitedDuringHandover = "core_exited_during_handover"
	CodeGateRecoveryRequired     = "gate_recovery_required"
)

var (
	ErrHandoverUnsupported      = errors.New(CodeHandoverUnsupported)
	ErrHandoverPermission       = errors.New(CodeHandoverPermission)
	ErrHandoverInvalid          = errors.New(CodeHandoverInvalid)
	ErrHandoverExpired          = errors.New(CodeHandoverExpired)
	ErrHandoverConflict         = errors.New(CodeHandoverConflict)
	ErrCoreExitedDuringHandover = errors.New(CodeCoreExitedDuringHandover)
)

type HandoverState string

const (
	HandoverArmed    HandoverState = "armed"
	HandoverPrepared HandoverState = "prepared"
	HandoverDetached HandoverState = "detached"
	HandoverAdopting HandoverState = "adopting"
	HandoverAdopted  HandoverState = "adopted"
	HandoverFailed   HandoverState = "failed"
	HandoverStopped  HandoverState = "stopped"
	HandoverExpired  HandoverState = "expired"
)

type HandoverRequest struct {
	Version            int             `json:"version"`
	Kind               string          `json:"kind"`
	ID                 string          `json:"id"`
	NodeID             string          `json:"node_id"`
	BootID             string          `json:"boot_id"`
	Updater            ProcessIdentity `json:"updater"`
	SourceAgent        ProcessIdentity `json:"source_agent"`
	TargetAgentSHA256  string          `json:"target_agent_sha256"`
	TransactionID      string          `json:"transaction_id"`
	Scope              string          `json:"scope"`
	Phase              HandoverState   `json:"phase"`
	CreatedBoottimeNS  int64           `json:"created_boottime_ns"`
	DeadlineBoottimeNS int64           `json:"deadline_boottime_ns"`
}

func validBoundProcess(p ProcessIdentity) bool {
	return p.PID > 0 && p.Token != "" && len(p.Token) <= 128
}

func (r HandoverRequest) Validate() error {
	if r.Version != HandoverProtocolVersion || r.Kind != "core-handover-request" || !validSHA256(r.ID) || r.NodeID == "" || len(r.NodeID) > 128 || r.BootID == "" || len(r.BootID) > 256 || !validBoundProcess(r.Updater) || !validBoundProcess(r.SourceAgent) || !validSHA256(r.TargetAgentSHA256) || r.TransactionID == "" || len(r.TransactionID) > 256 || r.Scope != "bootstrap" && r.Scope != "worker" || r.Phase != HandoverArmed || r.CreatedBoottimeNS < 0 || r.DeadlineBoottimeNS < r.CreatedBoottimeNS || r.DeadlineBoottimeNS-r.CreatedBoottimeNS != int64(HandoverWindow) {
		return ErrHandoverInvalid
	}
	return nil
}

func HandoverRequestSHA256(request HandoverRequest) (string, error) {
	if err := request.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	return HandoverSHA256(data), nil
}

// HandoverAuthorization is supplied from local authenticated configuration and
// the active update transaction, never copied wholesale from the request.
type HandoverAuthorization struct {
	NodeID            string
	Environment       OSEnvironment
	SourceAgent       ProcessIdentity
	TargetAgentSHA256 string
	TransactionID     string
	Scope             string
}

// AuthorizeHandoverRequest checks the live updater and CLOCK_BOOTTIME each time
// permission is exercised. Reading a receipt alone never grants authorization.
func AuthorizeHandoverRequest(r HandoverRequest, expected HandoverAuthorization) error {
	if err := r.Validate(); err != nil {
		return err
	}
	current := ObserveOSEnvironment()
	// Each mismatch names the field so a refused handover is diagnosable; the
	// values are local process and transaction facts, never credentials.
	switch {
	case !completeEnvironment(current):
		return fmt.Errorf("%w: OS environment is incomplete: %+v", ErrHandoverConflict, current)
	case expected.Environment != current || r.BootID != current.BootID:
		return fmt.Errorf("%w: OS environment differs (request boot %s, expected %+v, current %+v)", ErrHandoverConflict, r.BootID, expected.Environment, current)
	case r.NodeID != expected.NodeID:
		return fmt.Errorf("%w: node differs", ErrHandoverConflict)
	case r.SourceAgent != expected.SourceAgent:
		return fmt.Errorf("%w: source Agent %+v is not %+v", ErrHandoverConflict, r.SourceAgent, expected.SourceAgent)
	case r.TargetAgentSHA256 != expected.TargetAgentSHA256:
		return fmt.Errorf("%w: target Agent %s is not %s", ErrHandoverConflict, r.TargetAgentSHA256, expected.TargetAgentSHA256)
	case r.TransactionID != expected.TransactionID:
		return fmt.Errorf("%w: transaction %s is not %s", ErrHandoverConflict, r.TransactionID, expected.TransactionID)
	case r.Scope != expected.Scope:
		return fmt.Errorf("%w: scope %s is not %s", ErrHandoverConflict, r.Scope, expected.Scope)
	}
	now, err := HandoverBoottimeNS()
	if err != nil {
		return err
	}
	if now < r.CreatedBoottimeNS || now >= r.DeadlineBoottimeNS {
		return ErrHandoverExpired
	}
	return checkHandoverUpdater(r.Updater, current)
}

type HandoverCoreReceipt struct {
	Run           CoreRun         `json:"run"`
	RuntimeSHA256 string          `json:"runtime_sha256"`
	LastSampleAt  time.Time       `json:"last_sample_at"`
	Logs          []LogCheckpoint `json:"logs"`
}

type HandoverReceipt struct {
	Version           int                   `json:"version"`
	Kind              string                `json:"kind"`
	RequestID         string                `json:"request_id"`
	RequestSHA256     string                `json:"request_sha256"`
	NodeID            string                `json:"node_id"`
	Environment       OSEnvironment         `json:"environment"`
	SourceAgent       ProcessIdentity       `json:"source_agent"`
	TargetAgent       ProcessIdentity       `json:"target_agent"`
	TargetAgentSHA256 string                `json:"target_agent_sha256"`
	TransactionID     string                `json:"transaction_id"`
	Scope             string                `json:"scope"`
	State             HandoverState         `json:"state"`
	Cores             []HandoverCoreReceipt `json:"cores"`
	FailureCode       string                `json:"failure_code"`
	// Reason contains only caller-selected safe metadata, never raw native config.
	Reason string `json:"reason"`
}

func validHandoverState(state HandoverState) bool {
	switch state {
	case HandoverArmed, HandoverPrepared, HandoverDetached, HandoverAdopting, HandoverAdopted, HandoverFailed, HandoverStopped, HandoverExpired:
		return true
	}
	return false
}

func ValidateHandoverTransition(from, to HandoverState) error {
	allowed := false
	switch from {
	case HandoverArmed:
		allowed = to == HandoverPrepared || to == HandoverFailed || to == HandoverExpired || to == HandoverStopped
	case HandoverPrepared:
		allowed = to == HandoverDetached || to == HandoverFailed || to == HandoverExpired || to == HandoverStopped
	case HandoverDetached:
		allowed = to == HandoverAdopting || to == HandoverFailed || to == HandoverExpired || to == HandoverStopped
	case HandoverAdopting:
		allowed = to == HandoverAdopting || to == HandoverAdopted || to == HandoverFailed || to == HandoverExpired || to == HandoverStopped
	case HandoverAdopted, HandoverFailed, HandoverExpired:
		allowed = to == HandoverStopped
	}
	if !allowed {
		return ErrHandoverConflict
	}
	return nil
}

func (r HandoverReceipt) Validate() error {
	if r.Version != HandoverProtocolVersion || r.Kind != "core-handover-receipt" || !validSHA256(r.RequestID) || !validSHA256(r.RequestSHA256) || r.NodeID == "" || len(r.NodeID) > 128 || !completeEnvironment(r.Environment) || !validBoundProcess(r.SourceAgent) || !validSHA256(r.TargetAgentSHA256) || r.TransactionID == "" || len(r.TransactionID) > 256 || r.Scope != "bootstrap" && r.Scope != "worker" || !validHandoverState(r.State) || len(r.Reason) > 1024 {
		return ErrHandoverInvalid
	}
	if r.TargetAgent != (ProcessIdentity{}) && !validBoundProcess(r.TargetAgent) {
		return ErrHandoverInvalid
	}
	if (r.State == HandoverAdopting || r.State == HandoverAdopted) && !validBoundProcess(r.TargetAgent) {
		return ErrHandoverInvalid
	}
	if r.State == HandoverFailed || r.State == HandoverExpired {
		if !validHandoverFailureCode(r.FailureCode) || r.Reason == "" {
			return ErrHandoverInvalid
		}
		if r.State == HandoverExpired && r.FailureCode != CodeHandoverExpired {
			return ErrHandoverInvalid
		}
	} else if r.FailureCode != "" || r.Reason != "" {
		return ErrHandoverInvalid
	}
	seen := map[string]bool{}
	for _, core := range r.Cores {
		if err := validateCoreRun(&core.Run); err != nil {
			return errors.Join(ErrHandoverInvalid, err)
		}
		if core.Run.Version != CoreRunMarkerVersion || !validBoundProcess(core.Run.Process) || core.Run.NodeID != r.NodeID || core.Run.Environment != r.Environment || !validSHA256(core.RuntimeSHA256) || core.LastSampleAt.IsZero() || seen[core.Run.Core] {
			return ErrHandoverInvalid
		}
		if err := validateLogCheckpoints(core.Logs); err != nil {
			return err
		}
		seen[core.Run.Core] = true
	}
	return nil
}

func validHandoverFailureCode(code string) bool {
	switch code {
	case CodeHandoverUnsupported, CodeHandoverPermission, CodeHandoverInvalid, CodeHandoverExpired, CodeHandoverConflict, CodeHandoverConfigChanged, CodeCoreExitedDuringHandover, CodeGateRecoveryRequired:
		return true
	}
	return false
}

func (r HandoverReceipt) MatchRequest(request HandoverRequest) error {
	if err := r.Validate(); err != nil {
		return err
	}
	digest, err := HandoverRequestSHA256(request)
	if err != nil {
		return err
	}
	if r.RequestID != request.ID || r.RequestSHA256 != digest || r.NodeID != request.NodeID || r.Environment.BootID != request.BootID || r.SourceAgent != request.SourceAgent || r.TargetAgentSHA256 != request.TargetAgentSHA256 || r.TransactionID != request.TransactionID || r.Scope != request.Scope {
		return ErrHandoverConflict
	}
	return nil
}

func LoadHandoverRequest() (*HandoverRequest, error) {
	if err := checkRequestPermissions(); err != nil {
		return nil, err
	}
	data, err := readAccountingMaterial(HandoverRequestPath, MaxHandoverMaterialBytes, false)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	var request HandoverRequest
	if err := strictHandoverJSON(data, &request); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	return &request, nil
}

// SaveHandoverRequest is root-only and refuses to replace any unresolved request.
func SaveHandoverRequest(request HandoverRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if _, err := os.Lstat(HandoverRequestPath); err == nil {
		return ErrHandoverConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	gid, err := prepareRequestDirectory()
	if err != nil {
		return err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return writeHandoverMaterial(HandoverRequestPath, data, 0640, 0, gid)
}

// RemoveHandoverRequest revokes the exact request; it never clears core markers.
func RemoveHandoverRequest(expected HandoverRequest) error {
	request, err := LoadHandoverRequest()
	if err != nil {
		return err
	}
	if *request != expected {
		return ErrHandoverConflict
	}
	return removeHandoverMaterial(HandoverRequestPath)
}

func HandoverReceiptPath(dataDirectory string) string {
	return filepath.Join(dataDirectory, "handover", "receipt.json.enc")
}

// identityKey MUST be the existing node identity vault, not accounting.key.
// Read does not enforce current authorization: expired evidence remains readable.
func LoadHandoverReceipt(dataDirectory string, identityKey *vault.Vault) (*HandoverReceipt, error) {
	var receipt HandoverReceipt
	if err := checkReceiptDirectory(filepath.Dir(HandoverReceiptPath(dataDirectory))); err != nil {
		return nil, err
	}
	if err := loadEncryptedMaterial(HandoverReceiptPath(dataDirectory), identityKey, &receipt); err != nil {
		return nil, err
	}
	if err := receipt.Validate(); err != nil {
		return nil, err
	}
	return &receipt, nil
}

// SaveHandoverReceipt validates the durable predecessor under the caller's
// lifecycle lock. The request is already armed, so the first receipt may be
// prepared (or a failure from armed). A new transaction may replace only
// stopped/adopted evidence.
func SaveHandoverReceipt(dataDirectory string, identityKey *vault.Vault, request HandoverRequest, next HandoverReceipt) error {
	if err := next.MatchRequest(request); err != nil {
		return err
	}
	directory := filepath.Dir(HandoverReceiptPath(dataDirectory))
	if err := createReceiptDirectory(directory); err != nil {
		return err
	}
	previous, err := LoadHandoverReceipt(dataDirectory, identityKey)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if previous == nil || previous.RequestID != next.RequestID {
		if previous != nil && previous.State != HandoverStopped && previous.State != HandoverAdopted {
			return ErrHandoverConflict
		}
		if next.State != HandoverArmed {
			if err := ValidateHandoverTransition(HandoverArmed, next.State); err != nil {
				return err
			}
		}
	} else {
		if err := previous.MatchRequest(request); err != nil {
			return err
		}
		if previous.Environment != next.Environment {
			return ErrHandoverConflict
		}
		if err := ValidateHandoverTransition(previous.State, next.State); err != nil {
			return err
		}
		if previous.TargetAgent != (ProcessIdentity{}) && previous.TargetAgent != next.TargetAgent {
			// A crashed adopter may be resumed by the same target image under
			// the lifecycle lock, only after the previous adopter is proven gone.
			if previous.State != HandoverAdopting || next.State != HandoverAdopting || previous.Environment != ObserveOSEnvironment() || ClassifyRecordedProcess(previous.TargetAgent.PID, previous.TargetAgent.Token) != ProcessExited {
				return ErrHandoverConflict
			}
		}
		if previous.State != HandoverArmed {
			before, err := json.Marshal(previous.Cores)
			if err != nil {
				return err
			}
			after, err := json.Marshal(next.Cores)
			if err != nil {
				return err
			}
			if string(before) != string(after) {
				return ErrHandoverConflict
			}
		}
	}
	return saveEncryptedMaterial(HandoverReceiptPath(dataDirectory), identityKey, next)
}

func checkReceiptDirectory(directory string) error {
	if err := checkHandoverDirectory(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0700 {
		return ErrHandoverInvalid
	}
	return nil
}

func createReceiptDirectory(directory string) error {
	if err := checkHandoverDirectory(filepath.Dir(directory)); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0700); err == nil {
		// The root barrier may create it first; it must stay the Agent's directory.
		uid, gid, err := handoverFileOwner(filepath.Dir(directory))
		if err != nil {
			return err
		}
		if uid >= 0 {
			if err := os.Lchown(directory, uid, gid); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := checkReceiptDirectory(directory); err != nil {
		return err
	}
	return syncAccountingDirectory(filepath.Dir(directory))
}
