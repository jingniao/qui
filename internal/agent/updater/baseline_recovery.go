package updater

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/release"
	"runtime"
)

// BaselineRecovery records restoration after an authenticated pre-switch
// rejection. It is separate from installation; the refused target never became
// current and its original command ID can never be installed later.
type BaselineRecovery struct {
	Request                release.UpdateRequest `json:"request"`
	Original               release.UpdateRequest `json:"original"`
	Rejection              release.UpdateResult  `json:"rejection"`
	Baseline               Image                 `json:"baseline"`
	InstallationGeneration int64                 `json:"installation_generation"`
	Phase                  string                `json:"phase"`
}

func baselineEqual(a, b BaselineRecovery) bool {
	a.Phase, b.Phase = "", ""
	return a == b
}
func baselinePending(r BaselineRecovery) bool {
	return r.Phase == "prepared" || r.Phase == "failed_closed"
}
func (h *History) validateBaselineRecovery(next BaselineRecovery) error {
	r, o := next.Request, next.Original
	m := r.Release.Manifest
	if !validBaselineIntent(h.nodeID, r, o, next.Rejection) {
		return ErrInstallation
	}
	if _, exists := h.requests[o.ID]; exists {
		return ErrInstallation
	}
	s, exists := h.installations[m.Component]
	if !exists || pending(s) || s.Current != next.Baseline || s.Generation != next.InstallationGeneration {
		return ErrInstallation
	}
	previous, exists := h.baselineRecoveries[r.ID]
	if !exists {
		if _, exists := h.requests[r.ID]; exists {
			return ErrInstallation
		}
		if next.Phase != "prepared" {
			return ErrInstallation
		}
		for _, other := range h.baselineRecoveries {
			if baselinePending(other) && h.currentBaselineRecovery[other.Request.Release.Manifest.Component] == other.Request.ID && (other.Original.ID != o.ID || other.Original != o || other.Rejection != next.Rejection || other.Request.Revision != r.Revision || other.Baseline != next.Baseline) {
				return ErrUpdatePending
			}
		}
		return nil
	}
	if !baselineEqual(previous, next) || previous.Phase != "prepared" || (next.Phase != "recovered" && next.Phase != "failed_closed") {
		return ErrInstallation
	}
	return nil
}
func (h *History) BaselineRecovery(id string) (BaselineRecovery, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.diskHead(); err != nil {
		return BaselineRecovery{}, false, err
	}
	r, ok := h.baselineRecoveries[id]
	return r, ok, nil
}
func (h *History) PendingBaselineRecovery(component string) (BaselineRecovery, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.diskHead(); err != nil {
		return BaselineRecovery{}, false, err
	}
	id, ok := h.currentBaselineRecovery[component]
	if !ok {
		return BaselineRecovery{}, false, nil
	}
	r := h.baselineRecoveries[id]
	return r, baselinePending(r), nil
}
func (h *History) BeginBaselineRecovery(ctx context.Context, r BaselineRecovery) (BaselineRecovery, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return BaselineRecovery{}, err
	}
	if err := h.diskHead(); err != nil {
		return BaselineRecovery{}, err
	}
	if previous, exists := h.baselineRecoveries[r.Request.ID]; exists {
		if !baselineEqual(previous, r) {
			return BaselineRecovery{}, ErrInstallation
		}
		return previous, nil
	}
	r.Phase = "prepared"
	if err := h.validateBaselineRecovery(r); err != nil {
		return BaselineRecovery{}, err
	}
	if err := h.appendEvent(historyEvent{Kind: "release-baseline-recovery-v1", BaselineRecovery: &r}); err != nil {
		return BaselineRecovery{}, err
	}
	return r, nil
}
func (h *History) finishBaselineRecovery(ctx context.Context, id, phase string) (BaselineRecovery, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return BaselineRecovery{}, err
	}
	if err := h.diskHead(); err != nil {
		return BaselineRecovery{}, err
	}
	r, ok := h.baselineRecoveries[id]
	if !ok || h.currentBaselineRecovery[r.Request.Release.Manifest.Component] != id {
		return BaselineRecovery{}, ErrInstallation
	}
	if r.Phase == phase {
		return r, nil
	}
	r.Phase = phase
	if err := h.validateBaselineRecovery(r); err != nil {
		return r, err
	}
	if err := h.appendEvent(historyEvent{Kind: "release-baseline-recovery-v1", BaselineRecovery: &r}); err != nil {
		return r, err
	}
	return r, nil
}

