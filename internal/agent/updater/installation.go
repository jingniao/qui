package updater

import (
	"context"
	"errors"
	"regexp"
	"runtime"
	"time"

	"proxycontrol/pkg/contracts/release"
)

var ErrInstallation = errors.New("release installation transition rejected")
var ErrUpdatePending = errors.New("release installation requires pending recovery")
var requestPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// Image identity is independent of its locally derived execution path. Sequence
// zero is reserved for the original operator-configured binary, copied locally.
type Image struct {
	SHA256   string `json:"sha256"`
	Bytes    int64  `json:"bytes"`
	Version  string `json:"version"`
	Sequence int64  `json:"sequence"`
}
type Installation struct {
	Component  string `json:"component"`
	Generation int64  `json:"generation"`
	Phase      string `json:"phase"`
	Current    Image  `json:"current"`
	Previous   *Image `json:"previous,omitempty"`
	Target     *Image `json:"target,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	Revision   int64  `json:"revision,omitempty"`
}

func imageOf(m release.Manifest) Image {
	return Image{SHA256: m.SHA256, Bytes: m.Bytes, Version: m.Version, Sequence: m.Sequence}
}
func (i Image) manifest(component string) release.Manifest {
	at := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	return release.Manifest{SchemaVersion: 1, Component: component, Version: i.Version, OS: runtime.GOOS, Architecture: runtime.GOARCH, Sequence: max(1, i.Sequence), Bytes: i.Bytes, SHA256: i.SHA256, IssuedAt: at, ExpiresAt: at.Add(time.Hour)}
}
func (i Image) valid(component string) bool {
	return i.Sequence >= 0 && i.manifest(component).Validate() == nil
}
func imageEqual(a, b *Image) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func copyInstallation(s Installation) Installation {
	if s.Previous != nil {
		p := *s.Previous
		s.Previous = &p
	}
	if s.Target != nil {
		p := *s.Target
		s.Target = &p
	}
	return s
}
func pending(s Installation) bool {
	return s.Phase == "switching" || s.Phase == "rolling_back" || s.Phase == "failed_closed"
}

func (h *History) validateInstallation(next Installation) error {
	if !release.Component(next.Component) || !next.Current.valid(next.Component) || next.Previous != nil && !next.Previous.valid(next.Component) || next.Target != nil && !next.Target.valid(next.Component) {
		return ErrInstallation
	}
	old, exists := h.installations[next.Component]
	if !exists {
		if next.Generation != 0 || next.Phase != "ready" || next.Current.Sequence != 0 || next.Previous != nil || next.Target != nil || next.RequestID != "" || next.Revision != 0 {
			return ErrInstallation
		}
		return nil
	}
	if next.Phase == "switching" {
		for _, r := range h.baselineRecoveries {
			if baselinePending(r) && h.currentBaselineRecovery[r.Request.Release.Manifest.Component] == r.Request.ID {
				return ErrUpdatePending
			}
		}
		if h.refusedInstallations[next.RequestID] || pending(old) || next.Generation != old.Generation+1 || next.Current != old.Current || !imageEqual(next.Previous, old.Previous) || next.Target == nil || next.Target.Sequence <= old.Current.Sequence || next.Revision < 1 || !requestPattern.MatchString(next.RequestID) {
			return ErrInstallation
		}
		if _, exists := h.requests[next.RequestID]; exists {
			return ErrInstallation
		}
		accepted, exists := h.highest[next.Component]
		if !exists || *next.Target != imageOf(accepted) {
			// A direct-URL core update records its locally verified image
			// instead of a signed acceptance.
			if !h.verifiedImages[next.Component][*next.Target] {
				return ErrInstallation
			}
		}
		for component, s := range h.installations {
			if component != next.Component && pending(s) {
				return ErrUpdatePending
			}
		}
		return nil
	}
	if !pending(old) || next.Generation != old.Generation || next.RequestID != old.RequestID || next.Revision != old.Revision || !imageEqual(next.Target, old.Target) {
		return ErrInstallation
	}
	switch next.Phase {
	case "committed":
		if old.Phase != "switching" || old.Target == nil || next.Current != *old.Target || next.Previous == nil || *next.Previous != old.Current {
			return ErrInstallation
		}
	case "rolling_back":
		if old.Phase != "switching" && old.Phase != "failed_closed" || next.Current != old.Current || !imageEqual(next.Previous, old.Previous) {
			return ErrInstallation
		}
	case "rolled_back", "failed_closed":
		if old.Phase != "rolling_back" || next.Current != old.Current || !imageEqual(next.Previous, old.Previous) {
			return ErrInstallation
		}
	default:
		return ErrInstallation
	}
	return nil
}
func (h *History) observeEvent(event historyEvent) error {
	switch event.Kind {
	case "release-acceptance-v1":
		m := event.Release.Manifest
		if event.BaselineRecovery != nil || event.Installation != nil || m.Validate() != nil || m.OS != runtime.GOOS || m.Architecture != runtime.GOARCH {
			return ErrHistory
		}
		if old, ok := h.highest[m.Component]; ok && m.Sequence <= old.Sequence {
			return ErrHistory
		}
		h.highest[m.Component] = m
	case "release-installation-v1":
		if event.BaselineRecovery != nil || event.Installation == nil || event.Release != (release.Signed{}) {
			return ErrHistory
		}
		if event.Image != nil {
			// A direct-URL core update has no signed acceptance; the recorded
			// image is the local verification result for this target.
			if event.Image.valid(event.Installation.Component) == false || event.Installation.Target == nil || *event.Image != *event.Installation.Target {
				return ErrHistory
			}
			if h.verifiedImages[event.Installation.Component] == nil {
				h.verifiedImages[event.Installation.Component] = map[Image]bool{}
			}
			h.verifiedImages[event.Installation.Component][*event.Image] = true
		}
		if err := h.validateInstallation(*event.Installation); err != nil {
			return err
		}
		s := copyInstallation(*event.Installation)
		h.installations[s.Component] = s
		if s.RequestID != "" {
			h.requests[s.RequestID] = s
		}
	case "release-baseline-recovery-v1":
		if event.BaselineRecovery == nil || event.Installation != nil || event.Release != (release.Signed{}) {
			return ErrHistory
		}
		if err := h.validateBaselineRecovery(*event.BaselineRecovery); err != nil {
			return err
		}
		r := *event.BaselineRecovery
		h.baselineRecoveries[r.Request.ID] = r
		h.currentBaselineRecovery[r.Request.Release.Manifest.Component] = r.Request.ID
		h.refusedInstallations[r.Original.ID] = true
	default:
		return ErrHistory
	}
	return nil
}
func (h *History) appendEvent(event historyEvent) error {
	if h.head.Number >= maxHistoryEvents {
		return ErrHistory
	}
	event.NodeID, event.Number, event.Previous = h.nodeID, h.head.Number+1, h.head.Digest
	data, err := h.encode(event)
	if err != nil {
		return err
	}
	if err := exclusiveRoot(h.root, eventName(event.Number), data); err != nil {
		return err
	}
	next := historyHead{Kind: h.head.Kind, NodeID: h.nodeID, Number: event.Number, Digest: eventDigest(data)}
	if err := h.writeHead(next); err != nil {
		return err
	}
	if err := h.observeEvent(event); err != nil {
		return err
	}
	h.head = next
	return nil
}
func (h *History) Installation(component string) (Installation, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.diskHead(); err != nil {
		return Installation{}, false, err
	}
	s, ok := h.installations[component]
	return copyInstallation(s), ok, nil
}
func (h *History) InstallationRequest(id string) (Installation, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.diskHead(); err != nil {
		return Installation{}, false, err
	}
	s, ok := h.requests[id]
	return copyInstallation(s), ok, nil
}
func (h *History) BootstrapImage(ctx context.Context, component string, image Image) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := h.diskHead(); err != nil {
		return err
	}
	if old, ok := h.installations[component]; ok {
		if old.Generation == 0 && old.Current == image {
			return nil
		}
		return ErrInstallation
	}
	next := Installation{Component: component, Phase: "ready", Current: image}
	if err := h.validateInstallation(next); err != nil {
		return err
	}
	return h.appendEvent(historyEvent{Kind: "release-installation-v1", Installation: &next})
}
func (h *History) BeginInstallation(ctx context.Context, id string, revision int64, s release.Signed, policy release.Policy) (Installation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Installation{}, err
	}
	if err := h.diskHead(); err != nil {
		return Installation{}, err
	}
	if err := h.check(s, policy); err != nil {
		return Installation{}, err
	}
	old, ok := h.installations[s.Manifest.Component]
	if !ok {
		return Installation{}, ErrInstallation
	}
	next := copyInstallation(old)
	image := imageOf(s.Manifest)
	next.Generation++
	next.Phase = "switching"
	next.Target = &image
	next.RequestID = id
	next.Revision = revision
	if err := h.validateInstallation(next); err != nil {
		return Installation{}, err
	}
	if err := h.appendEvent(historyEvent{Kind: "release-installation-v1", Installation: &next}); err != nil {
		return Installation{}, err
	}
	return copyInstallation(next), nil
}

// BeginInstallationImage reserves a locally verified image for a direct-URL
// core update, which has no signed acceptance to bind the target to. The
// recorded image is the local verification result for this exact target.
func (h *History) BeginInstallationImage(ctx context.Context, component, id string, revision int64, image Image) (Installation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Installation{}, err
	}
	if err := h.diskHead(); err != nil {
		return Installation{}, err
	}
	if !image.valid(component) || revision < 1 || !requestPattern.MatchString(id) {
		return Installation{}, ErrInstallation
	}
	old, ok := h.installations[component]
	if !ok {
		return Installation{}, ErrInstallation
	}
	if h.verifiedImages[component] == nil {
		h.verifiedImages[component] = map[Image]bool{}
	}
	h.verifiedImages[component][image] = true
	next := copyInstallation(old)
	target := image
	next.Generation++
	next.Phase = "switching"
	next.Target = &target
	next.RequestID = id
	next.Revision = revision
	if err := h.validateInstallation(next); err != nil {
		delete(h.verifiedImages[component], image)
		return Installation{}, err
	}
	if err := h.appendEvent(historyEvent{Kind: "release-installation-v1", Installation: &next, Image: &target}); err != nil {
		delete(h.verifiedImages[component], image)
		return Installation{}, err
	}
	return copyInstallation(next), nil
}

func (h *History) TransitionInstallation(ctx context.Context, id, phase string) (Installation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Installation{}, err
	}
	if err := h.diskHead(); err != nil {
		return Installation{}, err
	}
	s, ok := h.requests[id]
	if !ok {
		return Installation{}, ErrInstallation
	}
	old, ok := h.installations[s.Component]
	if !ok || old.RequestID != id {
		return Installation{}, ErrInstallation
	}
	if old.Phase == phase {
		return copyInstallation(old), nil
	}
	next := copyInstallation(old)
	next.Phase = phase
	if phase == "committed" && old.Target != nil {
		previous := old.Current
		next.Previous = &previous
		next.Current = *old.Target
	}
	if err := h.validateInstallation(next); err != nil {
		return Installation{}, err
	}
	if err := h.appendEvent(historyEvent{Kind: "release-installation-v1", Installation: &next}); err != nil {
		return Installation{}, err
	}
	return copyInstallation(next), nil
}
