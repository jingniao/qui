package updater

import (
	"context"
	"proxycontrol/pkg/contracts/release"
	"runtime"
)

// RecoveryProof can only be minted from the original authenticated history
// and a verified local image. Its fields cannot be supplied by a command.
type RecoveryProof struct {
	history   *History
	directory string
	state     Installation
	request   release.UpdateRequest
	baseline  *BaselineRecovery
}

func (p RecoveryProof) Authorize(ctx context.Context, nodeID string, revision int64, request release.UpdateRequest) (Installation, error) {
	if p.history == nil || p.request != request || request.Operation != "recover" || p.history.nodeID != nodeID || request.NodeID != nodeID || request.Revision != revision {
		return Installation{}, ErrInstallation
	}
	s, ok, err := p.history.Installation(p.state.Component)
	if err != nil || !ok || s.Generation != p.state.Generation || s.Phase != p.state.Phase || s.RequestID != p.state.RequestID || s.Revision != p.state.Revision || s.Current != p.state.Current || !imageEqual(s.Target, p.state.Target) || !imageEqual(s.Previous, p.state.Previous) {
		return Installation{}, ErrInstallation
	}
	if p.baseline != nil {
		r, ok, err := p.history.BaselineRecovery(request.ID)
		current, currentOK, currentErr := p.history.CurrentBaselineRecovery(s.Component)
		if err != nil || !ok || currentErr != nil || !currentOK || current.Request.ID != request.ID || r != *p.baseline || r.Baseline != s.Current || r.InstallationGeneration != s.Generation {
			return Installation{}, ErrInstallation
		}
		if _, exists, err := p.history.InstallationRequest(r.Original.ID); err != nil || exists {
			return Installation{}, ErrInstallation
		}
	}
	if _, err := ResolveImage(ctx, p.directory, s.Component, s.Current); err != nil {
		return Installation{}, err
	}
	return copyInstallation(s), nil
}

func (i *Installer) BaselineRecoveryProof(ctx context.Context, request release.UpdateRequest) (RecoveryProof, error) {
	if i.History == nil || !validRecoveryRequest(request) || request.Operation != "recover" || request.NodeID != i.History.nodeID || request.Release.Manifest.OS != runtime.GOOS || request.Release.Manifest.Architecture != runtime.GOARCH {
		return RecoveryProof{}, ErrInstallation
	}
	r, ok, err := i.History.BaselineRecovery(request.ID)
	if err != nil || !ok || r.Request != request {
		return RecoveryProof{}, ErrInstallation
	}
	s, ok, err := i.History.Installation(request.Release.Manifest.Component)
	if err != nil || !ok || pending(s) || s.Current != r.Baseline || s.Generation != r.InstallationGeneration {
		return RecoveryProof{}, ErrInstallation
	}
	p := RecoveryProof{history: i.History, directory: i.Directory, state: copyInstallation(s), request: request, baseline: &r}
	if _, err := p.Authorize(ctx, request.NodeID, request.Revision, request); err != nil {
		return RecoveryProof{}, err
	}
	return p, nil
}
