package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"proxycontrol/internal/agent/configuration"
	"proxycontrol/internal/agent/core"
	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/release"
	"strings"
	"testing"
)

// Start from a sing-box primary so the official Xray zip fixture is the absent
// component. Every adapter is fake; download/archive/image/history are real.
func membershipHarness(t *testing.T) (*coreUpdates, *core.MultiRuntime, string, string) {
	a := &fakeCoreRuntime{version: "26.3.27"}
	w, c, m, original := updateHarness(t, a)
	b := &fakeCoreRuntime{name: "sing-box", version: "1.12.0", selected: original}
	multi, err := core.NewMultiRuntime(b)
	if err != nil {
		t.Fatal(err)
	}
	m.Adapter = multi
	s := desiredstate.State{SchemaVersion: 1, NodeID: "node-update", Revision: 2, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
	if _, err = m.Apply(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	w.component = "sing-box"
	w.cfg.CoreName = "sing-box"
	w.cfg.CorePath = original
	w.detect = multi.DetectCapabilities
	w.makeRuntime = func(node, directory, name, path string) core.Runtime {
		return &fakeCoreRuntime{name: name, version: "26.3.27", selected: path}
	}
	return w, multi, c.DataDirectory, original
}
func addRequest(digest string) release.CoreUpdateRequest {
	return release.CoreUpdateRequest{SchemaVersion: 2, ID: "add_xray", NodeID: "node-update", Revision: 2, Sequence: 1, Component: "xray", Version: "26.3.27", URL: updateAssetURL, SHA256: digest}
}
func TestCoreMembershipAddRemoveRecoveryAndReadd(t *testing.T) {
	ctx := context.Background()
	w, multi, dir, original := membershipHarness(t)
	archive, digest, _ := updateArchive(t, original, "dynamic")
	server := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.Write(archive) }))
	defer server.Close()
	w.source = updateSource(t, server)
	req := addRequest(digest)
	result, err := w.execute(ctx, req)
	if err != nil || result.Status != "success" {
		t.Fatal(result, err)
	}
	if multi.Component("xray") == nil {
		t.Fatal("new core unavailable")
	}
	w.refreshCapabilities(ctx)
	if len(w.capabilities.Snapshot().Cores) != 2 {
		t.Fatal("capabilities not refreshed")
	}
	inventory, err := configuration.ReadInventory(dir, w.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	path := inventory.Entries["xray"].Path
	startupCfg := w.cfg
	startupCfg.CoreName = "xray"
	startupCfg.CorePath = path
	installed, err := openCoreInstallation(ctx, startupCfg, w.nodeID)
	if err != nil || installed == nil || installed.path != path {
		t.Fatal("committed addition cannot restart", installed, err)
	}
	installed.Close()
	cores, err := inventory.Merge(w.cfg)
	if err != nil || len(cores) != 2 {
		t.Fatal(cores, err)
	}
	// A crash immediately after commit reconstructs the enabled set from disk.
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	repeated, err := w.execute(ctx, req)
	if err != nil || repeated != result {
		t.Fatal("addition is not idempotent", repeated, err)
	}
	remove := &pb.RemoveCoreCommand{Id: "remove_xray", Component: "xray", Revision: 2}
	removed := w.removeCore(ctx, remove)
	if !removed.Success {
		t.Fatal(removed)
	}
	if multi.Component("xray") != nil || len(w.capabilities.Snapshot().Cores) != 1 {
		t.Fatal("removed runtime still visible")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("managed image remains", err)
	}
	if _, err = os.Stat(original); err != nil {
		t.Fatal("installer/operator image was touched", err)
	}
	// Simulate power loss after inventory commit but before image cleanup.
	if err = os.WriteFile(path, []byte("interrupted removal"), 0500); err != nil {
		t.Fatal(err)
	}
	if err = cleanupCoreImages(dir, "xray"); err != nil {
		t.Fatal(err)
	}
	inventory, err = configuration.ReadInventory(dir, w.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	cores, err = inventory.Merge(w.cfg)
	if err != nil || len(cores) != 1 || !inventory.Entries["xray"].Disabled {
		t.Fatal(cores, err)
	}
	if len(w.pendingRemovals()) != 1 {
		t.Fatal("durable removal receipt lost")
	}
	if err = w.acknowledgeRemoval(removed); err != nil || len(w.pendingRemovals()) != 0 {
		t.Fatal("removal ack", err)
	}
	if r := w.removeCore(ctx, remove); !r.Success {
		t.Fatal("duplicate removal failed", r)
	}
	req.ID = "readd_xray"
	req.Sequence = 2
	result, err = w.execute(ctx, req)
	if err != nil || result.Status != "success" {
		t.Fatal("readd", result, err)
	}
	inventory, _ = configuration.ReadInventory(dir, w.nodeID)
	if inventory.Entries["xray"].Generation != "readd_xray" {
		t.Fatal("old history reused")
	}
	// Subsequent ordinary updates bootstrap from the dynamic image, then removal
	// must preserve that history's anti-rollback high-water across a new namespace.
	req.ID = "update_dynamic"
	req.Sequence = 5
	result, err = w.execute(ctx, req)
	if err != nil || result.Status != "success" {
		t.Fatal("dynamic update", result, err)
	}
	remove.Id = "remove_updated"
	if r := w.removeCore(ctx, remove); !r.Success {
		t.Fatal(r)
	}
	req.ID = "stale_readd"
	req.Sequence = 3
	result, err = w.execute(ctx, req)
	if err != nil || result.Status == "success" {
		t.Fatal("removal reset history high-water", result, err)
	}
	req.ID = "new_readd"
	req.Sequence = 6
	result, err = w.execute(ctx, req)
	if err != nil || result.Status != "success" {
		t.Fatal("fresh sequence readd", result, err)
	}
}
func TestAbsentCoreFailureDoesNotEnable(t *testing.T) {
	w, multi, dir, original := membershipHarness(t)
	archive, digest, _ := updateArchive(t, original, "bad-version")
	server := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.Write(archive) }))
	defer server.Close()
	w.source = updateSource(t, server)
	w.makeRuntime = func(node, directory, name, path string) core.Runtime {
		return &fakeCoreRuntime{name: name, version: "1.0.0", selected: path}
	}
	req := addRequest(digest)
	r, err := w.execute(context.Background(), req)
	if err != nil || r.Status != "rejected" || r.Code != "version_mismatch" {
		t.Fatal(r, err)
	}
	if multi.Component("xray") != nil {
		t.Fatal("failed core enabled")
	}
	if _, err = os.Stat(filepath.Join(dir, "cores.json.enc")); !os.IsNotExist(err) {
		t.Fatal("failed addition wrote inventory", err)
	}
	req.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	r, err = w.execute(context.Background(), req)
	if err != nil || r.Code != "checksum_mismatch" {
		t.Fatal(r, err)
	}
}