func (i *Installer) PrepareBaselineRecovery(ctx context.Context, driver Driver, request, original release.UpdateRequest, rejection release.UpdateResult) (BaselineRecovery, error) {
	if i.History == nil || !validBaselineIntent(driver.NodeID(), request, original, rejection) || request.NodeID != driver.NodeID() || request.Revision != driver.Revision() || request.Release.Manifest.Component != driver.Component() {
		return BaselineRecovery{}, ErrInstallation
	}
	if _, exists, err := i.History.InstallationRequest(original.ID); err != nil || exists {
		return BaselineRecovery{}, ErrInstallation
	}
	s, exists, err := i.History.Installation(driver.Component())
	if err != nil {
		return BaselineRecovery{}, err
	}
	if exists && pending(s) {
		return BaselineRecovery{}, ErrUpdatePending
	}
	if !exists {
		path, version, err := driver.Current(ctx)
		if err != nil {
			return BaselineRecovery{}, err
		}
		image, err := InspectImage(ctx, driver.Component(), path, version)
		if err != nil {
			return BaselineRecovery{}, err
		}
		if _, err := MaterializeImage(ctx, i.Directory, path, driver.Component(), image); err != nil {
			return BaselineRecovery{}, err
		}
		if err := i.History.BootstrapImage(ctx, driver.Component(), image); err != nil {
			return BaselineRecovery{}, err
		}
		s, exists, err = i.History.Installation(driver.Component())
		if err != nil || !exists {
			return BaselineRecovery{}, ErrInstallation
		}
	}
	return i.History.BeginBaselineRecovery(ctx, BaselineRecovery{Request: request, Original: original, Rejection: rejection, Baseline: s.Current, InstallationGeneration: s.Generation, Phase: "prepared"})
}
func (i *Installer) ReconcileBaseline(ctx context.Context, driver Driver, id string) (BaselineRecovery, error) {
	r, ok, err := i.History.BaselineRecovery(id)
	if err != nil || !ok || r.Request.NodeID != driver.NodeID() || r.Request.Revision != driver.Revision() || r.Request.Release.Manifest.Component != driver.Component() {
		return r, ErrInstallation
	}
	if r.Phase == "failed_closed" {
		return r, ErrUpdatePending
	}
	proof, err := i.BaselineRecoveryProof(ctx, r.Request)
	if err != nil {
		return r, err
	}
	if _, err := proof.Authorize(ctx, driver.NodeID(), driver.Revision(), r.Request); err != nil {
		return r, err
	}
	if err := driver.Hold(); err != nil {
		return r, errors.Join(err, driver.Stop(ctx))
	}
	path, err := ResolveImage(ctx, i.Directory, driver.Component(), r.Baseline)
	if err == nil {
		err = driver.Activate(ctx, path)
	}
	if err != nil {
		stopErr := driver.Stop(ctx)
		if processStopUnconfirmed(driver) {
			return r, errors.Join(err, stopErr, ErrProcessStopUnconfirmed)
		}
		if r.Phase == "prepared" {
			_, saveErr := i.History.finishBaselineRecovery(ctx, id, "failed_closed")
			return r, errors.Join(err, stopErr, saveErr)
		}
		return r, errors.Join(err, stopErr)
	}
	if r.Phase == "prepared" {
		r, err = i.History.finishBaselineRecovery(ctx, id, "recovered")
		if err != nil {
			return r, errors.Join(err, driver.Stop(ctx))
		}
	}
	if err := driver.Release(); err != nil {
		return r, errors.Join(err, driver.Stop(ctx))
	}
	return r, nil
}

func (h *History) CurrentBaselineRecovery(component string) (BaselineRecovery, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.diskHead(); err != nil {
		return BaselineRecovery{}, false, err
	}
	id, ok := h.currentBaselineRecovery[component]
	if !ok {
		return BaselineRecovery{}, false, nil
	}
	return h.baselineRecoveries[id], true, nil
}

func validBaselineIntent(nodeID string, r, o release.UpdateRequest, rejection release.UpdateResult) bool {
	m := r.Release.Manifest
	return validRecoveryRequest(r) && validRecoveryRequest(o) && r.Operation == "recover" && o.Operation == "install" && r.NodeID == nodeID && o.NodeID == nodeID && r.OriginalID == o.ID && r.Revision == o.Revision && r.Release == o.Release && rejection.Validate() == nil && rejection.Status == "rejected" && rejection.InstalledDigest == "" && rejection.ID == o.ID && rejection.NodeID == o.NodeID && rejection.Revision == o.Revision && rejection.TargetDigest == m.SHA256 && m.OS == runtime.GOOS && m.Architecture == runtime.GOARCH
}

func validRecoveryRequest(request release.UpdateRequest) bool {
	if request.Release.Manifest.Component == "proxy-agent" {
		return release.AgentUpdateRequest(request).Validate() == nil
	}
	return request.Validate() == nil
}
