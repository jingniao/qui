package updater

import (
	"context"
	"errors"
	"proxycontrol/pkg/contracts/agentstorage"
)

// Identity activation is irreversible for retained executables that only read
// the legacy key/certificate files. Inspect without repairing history tails.
func CheckIdentityActivation(ctx context.Context, options HistoryOptions, images string, baseline Image, required agentstorage.Contract) (resultErr error) {
	dir, checkpoint, err := historyPaths(options)
	if err != nil {
		return err
	}
	h, err := historyHandles(options, dir, checkpoint)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, h.Close()) }()
	h.inspectionOnly = true
	if err := h.load(); err != nil {
		return err
	}
	return h.CheckActivation(ctx, images, baseline, required)
}

// CheckActivation proves, on an already opened history, that no installation is
// pending and that the original baseline and the current/previous installed
// images can all read the storage contract that would be persisted. It is the
// part of CheckIdentityActivation usable by the process that holds the history.
func (h *History) CheckActivation(ctx context.Context, images string, baseline Image, required agentstorage.Contract) error {
	s, found, err := h.Installation("proxy-agent")
	if err != nil || !found || pending(s) || s.Phase == "failed_closed" {
		return errors.Join(ErrHistory, err)
	}
	h.mu.Lock()
	for _, r := range h.baselineRecoveries {
		if baselinePending(r) {
			h.mu.Unlock()
			return ErrHistory
		}
	}
	h.mu.Unlock()
	for _, image := range []*Image{&baseline, &s.Current, s.Previous} {
		if image == nil {
			continue
		}
		if _, err := ProbeAgentImage(ctx, images, *image, required); err != nil {
			return err
		}
	}
	return nil
}