type refusingClose struct{ *fakeCoreRuntime }

func (r *refusingClose) Close() error { return core.ErrCoreStopUnconfirmed }
func TestCoreRemovalRefusals(t *testing.T) {
	w, multi, dir, original := membershipHarness(t)
	ctx := context.Background()
	if r := w.removeCore(ctx, &pb.RemoveCoreCommand{Id: "last", Component: "sing-box", Revision: 2}); r.Code != "last_core" {
		t.Fatal(r)
	}
	if err := multi.AddIdle(&fakeCoreRuntime{name: "xray", selected: original}); err != nil {
		t.Fatal(err)
	}
	s := desiredstate.State{SchemaVersion: 1, NodeID: w.nodeID, Revision: 3, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}, Inbounds: []desiredstate.Inbound{{ID: "disabled", Core: "xray", Protocol: "socks5", Port: 18000, Listen: "127.0.0.1", Enabled: false, DefaultOutbound: "direct"}}}
	if _, err := w.manager.Apply(ctx, s); err != nil {
		t.Fatal(err)
	}
	if r := w.removeCore(ctx, &pb.RemoveCoreCommand{Id: "used", Component: "xray", Revision: 3}); r.Code != "core_in_use" {
		t.Fatal(r)
	}
	if r := w.removeCore(ctx, &pb.RemoveCoreCommand{Id: "stale", Component: "xray", Revision: 2}); r.Code != "revision_mismatch" {
		t.Fatal(r)
	}
	if _, err := os.Stat(filepath.Join(dir, "cores.json.enc")); !os.IsNotExist(err) {
		t.Fatal("refusal changed inventory")
	}
	s.Revision = 4
	s.Inbounds = nil
	if _, err := w.manager.Apply(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := multi.RemoveIdle("xray"); err != nil {
		t.Fatal(err)
	}
	if err := multi.AddIdle(&refusingClose{&fakeCoreRuntime{name: "xray", selected: original}}); err != nil {
		t.Fatal(err)
	}
	if r := w.removeCore(ctx, &pb.RemoveCoreCommand{Id: "stop", Component: "xray", Revision: 4}); r.Success {
		t.Fatal("unconfirmed stop accepted")
	}
	if multi.Component("xray") == nil {
		t.Fatal("unconfirmed adapter forgotten")
	}
	if _, err := os.Stat(filepath.Join(dir, "cores.json.enc")); !os.IsNotExist(err) {
		t.Fatal("unconfirmed stop disabled inventory")
	}

	// Verify retained revision can still be opened by the same runtime contract.
	if _, err := reconcile.New(dir, w.nodeID, multi); err != nil {
		t.Fatal(err)
	}
}

