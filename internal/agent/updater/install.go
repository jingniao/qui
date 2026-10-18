package updater

import (
	"context"
	"errors"
	"time"

	"proxycontrol/pkg/contracts/release"
)

// Driver is implemented by a Reconciler-held lease. It exposes fixed core
// operations only, with no remote process arguments or shell entry point.
type Driver interface {
	Component() string
	NodeID() string
	Revision() int64
	Current(context.Context) (path, version string, err error)
	Check(context.Context, string, string) error
	Activate(context.Context, string) error
	Stop(context.Context) error
	Hold() error
	Release() error
}

var ErrProcessStopUnconfirmed = errors.New("managed process exit is unconfirmed; retain the original process and do not start a replacement")

// Process drivers can report an unconfirmed stop without inventing a closed
// installation outcome. Existing core drivers retain their own stop contract.
func processStopUnconfirmed(driver Driver) bool {
	observer, ok := driver.(interface{ UnconfirmedStop() bool })
	return ok && observer.UnconfirmedStop()
}

type Installer struct {
	History   *History
	Directory string
}

func (i *Installer) Apply(ctx context.Context, driver Driver, id string, revision int64, candidate Candidate, policy release.Policy) (Installation, error) {
	if i.History == nil || i.Directory == "" || driver.NodeID() != i.History.nodeID || driver.Revision() != revision || driver.Component() != candidate.Manifest.Component || candidate.Signed.Manifest != candidate.Manifest || candidate.SHA256 != candidate.Manifest.SHA256 {
		return Installation{}, ErrInstallation
	}
	if previous, ok, err := i.History.InstallationRequest(id); err != nil {
		return Installation{}, err
	} else if ok {
		if previous.Component != driver.Component() || previous.Target == nil || *previous.Target != imageOf(candidate.Manifest) || previous.Revision != revision {
			return Installation{}, ErrInstallation
		}
		if previous.Phase == "committed" {
			return previous, nil
		}
		if previous.Phase == "rolled_back" {
			return previous, ErrInstallation
		}
		return previous, ErrUpdatePending
	}
	policy.Now = time.Now().UTC()
	if err := i.History.Check(candidate.Signed, policy); err != nil {
		return Installation{}, err
	}
	s, exists, err := i.History.Installation(driver.Component())
	if err != nil {
		return Installation{}, err
	}
	if exists && pending(s) {
		return s, ErrUpdatePending
	}
	if !exists {
		path, version, err := driver.Current(ctx)
		if err != nil {
			return Installation{}, err
		}
		baseline, err := InspectImage(ctx, driver.Component(), path, version)
		if err != nil {
			return Installation{}, err
		}
		if _, err := MaterializeImage(ctx, i.Directory, path, driver.Component(), baseline); err != nil {
			return Installation{}, err
		}
		if err := i.History.BootstrapImage(ctx, driver.Component(), baseline); err != nil {
			return Installation{}, err
		}
	}
	image := imageOf(candidate.Manifest)
	path, err := MaterializeImage(ctx, i.Directory, candidate.Path, driver.Component(), image)
	if err != nil {
		return Installation{}, err
	}
	if err := driver.Check(ctx, path, image.Version); err != nil {
		return Installation{}, err
	}
	policy.Now = time.Now().UTC()
	s, err = i.History.BeginInstallation(ctx, id, revision, candidate.Signed, policy)
	if err != nil {
		// A complete begin event can exist even if its checkpoint write failed.
		// Authenticate that reservation before any possible execution or retry.
		if recoverErr := i.History.RecoverCheckpoint(); recoverErr != nil {
			holdErr := driver.Hold()
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return s, errors.Join(err, recoverErr, holdErr, driver.Stop(stopCtx))
		}
		if state, exists, stateErr := i.History.Installation(driver.Component()); stateErr == nil && exists && pending(state) {
			if holdErr := driver.Hold(); holdErr != nil {
				return s, errors.Join(err, holdErr, driver.Stop(context.Background()))
			}
		}
		return s, err
	}
	if err := driver.Hold(); err != nil {
		return s, errors.Join(err, driver.Stop(context.Background()))
	}
	if err := driver.Activate(ctx, path); err != nil {
		return i.rollback(driver, id, err)
	}
	committed, err := i.History.TransitionInstallation(ctx, id, "committed")
	if err == nil {
		if err := driver.Release(); err != nil {
			return committed, errors.Join(err, driver.Stop(context.Background()))
		}
		return committed, nil
	}
	// Never undo a healthy activation behind a durable commit record merely
	// because the checkpoint result was uncertain. Recover authenticated state.
	if recoverErr := i.History.RecoverCheckpoint(); recoverErr == nil {
		if state, ok, stateErr := i.History.InstallationRequest(id); stateErr == nil && ok && state.Phase == "committed" {
			if err := driver.Release(); err != nil {
				return state, errors.Join(err, driver.Stop(context.Background()))
			}
			return state, nil
		}
	} else {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return committed, errors.Join(err, recoverErr, driver.Stop(stopCtx))
	}
	return i.rollback(driver, id, err)
}
func (i *Installer) rollback(driver Driver, id string, cause error) (Installation, error) {
	if err := driver.Hold(); err != nil {
		return Installation{}, errors.Join(cause, err, driver.Stop(context.Background()))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, err := i.History.TransitionInstallation(ctx, id, "rolling_back")
	if err != nil {
		return s, errors.Join(cause, err, driver.Stop(ctx))
	}
	path, err := ResolveImage(ctx, i.Directory, s.Component, s.Current)
	if err == nil {
		err = driver.Activate(ctx, path)
	}
	phase := "rolled_back"
	if err != nil {
		phase = "failed_closed"
		err = errors.Join(err, driver.Stop(ctx))
	}
	if phase == "failed_closed" && processStopUnconfirmed(driver) {
		return s, errors.Join(cause, err, ErrProcessStopUnconfirmed)
	}
	final, saveErr := i.History.TransitionInstallation(ctx, id, phase)
	if saveErr != nil {
		saveErr = errors.Join(saveErr, driver.Stop(ctx))
	}
	if saveErr == nil && phase == "rolled_back" {
		if err := driver.Release(); err != nil {
			saveErr = errors.Join(err, driver.Stop(ctx))
		}
	}
	return final, errors.Join(cause, err, saveErr)
}

// ApplyImage performs the supervised switch for a locally verified direct-URL
// core image. The caller has already checked the archive digest, extracted the
// single expected executable and matched the requested version; this method
// binds the image to the local history, validates it natively, activates it
// under a hold and rolls back to the previous image on failure.
func (i *Installer) ApplyImage(ctx context.Context, driver Driver, id string, revision int64, image Image, source string) (Installation, error) {
	if i.History == nil || i.Directory == "" || source == "" || !image.valid(driver.Component()) || !requestPattern.MatchString(id) || driver.NodeID() != i.History.nodeID || driver.Revision() != revision {
		return Installation{}, ErrInstallation
	}
	if previous, ok, err := i.History.InstallationRequest(id); err != nil {
		return Installation{}, err
	} else if ok {
		if previous.Component != driver.Component() || previous.Target == nil || *previous.Target != image || previous.Revision != revision {
			return Installation{}, ErrInstallation
		}
		if previous.Phase == "committed" {
			return previous, nil
		}
		if previous.Phase == "rolled_back" {
			return previous, ErrInstallation
		}
		return previous, ErrUpdatePending
	}
	s, exists, err := i.History.Installation(driver.Component())
	if err != nil {
		return Installation{}, err
	}
	if exists && pending(s) {
		return s, ErrUpdatePending
	}
	if !exists {
		path, version, err := driver.Current(ctx)
		if err != nil {
			return Installation{}, err
		}
		baseline, err := InspectImage(ctx, driver.Component(), path, version)
		if err != nil {
			return Installation{}, err
		}
		if _, err := MaterializeImage(ctx, i.Directory, path, driver.Component(), baseline); err != nil {
			return Installation{}, err
		}
		if err := i.History.BootstrapImage(ctx, driver.Component(), baseline); err != nil {
			return Installation{}, err
		}
		if s, _, err = i.History.Installation(driver.Component()); err != nil {
			return Installation{}, err
		}
	}
	// The installed image always advances the local sequence, so an operator
	// supplied counter can never collide with a retained image.
	if image.Sequence <= s.Current.Sequence {
		image.Sequence = s.Current.Sequence + 1
	}
	materialized, err := MaterializeImage(ctx, i.Directory, source, driver.Component(), image)
	if err != nil {
		return Installation{}, err
	}
	if err := driver.Check(ctx, materialized, image.Version); err != nil {
		return Installation{}, err
	}
	if _, err := i.History.BeginInstallationImage(ctx, driver.Component(), id, revision, image); err != nil {
		return Installation{}, err
	}
	if err := driver.Hold(); err != nil {
		return Installation{}, errors.Join(err, driver.Stop(context.Background()))
	}
	if err := driver.Activate(ctx, materialized); err != nil {
		return i.rollback(driver, id, err)
	}
	committed, err := i.History.TransitionInstallation(ctx, id, "committed")
	if err != nil {
		return i.rollback(driver, id, err)
	}
	if err := driver.Release(); err != nil {
		return committed, errors.Join(err, driver.Stop(context.Background()))
	}
	return committed, nil
}

// ReconcileCommitted re-establishes the exact recorded successful image after
// command receipt interruption. It cannot choose a new image or reset a grant.
func (i *Installer) ReconcileCommitted(ctx context.Context, driver Driver, id string) (Installation, error) {
	if i.History == nil || driver.NodeID() != i.History.nodeID {
		return Installation{}, ErrInstallation
	}
	s, ok, err := i.History.Installation(driver.Component())
	if err != nil || !ok {
		return s, ErrInstallation
	}
	if s.RequestID != id || s.Revision != driver.Revision() || (s.Phase != "committed" && s.Phase != "rolled_back") {
		return s, ErrInstallation
	}
	if err := driver.Hold(); err != nil {
		return s, errors.Join(err, driver.Stop(ctx))
	}
	path, err := ResolveImage(ctx, i.Directory, s.Component, s.Current)
	if err == nil {
		err = driver.Activate(ctx, path)
	}
	if err != nil {
		return s, errors.Join(err, driver.Stop(ctx))
	}
	if err := driver.Release(); err != nil {
		return s, errors.Join(err, driver.Stop(ctx))
	}
	return s, nil
}

// Recover restores only the last durable successful image for an interrupted
// attempt. The Reconciler still enforces original desired state and accounting.
func (i *Installer) Recover(ctx context.Context, driver Driver) (Installation, error) {
	if i.History == nil || driver.NodeID() != i.History.nodeID {
		return Installation{}, ErrInstallation
	}
	s, ok, err := i.History.Installation(driver.Component())
	if err != nil || !ok || !pending(s) {
		return s, err
	}
	if driver.Revision() != s.Revision {
		return s, ErrInstallation
	}
	return i.rollback(driver, s.RequestID, nil)
}

// ReconcileRetained reopens the authenticated image when an update was
// started but failed before the installer could append an installation event.
// The history must still be the last confirmed image; this method never
// creates or rewrites an installation record.
func (i *Installer) ReconcileRetained(ctx context.Context, driver Driver, image Image) (Installation, error) {
	if i.History == nil || driver.NodeID() != i.History.nodeID {
		return Installation{}, ErrInstallation
	}
	s, ok, err := i.History.Installation(driver.Component())
	confirmed := s.Phase == "ready" && s.RequestID == "" && s.Revision == 0 && s.Target == nil || s.Phase == "committed" || s.Phase == "rolled_back"
	if err != nil || !ok || !confirmed || s.Current != image {
		return s, ErrInstallation
	}
	if err := driver.Hold(); err != nil {
		return s, errors.Join(err, driver.Stop(ctx))
	}
	path, err := ResolveImage(ctx, i.Directory, s.Component, s.Current)
	if err == nil {
		err = driver.Activate(ctx, path)
	}
	if err != nil {
		return s, errors.Join(err, driver.Stop(ctx))
	}
	if err := driver.Release(); err != nil {
		return s, errors.Join(err, driver.Stop(ctx))
	}
	return s, nil
}
