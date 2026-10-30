package control

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/core/singbox"
	"proxycontrol/internal/agent/core/xray"
	"proxycontrol/internal/agent/updater"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
	"strings"
)

func coreRuntime(nodeID, directory, name, path string) core.Runtime {
	if name == "sing-box" {
		return &singbox.Adapter{NodeID: nodeID, Directory: directory, Path: path}
	}
	return &xray.Adapter{NodeID: nodeID, Directory: directory, Path: path}
}

// New components are idle: the current authenticated revision must assign no
// inbound to them. Publication of inventory is the commit point. Before that
// point a crash leaves only a verified, unreachable image; after it startup
// constructs the same idle runtime. No peer or current revision is switched.
func (w *coreUpdates) addCore(ctx context.Context, request release.CoreUpdateRequest) (release.UpdateResult, error) {
	if !release.CoreComponent(request.Component) || request.Revision != w.manager.Current() {
		return w.finish(request, "rejected", "scope_mismatch", updater.Installation{})
	}
	extracted, err := w.source.FetchCore(ctx, filepath.Join(w.cfg.DataDirectory, "releases", "core-add-staging", request.Component), request.Component, request.URL, request.SHA256)
	if err != nil {
		return w.finish(request, "rejected", coreDownloadCode(err), updater.Installation{})
	}
	defer os.RemoveAll(filepath.Dir(extracted))
	image, err := updater.InspectImage(ctx, request.Component, extracted, request.Version)
	if err != nil {
		return w.finish(request, "rejected", "archive_invalid", updater.Installation{})
	}
	image.Sequence = request.Sequence
	runtime := w.makeRuntime(w.nodeID, filepath.Join(w.cfg.DataDirectory, "runtime", request.Component), request.Component, extracted)
	binary, ok := runtime.(core.BinaryRuntime)
	if !ok {
		return w.finish(request, "rejected", "preflight_failed", updater.Installation{})
	}
	version, err := binary.DetectVersion(ctx, extracted)
	if err != nil || version != request.Version {
		return w.finish(request, "rejected", "version_mismatch", updater.Installation{})
	}
	if caps, e := runtime.DetectCapabilities(ctx); e != nil || caps == nil {
		return w.finish(request, "rejected", "preflight_failed", updater.Installation{})
	}
	directory := filepath.Join(w.cfg.DataDirectory, "releases", "installed")
	path, err := updater.MaterializeImage(ctx, directory, extracted, request.Component, image)
	if err != nil {
		return w.finish(request, "failed_closed", "installation_failed", updater.Installation{})
	}
	if err = binary.SelectExecutable(ctx, path); err != nil {
		w.discardUnpublishedCore(request.Component)
		return w.finish(request, "rejected", "preflight_failed", updater.Installation{})
	}
	err = w.manager.WithCoreMembership(ctx, request.Revision, request.Component, func(m *core.MultiRuntime) error {
		if m.Component(request.Component) != nil {
			return errors.New("core already enabled")
		}
		inventory, e := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID)
		if e != nil {
			return e
		}
		previous := inventory.Entries[request.Component]
		if request.Sequence <= previous.Sequence {
			return errors.New("stale core sequence")
		}
		cfg := w.cfg
		cfg.CoreName = request.Component
		historyDir, checkpoint := coreHistoryPaths(cfg)
		_, dirErr := os.Lstat(historyDir)
		_, checkpointErr := os.Lstat(checkpoint)
		if previous.Disabled && (!errors.Is(dirErr, os.ErrNotExist) || !errors.Is(checkpointErr, os.ErrNotExist)) {
			history, e := openCoreHistory(cfg, w.nodeID)
			if e != nil {
				return e
			}
			state, exists, stateErr := history.Installation(request.Component)
			e = stateErr
			if e == nil && exists {
				high := state.Current.Sequence
				if state.Target != nil && state.Target.Sequence > high {
					high = state.Target.Sequence
				}
				if state.Previous != nil && state.Previous.Sequence > high {
					high = state.Previous.Sequence
				}
				if request.Sequence <= high {
					e = errors.New("stale core sequence")
				}
			}
			history.Close()
			if e != nil {
				return e
			}
		}
		if e = m.AddIdle(runtime); e != nil {
			return e
		}
		inventory.Primary = m.Name()
		inventory.Entries[request.Component] = configuration.InventoryCore{Path: path, RequestID: request.ID, Revision: request.Revision, Version: request.Version, Digest: request.SHA256, InstalledDigest: image.SHA256, ImageBytes: image.Bytes, URL: request.URL, Sequence: request.Sequence, Generation: request.ID}
		if e = configuration.WriteInventory(w.cfg.DataDirectory, inventory); e != nil { // Re-read: rename may have committed before directory sync failed.
			saved, readErr := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID)
			if readErr != nil || saved.Entries[request.Component].RequestID != request.ID {
				_ = m.RemoveIdle(request.Component)
				return e
			}
		}
		return nil
	})
	if err != nil {
		_ = runtime.Close()
		w.discardUnpublishedCore(request.Component)
		return w.finish(request, "rejected", "preflight_failed", updater.Installation{})
	}
	return w.finish(request, "success", "success", updater.Installation{Current: image})
}