func TestInstallerCoreRemovalPreservesRecordAndBinary(t *testing.T) {
	w, multi, dir, original := membershipHarness(t)
	if err := multi.AddIdle(&fakeCoreRuntime{name: "xray", selected: original}); err != nil {
		t.Fatal(err)
	}
	record := []byte("proxycontrol-install-v2\nagent\nprogram\nservice\n\n\ninstalled\n\n\nsystemd\n\n\ncore xray /usr/local/lib/proxy-agent/xray digest\n")
	recordPath := filepath.Join(dir, "installer.record")
	if err := os.WriteFile(recordPath, record, 0600); err != nil {
		t.Fatal(err)
	}
	result := w.removeCore(context.Background(), &pb.RemoveCoreCommand{Id: "disable_installer_xray", Component: "xray", Revision: 2})
	if !result.Success {
		t.Fatal(result)
	}
	got, err := os.ReadFile(recordPath)
	if err != nil || string(got) != string(record) {
		t.Fatal("installer record changed", err)
	}
	if _, err = os.Stat(original); err != nil {
		t.Fatal("external binary removed", err)
	}
	inventory, err := configuration.ReadInventory(dir, w.nodeID)
	if err != nil || !inventory.Entries["xray"].Disabled {
		t.Fatal(inventory, err)
	}
}

func TestRemovalRefusesInterruptedPendingRevision(t *testing.T) {
	w, multi, dir, original := membershipHarness(t)
	if err := multi.AddIdle(&fakeCoreRuntime{name: "xray", selected: original}); err != nil {
		t.Fatal(err)
	}
	pending := desiredstate.State{SchemaVersion: 1, NodeID: w.nodeID, Revision: 3, Logging: desiredstate.Logging{DomainOnly: true}, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}, Inbounds: []desiredstate.Inbound{{ID: "pending", Core: "xray", Protocol: "socks5", Listen: "127.0.0.1", Port: 19000, DefaultOutbound: "direct"}}}
	data, err := json.Marshal(pending)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(dir, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := vault.FromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := v.Seal(data)
	if err != nil {
		t.Fatal(err)
	}
	revisionDir := filepath.Join(dir, "revisions", "3")
	if err = os.MkdirAll(revisionDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(revisionDir, "desired.json.enc"), sealed, 0600); err != nil {
		t.Fatal(err)
	}
	result := w.removeCore(context.Background(), &pb.RemoveCoreCommand{Id: "pending_remove", Component: "xray", Revision: 2})
	if result.Code != "core_pending" || result.Success {
		t.Fatal(result)
	}
	if multi.Component("xray") == nil {
		t.Fatal("pending core removed")
	}
}

