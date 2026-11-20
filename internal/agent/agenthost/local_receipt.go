package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/release"
)

var localRequestID = regexp.MustCompile(`^local-[A-Za-z0-9_-]{1,121}$`)

type LocalInstallReceipt struct {
	Request      release.AgentUpdateRequest `json:"request"`
	Result       *release.UpdateResult      `json:"result,omitempty"`
	Installation *updater.Installation      `json:"installation,omitempty"`
	Outcome      *LocalInstallationOutcome  `json:"outcome,omitempty"`
}

// LocalReceipt reads through the owner of History, without requiring a healthy
// worker or acquiring a second history lock. It never queues or recovers a job.
func LocalReceipt(ctx context.Context, configPath, id, manifestPath string) (LocalAgentStatus, error) {
	if !localRequestID.MatchString(id) {
		return LocalAgentStatus{}, errors.New("invalid local installation request ID")
	}
	c, err := configuration.LoadAgent(configPath)
	if err != nil {
		return LocalAgentStatus{}, err
	}
	data, err := readOriginal(manifestPath, release.MaxManifestBytes)
	if err != nil {
		return LocalAgentStatus{}, err
	}
	signed, err := release.Decode(data)
	if err != nil {
		return LocalAgentStatus{}, err
	}
	status, err := callLocal(ctx, c.DataDirectory, localRequest{Operation: "receipt", RequestID: id, Release: signed})
	if err == nil && (status.Receipt == nil || status.Receipt.Request.Release != signed) {
		return status, errors.New("original installation receipt signed material differs from the bound manifest")
	}
	return status, err
}

type localFailureRecord struct {
	NodeID    string                   `json:"nodeId"`
	RequestID string                   `json:"requestId"`
	Kind      string                   `json:"kind"`
	Outcome   LocalInstallationOutcome `json:"outcome"`
}

func (r *jobRunner) localFailurePath(id, kind string) string {
	return filepath.Join(localCandidates(r.directory), id+"."+kind+".enc")
}
func (r *jobRunner) readLocalFailure(id, kind string) (*LocalInstallationOutcome, error) {
	data, err := readOriginal(r.localFailurePath(id, kind), 1<<20)
	if err != nil {
		return nil, err
	}
	v, err := vault.FromKey(r.key)
	if err != nil {
		return nil, err
	}
	plain, err := v.Open(data)
	if err != nil {
		return nil, err
	}
	var record localFailureRecord
	if err = json.Unmarshal(plain, &record); err != nil {
		return nil, err
	}
	if record.NodeID != r.nodeID || record.RequestID != id || record.Kind != kind {
		return nil, ErrState
	}
	return &record.Outcome, nil
}
func (r *jobRunner) persistLocalFailure(id, kind string, outcome *LocalInstallationOutcome) error {
	if !localRequestID.MatchString(id) {
		return errors.New("invalid local failure receipt ID")
	}
	if old, err := r.readLocalFailure(id, kind); err == nil {
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(outcome)
		if string(a) != string(b) {
			return errors.New("different original local failure receipt")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data, err := json.Marshal(localFailureRecord{NodeID: r.nodeID, RequestID: id, Kind: kind, Outcome: *outcome})
	if err != nil {
		return err
	}
	v, err := vault.FromKey(r.key)
	if err != nil {
		return err
	}
	sealed, err := v.Seal(data)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(r.localFailurePath(id, kind), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(sealed)
	if err = errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	return syncDirectory(localCandidates(r.directory))
}
func (r *jobRunner) persistLocalCause(id string, cause error) error {
	if !localRequestID.MatchString(id) || cause == nil {
		return nil
	}
	return r.persistLocalFailure(id, "cause", &LocalInstallationOutcome{OriginalError: cause.Error()})
}
func (r *jobRunner) persistLocalOutcome(id string, outcome *LocalInstallationOutcome) error {
	return r.persistLocalFailure(id, "outcome", outcome)
}

func (r *jobRunner) localInstallReceipt(id string) (*LocalInstallReceipt, error) {
	if !localRequestID.MatchString(id) {
		return nil, errors.New("invalid local installation receipt ID")
	}
	store, err := agentjobs.Open(jobsOptions(r.directory, r.nodeID, r.key))
	if err != nil {
		return nil, err
	}
	job, found := store.Get(id)
	if err = store.Close(); err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("original local installation request %s has not been durably accepted", id)
	}
	if job.Request.NodeID != r.nodeID {
		return nil, ErrState
	}
	receipt := &LocalInstallReceipt{Request: job.Request, Result: job.Result}
	installation, exists, err := r.history.InstallationRequest(id)
	if err != nil {
		return nil, err
	}
	if exists {
		m := job.Request.Release.Manifest
		if installation.Component != "proxy-agent" || installation.Revision != job.Request.Revision || installation.Target == nil || installation.Target.SHA256 != m.SHA256 || installation.Target.Bytes != m.Bytes || installation.Target.Sequence != m.Sequence || installation.Target.Version != m.Version {
			return nil, ErrState
		}
		receipt.Installation = &installation
		if job.Result != nil {
			status := map[string]string{"committed": "success", "rolled_back": "rolled_back", "failed_closed": "failed_closed"}[installation.Phase]
			if status == "" || job.Result.Status != status {
				return nil, errors.New("original job result differs from its installation history")
			}
		}
	}
	for _, kind := range []string{"outcome", "cause"} {
		outcome, err := r.readLocalFailure(id, kind)
		if err == nil {
			receipt.Outcome = outcome
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if exists && (installation.Phase == "rolled_back" || installation.Phase == "failed_closed") {
		if receipt.Outcome == nil {
			receipt.Outcome = &LocalInstallationOutcome{OriginalError: "Original managed installation was interrupted before an error receipt was persisted."}
		}
		success := installation.Phase == "rolled_back"
		receipt.Outcome.Recovery.Attempted = true
		receipt.Outcome.Recovery.Success = &success
	}
	return receipt, nil
}