// Only digest-derived files inside releases/installed are Agent-owned. Installer
// record v1/v2 paths (/usr/local/lib/proxy-agent/NAME) are never changed. A
// disabled overlay survives qui-update carrying YAML and its record forward.
func cleanupCoreImages(directory, name string) error {
	if name != "xray" && name != "sing-box" {
		return errors.New("invalid core")
	}
	installed := filepath.Join(directory, "releases", "installed")
	info, err := os.Lstat(installed)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid managed core directory")
	}
	root, err := os.OpenRoot(installed)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		digest := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(e.Name(), name+"-"), ".exe"), ".bin")
		if len(digest) != 64 {
			continue
		}
		if _, err := hex.DecodeString(digest); err != nil {
			continue
		}
		if !e.IsDir() && strings.HasPrefix(e.Name(), name+"-") && (strings.HasSuffix(e.Name(), ".bin") || strings.HasSuffix(e.Name(), ".bin.exe")) {
			if e.Type()&os.ModeSymlink == 0 {
				if err = root.Chmod(e.Name(), 0600); err != nil {
					return err
				}
			}
			if err = root.Remove(e.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}
func (w *coreUpdates) removeCore(ctx context.Context, command *pb.RemoveCoreCommand) *pb.RemoveCoreResult {
	result := &pb.RemoveCoreResult{Id: command.Id, Component: command.Component, Revision: command.Revision, Code: "invalid_request"}
	if w == nil || w.manager == nil {
		result.Code = "unavailable"
		return result
	}
	if !release.ValidUpdateID(command.Id) || !release.CoreComponent(command.Component) {
		return result
	}
	// A completed removal remains idempotent after later revisions. A crash
	// between overlay commit and runtime detachment still goes through the full
	// operation below, rather than reporting a detached runtime prematurely.
	duplicate := false
	err := w.manager.WithCoreInventory(func() error {
		i, e := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID)
		if e != nil {
			return e
		}
		entry := i.Entries[command.Component]
		if m, ok := w.manager.Adapter.(*core.MultiRuntime); ok && entry.Disabled && entry.RequestID == command.Id && entry.Revision == command.Revision && m.Component(command.Component) == nil {
			duplicate = true
			return cleanupCoreImages(w.cfg.DataDirectory, command.Component)
		}
		return nil
	})
	if duplicate && err == nil {
		result.Success = true
		result.Code = "success"
		return result
	}
	if err != nil {
		result.Code = "inventory_invalid"
		return result
	}
	err = w.manager.WithCoreMembership(ctx, command.Revision, command.Component, func(m *core.MultiRuntime) error {
		inventory, err := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID)
		if err != nil {
			return err
		}
		entry := inventory.Entries[command.Component]
		if entry.Disabled && entry.RequestID == command.Id && entry.Revision == command.Revision {
			if m.Component(command.Component) != nil {
				if err := m.RemoveIdle(command.Component); err != nil {
					return err
				}
			}
			return cleanupCoreImages(w.cfg.DataDirectory, command.Component)
		}
		if entry.RequestID == command.Id {
			return errors.New("request_conflict")
		}
		runtime := m.Component(command.Component)
		if runtime == nil {
			return errors.New("core_not_found")
		}
		if len(m.Names()) == 1 {
			return errors.New("last_core")
		}
		if err = runtime.Close(); err != nil {
			return err
		}
		if b, ok := runtime.(core.BinaryRuntime); ok {
			entry.Path = b.ExecutablePath()
		}
		if entry.Path == "" {
			return errors.New("core path unavailable")
		}
		entry.Disabled = true
		entry.RemoveUnacked = true
		entry.RequestID = command.Id
		entry.Revision = command.Revision
		inventory.Entries[command.Component] = entry
		inventory.Primary = m.Name()
		if inventory.Primary == command.Component {
			inventory.PrimaryChangedRevision = command.Revision
			for _, name := range m.Names() {
				if name != command.Component {
					inventory.Primary = name
					break
				}
			}
		}
		if err = configuration.WriteInventory(w.cfg.DataDirectory, inventory); err != nil {
			saved, e := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID)
			if e != nil || saved.Entries[command.Component] != entry {
				return err
			}
		}
		if err = m.RemoveIdle(command.Component); err != nil {
			return err
		}
		return cleanupCoreImages(w.cfg.DataDirectory, command.Component)
	})
	if err != nil {
		result.Code = err.Error()
		return result
	}
	result.Success = true
	result.Code = "success"
	w.refreshCapabilities(ctx)
	return result
}