func TestRemovalStartupRecoveryWaitsForConfirmedExit(t *testing.T) {
	w, _, dir, original := membershipHarness(t)
	imageDir := filepath.Join(dir, "releases", "installed")
	if err := os.MkdirAll(imageDir, 0700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(imageDir, "xray-"+strings.Repeat("a", 64)+".bin")
	if err := os.WriteFile(image, []byte("retained"), 0500); err != nil {
		t.Fatal(err)
	}
	inventory := configuration.Inventory{NodeID: w.nodeID, Entries: map[string]configuration.InventoryCore{"xray": {Path: image, Disabled: true, RequestID: "remove_crash", Revision: 2, RemoveUnacked: true}}}
	if err := configuration.WriteInventory(dir, inventory); err != nil {
		t.Fatal(err)
	}
	enabled := []configuration.Core{{Name: "sing-box", Path: original}}
	refusing := func(node, directory, name, path string) core.Runtime {
		return &refusingClose{&fakeCoreRuntime{name: name, selected: path}}
	}
	if err := recoverCoreInventory(dir, w.nodeID, inventory, enabled, refusing); err == nil {
		t.Fatal("unconfirmed removal recovered")
	}
	if _, err := os.Stat(image); err != nil {
		t.Fatal("image removed before exact exit", err)
	}
	stopped := func(node, directory, name, path string) core.Runtime {
		return &fakeCoreRuntime{name: name, selected: path}
	}
	if err := recoverCoreInventory(dir, w.nodeID, inventory, enabled, stopped); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(image); !os.IsNotExist(err) {
		t.Fatal("interrupted cleanup not finished", err)
	}
	if err := recoverCoreInventory(dir, w.nodeID, inventory, enabled, stopped); err != nil {
		t.Fatal("recovery not idempotent", err)
	}
	if len(w.pendingRemovals()) != 1 {
		t.Fatal("startup receipt lost")
	}
}

func TestRemovalPersistsNewPrimary(t *testing.T) {
	w, multi, dir, original := membershipHarness(t)
	if err := multi.AddIdle(&fakeCoreRuntime{name: "xray", selected: original}); err != nil {
		t.Fatal(err)
	}
	i := configuration.Inventory{NodeID: w.nodeID, Entries: map[string]configuration.InventoryCore{"xray": {Path: original}}}
	if err := configuration.WriteInventory(dir, i); err != nil {
		t.Fatal(err)
	}
	if r := w.removeCore(context.Background(), &pb.RemoveCoreCommand{Id: "remove_primary", Component: "sing-box", Revision: 2}); !r.Success {
		t.Fatal(r)
	}
	inventory, err := configuration.ReadInventory(dir, w.nodeID)
	if err != nil || inventory.Primary != "xray" || multi.Name() != "xray" {
		t.Fatal(inventory, err)
	}
	if err := w.manager.Rollback(context.Background(), 1); err == nil {
		t.Fatal("ambiguous legacy rollback crossed primary change")
	}
	// Re-enabling the original YAML member cannot change empty-Core assignment.
	entry := inventory.Entries["sing-box"]
	entry.Disabled = false
	inventory.Entries["sing-box"] = entry
	cores, err := inventory.Merge(w.cfg)
	if err != nil || cores[0].Name != multi.Name() {
		t.Fatal("startup/live primary mismatch", cores, err)
	}
}
