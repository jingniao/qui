package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"proxycontrol/internal/agent/identity"
	"proxycontrol/internal/agent/storage"
	"proxycontrol/internal/agent/updater"
	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/release"
)

// Prepare the verified Agent image while the current Agent remains running.
// This command cannot alter the configured executable, identity, accounting,
// release acceptance sequence, service unit or node trust.
func prepareAgent(ctx context.Context, configPath, manifestPath, artifactPath string) (updater.AgentPreflight, error) {
	cfg, err := config.LoadAgent(configPath)
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	if !cfg.Updates.Enabled() {
		return updater.AgentPreflight{}, errors.New("local Agent release trust is not configured")
	}
	node, err := identity.Load(filepath.Join(cfg.DataDirectory, "identity"))
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	required, present, err := storage.Check(cfg.DataDirectory, node.NodeID, agentstorage.Current("release-tool"))
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	if !present {
		return updater.AgentPreflight{}, errors.New("start a compatible Agent to adopt its original storage contract before preparing an Agent update")
	}
	keys, err := release.PinnedKeys(cfg.Updates.Keys)
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, release.MaxManifestBytes+1))
	file.Close()
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	signed, err := release.Decode(data)
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	policy := release.Policy{Component: "proxy-agent", OS: runtime.GOOS, Architecture: runtime.GOARCH, Now: time.Now().UTC(), Keys: keys}
	if err := signed.Verify(policy); err != nil {
		return updater.AgentPreflight{}, err
	}
	artifact, err := os.Open(artifactPath)
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	defer artifact.Close()
	candidate, err := updater.Stage(ctx, filepath.Join(cfg.DataDirectory, "releases", "candidates"), signed, policy, artifact)
	if err != nil {
		return updater.AgentPreflight{}, err
	}
	return updater.PreflightAgent(ctx, filepath.Join(cfg.DataDirectory, "releases", "agent-prepared"), candidate, policy, required)
}
