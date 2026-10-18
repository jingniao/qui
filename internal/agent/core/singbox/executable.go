package singbox

import (
	"context"
	"errors"
	"proxycontrol/internal/agent/core"
)

func (a *Adapter) ExecutablePath() string { a.mu.Lock(); defer a.mu.Unlock(); return a.Path }

// DetectVersion runs the executable's own version command. It is the version
// gate that runs before a downloaded core is switched in.
func (a *Adapter) DetectVersion(ctx context.Context, path string) (string, error) {
	capabilities, err := Detect(ctx, path)
	if err != nil {
		return "", err
	}
	return capabilities.CoreVersion, nil
}

func (a *Adapter) ValidateExecutable(ctx context.Context, path string, configuration core.RenderedConfig) (string, error) {
	capabilities, err := Detect(ctx, path)
	if err != nil {
		return "", err
	}
	validator := &Adapter{Path: path, Directory: a.Directory}
	if err := validator.Validate(ctx, configuration); err != nil {
		return "", err
	}
	return capabilities.CoreVersion, nil
}
func (a *Adapter) SelectExecutable(ctx context.Context, path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.process != nil {
		return errors.New("sing-box executable cannot change while running")
	}
	if err := a.lifecycleReadyLocked(); err != nil {
		return err
	}
	capabilities, err := detect(ctx, path)
	if err != nil {
		return err
	}
	a.Path = path
	a.applyUpstreamCapabilities(capabilities)
	a.current = core.RenderedConfig{}
	a.history = nil
	return nil
}
