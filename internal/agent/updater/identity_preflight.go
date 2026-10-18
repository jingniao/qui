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
	if err := h.diskHead(); err != nil {
		return err
	}
	s, found, err := h.Installation("proxy-agent")
	if err != nil || !found || pending(s) || s.Phase == "failed_closed" {
		return errors.Join(ErrHistory, err)
	}
	for _, r := range h.baselineRecoveries {
		if baselinePending(r) {
			return ErrHistory
		}
	}
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