func (w *coreUpdates) pendingRemovals() []*pb.RemoveCoreResult {
	if w == nil {
		return nil
	}
	inventory, err := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID)
	if err != nil {
		return nil
	}
	results := []*pb.RemoveCoreResult{}
	for name, e := range inventory.Entries {
		if e.Disabled && e.RemoveUnacked {
			results = append(results, &pb.RemoveCoreResult{Id: e.RequestID, Component: name, Revision: e.Revision, Success: true, Code: "success"})
		}
	}
	return results
}
func (w *coreUpdates) acknowledgeRemoval(result *pb.RemoveCoreResult) error {
	if w == nil || !result.Success {
		return nil
	}
	return w.manager.WithCoreInventory(func() error {
		inventory, err := configuration.ReadInventory(w.cfg.DataDirectory, w.nodeID)
		if err != nil {
			return err
		}
		e := inventory.Entries[result.Component]
		if !e.Disabled || e.RequestID != result.Id {
			return nil
		}
		e.RemoveUnacked = false
		inventory.Entries[result.Component] = e
		return configuration.WriteInventory(w.cfg.DataDirectory, inventory)
	})
}

func (w *coreUpdates) discardUnpublishedCore(name string) {
	_ = w.manager.WithCoreInventory(func() error {
		if m, ok := w.manager.Adapter.(*core.MultiRuntime); ok && m.Component(name) == nil {
			return cleanupCoreImages(w.cfg.DataDirectory, name)
		}
		return nil
	})
}

// recoverCoreInventory is run before constructing any enabled adapters. Disabled
// marker owners must confirm exact-child exit before their images are removed.
// Uncommitted additions are idle and only leave unreachable staged images.
func recoverCoreInventory(directory, nodeID string, inventory configuration.Inventory, cores []configuration.Core, makeRuntime func(string, string, string, string) core.Runtime) error {
	for name, entry := range inventory.Entries {
		if entry.Disabled {
			stopped := makeRuntime(nodeID, filepath.Join(directory, "runtime", name), name, entry.Path)
			if err := stopped.Close(); err != nil {
				return err
			}
		}
	}
	enabled := map[string]bool{}
	for _, entry := range cores {
		enabled[entry.Name] = true
	}
	for _, name := range []string{"xray", "sing-box"} {
		if !enabled[name] {
			if err := cleanupCoreImages(directory, name); err != nil {
				return err
			}
		}
	}
	return os.RemoveAll(filepath.Join(directory, "releases", "core-add-staging"))
}
