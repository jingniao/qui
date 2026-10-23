package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/agent/hostnet"
	"proxycontrol/pkg/contracts/desiredstate"
)

// allowHostNetwork makes the sync behave as on its only supported platform, so
// the helper protocol is exercised on a development host as well.
func allowHostNetwork(t *testing.T) {
	t.Helper()
	supported := hostNetworkSupported
	hostNetworkSupported = func() bool { return true }
	t.Cleanup(func() { hostNetworkSupported = supported })
}

// shortHostNetworkWait keeps the timeout case fast.
func shortHostNetworkWait(t *testing.T) {
	t.Helper()
	wait := hostNetworkStatusWait
	hostNetworkStatusWait = 500 * time.Millisecond
	t.Cleanup(func() { hostNetworkStatusWait = wait })
}

// fakeHostNetworkDial replaces the reachability proof.
func fakeHostNetworkDial(t *testing.T, dial func(ctx context.Context, network, address string) error) {
	t.Helper()
	previous := hostNetworkDial
	hostNetworkDial = dial
	t.Cleanup(func() { hostNetworkDial = previous })
}

// writeFakeHostNetworkStatus publishes one answer of the fake helper. A replace
// that a concurrent reader blocks is retried: on Windows an open file cannot be
// renamed over, and an answer lost that way would look like a helper that never
// reported. The packaged helper runs in its own process on Linux, where a rename
// replaces an open file.
func writeFakeHostNetworkStatus(directory string, status hostnet.Status) error {
	var err error
	for attempt := 0; attempt < 100; attempt++ {
		if err = hostnet.WriteStatus(directory, status); err == nil {
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
	return err
}

// startFakeHostNetworkHelper answers desired files the way the packaged oneshot
// unit does: it waits for a new id and publishes a status for that id.
func startFakeHostNetworkHelper(t *testing.T, directory string, answer func(hostnet.Desired) (bool, string)) {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		seen := ""
		for {
			select {
			case <-stop:
				return
			default:
			}
			if desired, err := hostnet.ReadDesired(directory); err == nil && desired.ID != seen {
				seen = desired.ID
				ok, message := answer(desired)
				if err := writeFakeHostNetworkStatus(directory, hostnet.Status{ID: desired.ID, OK: ok, Error: message}); err != nil {
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
}

// syncingManager is a Reconciler with a helper directory and a panel address.
func syncingManager(t *testing.T, directory, address string) *Reconciler {
	t.Helper()
	manager, err := New(t.TempDir(), "node-test", &fakeCoreAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	manager.HostNetworkDirectory = directory
	manager.ControlAddress = address
	return manager
}

// TestExemptAddressAndFamiliesFollowTheConfiguredNetwork pins the mapping the
// helper receives: the core outbound address lands in the field of its own family
// only, and only configured families are proven.
func TestExemptAddressAndFamiliesFollowTheConfiguredNetwork(t *testing.T) {
	cases := []struct {
		source string
		family int
		want   string
	}{
		{source: "", family: 4, want: ""},
		{source: "198.51.100.9", family: 4, want: "198.51.100.9"},
		{source: "198.51.100.9", family: 6, want: ""},
		{source: "2001:db8::9", family: 6, want: "2001:db8::9"},
		{source: "2001:db8::9", family: 4, want: ""},
		{source: "::ffff:198.51.100.9", family: 4, want: "::ffff:198.51.100.9"},
		{source: "not-an-address", family: 4, want: ""},
	}
	for _, test := range cases {
		if got := exemptAddress(test.source, test.family); got != test.want {
			t.Fatalf("exemptAddress(%q, %d) = %q, want %q", test.source, test.family, got, test.want)
		}
	}
	families := hostNetworkFamilies(desiredstate.Network{HostSourceIPv4: "198.51.100.4"})
	if len(families) != 1 || families[0].network != "tcp4" || families[0].family != 4 {
		t.Fatalf("ipv4 only network produced %+v", families)
	}
	families = hostNetworkFamilies(desiredstate.Network{HostSourceIPv6: "2001:db8::4"})
	if len(families) != 1 || families[0].network != "tcp6" || families[0].family != 6 {
		t.Fatalf("ipv6 only network produced %+v", families)
	}
	families = hostNetworkFamilies(desiredstate.Network{HostSourceIPv4: "198.51.100.4", HostSourceIPv6: "2001:db8::4"})
	if len(families) != 2 || families[0].network != "tcp4" || families[1].network != "tcp6" {
		t.Fatalf("dual-stack network produced %+v", families)
	}
	if families := hostNetworkFamilies(desiredstate.Network{}); len(families) != 0 {
		t.Fatalf("an unconfigured network produced %+v", families)
	}
}

// TestSyncHostNetworkMirrorsTheRevisionInTheHelper is the happy path: the desired
// file carries a fresh id, the exemption and both configured families, and the
// reachability proof dials the panel with the family of the configured address.
func TestSyncHostNetworkMirrorsTheRevisionInTheHelper(t *testing.T) {
	directory := t.TempDir()
	manager := syncingManager(t, directory, "127.0.0.1:9443")
	allowHostNetwork(t)
	shortHostNetworkWait(t)
	dials := []string{}
	fakeHostNetworkDial(t, func(_ context.Context, network, address string) error {
		dials = append(dials, network+" "+address)
		return nil
	})
	startFakeHostNetworkHelper(t, directory, func(hostnet.Desired) (bool, string) { return true, "" })
	network := desiredstate.Network{HostSourceIPv4: "198.51.100.4", OutboundSourceIP: "198.51.100.9"}
	if err := manager.syncHostNetwork(context.Background(), network); err != nil {
		t.Fatal(err)
	}
	desired, err := hostnet.ReadDesired(directory)
	if err != nil {
		t.Fatal(err)
	}
	if desired.ID == "" || desired.IPv4 != "198.51.100.4" || desired.ExemptIPv4 != "198.51.100.9" {
		t.Fatalf("the helper received %+v", desired)
	}
	if desired.IPv6 != "" || desired.ExemptIPv6 != "" {
		t.Fatalf("an unconfigured family reached the helper: %+v", desired)
	}
	if len(dials) != 1 || dials[0] != "tcp4 127.0.0.1:9443" {
		t.Fatalf("the reachability proof dialed %v", dials)
	}
}

// TestSyncHostNetworkReportsAHelperFailureAndATimeout keeps the two ways the
// helper can refuse visible to the apply.
func TestSyncHostNetworkReportsAHelperFailureAndATimeout(t *testing.T) {
	network := desiredstate.Network{HostSourceIPv4: "198.51.100.4"}
	t.Run("helper failure", func(t *testing.T) {
		directory := t.TempDir()
		manager := syncingManager(t, directory, "127.0.0.1:9443")
		allowHostNetwork(t)
		shortHostNetworkWait(t)
		fakeHostNetworkDial(t, func(context.Context, string, string) error { return nil })
		startFakeHostNetworkHelper(t, directory, func(hostnet.Desired) (bool, string) { return false, "nft -f - failed" })
		err := manager.syncHostNetwork(context.Background(), network)
		if err == nil || !strings.Contains(err.Error(), "nft -f - failed") {
			t.Fatalf("a failed helper was accepted: %v", err)
		}
	})
	t.Run("no status", func(t *testing.T) {
		manager := syncingManager(t, t.TempDir(), "127.0.0.1:9443")
		allowHostNetwork(t)
		shortHostNetworkWait(t)
		fakeHostNetworkDial(t, func(context.Context, string, string) error { return nil })
		err := manager.syncHostNetwork(context.Background(), network)
		if err == nil || !strings.Contains(err.Error(), "did not report") {
			t.Fatalf("a missing status was accepted: %v", err)
		}
	})
}

// TestSyncHostNetworkReportsADialFailure is the case the proof exists for: the
// rules were applied but new connections no longer reach the panel.
func TestSyncHostNetworkReportsADialFailure(t *testing.T) {
	directory := t.TempDir()
	manager := syncingManager(t, directory, "127.0.0.1:9443")
	allowHostNetwork(t)
	shortHostNetworkWait(t)
	fakeHostNetworkDial(t, func(context.Context, string, string) error { return errors.New("connection refused") })
	startFakeHostNetworkHelper(t, directory, func(hostnet.Desired) (bool, string) { return true, "" })
	err := manager.syncHostNetwork(context.Background(), desiredstate.Network{HostSourceIPv4: "198.51.100.4"})
	if err == nil || !strings.Contains(err.Error(), "no longer reach the panel") {
		t.Fatalf("an unreachable panel was accepted: %v", err)
	}
}

// TestSyncHostNetworkSkipsFamiliesThePanelCannotReach covers a node that enforces
// IPv6 while its panel is only reachable over IPv4, and the reverse: nothing can
// be proven for that family, so it must not fail the apply.
func TestSyncHostNetworkSkipsFamiliesThePanelCannotReach(t *testing.T) {
	cases := []struct {
		name    string
		address string
		network desiredstate.Network
	}{
		{name: "ipv6 node, ipv4 panel", address: "127.0.0.1:9443", network: desiredstate.Network{HostSourceIPv6: "2001:db8::4"}},
		{name: "ipv4 node, ipv6 panel", address: "[::1]:9443", network: desiredstate.Network{HostSourceIPv4: "198.51.100.4"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			manager := syncingManager(t, directory, test.address)
			allowHostNetwork(t)
			shortHostNetworkWait(t)
			fakeHostNetworkDial(t, func(context.Context, string, string) error {
				t.Fatal("a family without a panel address was dialed")
				return nil
			})
			startFakeHostNetworkHelper(t, directory, func(hostnet.Desired) (bool, string) { return true, "" })
			if err := manager.syncHostNetwork(context.Background(), test.network); err != nil {
				t.Fatalf("a family the panel cannot reach failed the apply: %v", err)
			}
		})
	}
}

// TestSyncHostNetworkSkipsPreFeatureInstallsAndRefusesAMissingHelper pins the two
// ways the helper directory is interpreted: nothing configured and no helper is a
// pre-feature install that keeps working, while configured values without the
// helper must fail the apply instead of being silently ignored.
func TestSyncHostNetworkSkipsPreFeatureInstallsAndRefusesAMissingHelper(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "host-network")
	manager := syncingManager(t, missing, "127.0.0.1:9443")
	allowHostNetwork(t)
	if err := manager.syncHostNetwork(context.Background(), desiredstate.Network{}); err != nil {
		t.Fatalf("an install without the helper failed an empty revision: %v", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an empty revision created the helper directory: %v", err)
	}
	err := manager.syncHostNetwork(context.Background(), desiredstate.Network{HostSourceIPv4: "198.51.100.4"})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("a configured revision without the helper was accepted: %v", err)
	}
}

// TestSyncHostNetworkRefusesOtherPlatforms keeps a non-empty host value from
// being accepted where nothing can enforce it, while an empty revision stays a
// no-op.
func TestSyncHostNetworkRefusesOtherPlatforms(t *testing.T) {
	manager := syncingManager(t, t.TempDir(), "127.0.0.1:9443")
	supported := hostNetworkSupported
	hostNetworkSupported = func() bool { return false }
	t.Cleanup(func() { hostNetworkSupported = supported })
	if err := manager.syncHostNetwork(context.Background(), desiredstate.Network{}); err != nil {
		t.Fatalf("an empty revision failed off Linux: %v", err)
	}
	err := manager.syncHostNetwork(context.Background(), desiredstate.Network{HostSourceIPv4: "198.51.100.4"})
	if err == nil || !strings.Contains(err.Error(), "requires Linux") {
		t.Fatalf("an enforced source off Linux was accepted: %v", err)
	}
}

// TestSyncHostNetworkClearsTheTableWhenNothingIsConfigured covers the clear path:
// a retained desired file that still enforces a family is removed, so the helper
// is driven with empty families and the panel is not dialed.
func TestSyncHostNetworkClearsTheTableWhenNothingIsConfigured(t *testing.T) {
	t.Run("the retained rules are cleared", func(t *testing.T) {
		directory := t.TempDir()
		manager := syncingManager(t, directory, "127.0.0.1:9443")
		allowHostNetwork(t)
		shortHostNetworkWait(t)
		fakeHostNetworkDial(t, func(context.Context, string, string) error {
			t.Fatal("a cleared revision dialed the panel")
			return nil
		})
		if err := hostnet.WriteDesired(directory, hostnet.Desired{ID: "previous", IPv4: "198.51.100.4"}); err != nil {
			t.Fatal(err)
		}
		startFakeHostNetworkHelper(t, directory, func(hostnet.Desired) (bool, string) { return true, "" })
		if err := manager.syncHostNetwork(context.Background(), desiredstate.Network{}); err != nil {
			t.Fatal(err)
		}
		desired, err := hostnet.ReadDesired(directory)
		if err != nil {
			t.Fatal(err)
		}
		if desired.ID == "" || desired.ID == "previous" || !desired.Clear() {
			t.Fatalf("a cleared revision wrote %+v", desired)
		}
	})
	t.Run("the clear request is awaited", func(t *testing.T) {
		directory := t.TempDir()
		manager := syncingManager(t, directory, "127.0.0.1:9443")
		allowHostNetwork(t)
		shortHostNetworkWait(t)
		if err := hostnet.WriteDesired(directory, hostnet.Desired{ID: "previous", IPv4: "198.51.100.4"}); err != nil {
			t.Fatal(err)
		}
		err := manager.syncHostNetwork(context.Background(), desiredstate.Network{})
		if err == nil || !strings.Contains(err.Error(), "did not report") {
			t.Fatalf("a clear that was never applied was accepted: %v", err)
		}
	})
}

// TestSyncHostNetworkLeavesTheHelperAloneWhenNothingIsEnforced pins the nodes that
// never used the feature: with nothing configured, an absent retained desired file
// means there is nothing to clear, and a retained clear request the helper confirmed
// is already in force, so the helper is neither written to nor waited for and a
// missing or broken helper cannot fail the apply.
func TestSyncHostNetworkLeavesTheHelperAloneWhenNothingIsEnforced(t *testing.T) {
	cases := []struct {
		name   string
		retain *hostnet.Desired
		status *hostnet.Status
	}{
		{name: "no retained desired file"},
		{
			name:   "retained clear desired file",
			retain: &hostnet.Desired{ID: "previous"},
			status: &hostnet.Status{ID: "previous", OK: true},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			manager := syncingManager(t, directory, "127.0.0.1:9443")
			allowHostNetwork(t)
			shortHostNetworkWait(t)
			fakeHostNetworkDial(t, func(context.Context, string, string) error {
				t.Fatal("an unconfigured revision dialed the panel")
				return nil
			})
			if test.retain != nil {
				if err := hostnet.WriteDesired(directory, *test.retain); err != nil {
					t.Fatal(err)
				}
			}
			if test.status != nil {
				if err := hostnet.WriteStatus(directory, *test.status); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.syncHostNetwork(context.Background(), desiredstate.Network{}); err != nil {
				t.Fatalf("an unconfigured revision needed the helper: %v", err)
			}
			desired, err := hostnet.ReadDesired(directory)
			if test.retain == nil {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("an unconfigured revision wrote %+v", desired)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if desired != *test.retain {
				t.Fatalf("an unconfigured revision rewrote the helper input: %+v", desired)
			}
		})
	}
}

// TestSyncHostNetworkRepublishesAClearTheHelperNeverConfirmed pins the other half:
// a retained clear request without a success status of its own id may have been
// published while the helper was not running, so it proves nothing about the table
// and the Agent publishes the clear again and waits for that answer. The fake
// helper answers the retained request itself as failed, so the seeded status cannot
// turn into a success before the sync reads it.
func TestSyncHostNetworkRepublishesAClearTheHelperNeverConfirmed(t *testing.T) {
	cases := []struct {
		name   string
		status *hostnet.Status
		helper bool
	}{
		{name: "no status file", helper: true},
		{name: "the same id failed", status: &hostnet.Status{ID: "previous", OK: false, Error: "test failure"}, helper: true},
		{name: "another id succeeded", status: &hostnet.Status{ID: "earlier", OK: true}, helper: true},
		{name: "no helper running"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			manager := syncingManager(t, directory, "127.0.0.1:9443")
			allowHostNetwork(t)
			shortHostNetworkWait(t)
			fakeHostNetworkDial(t, func(context.Context, string, string) error {
				t.Fatal("a clear revision dialed the panel")
				return nil
			})
			if err := hostnet.WriteDesired(directory, hostnet.Desired{ID: "previous"}); err != nil {
				t.Fatal(err)
			}
			if test.status != nil {
				if err := hostnet.WriteStatus(directory, *test.status); err != nil {
					t.Fatal(err)
				}
			}
			if test.helper {
				startFakeHostNetworkHelper(t, directory, func(desired hostnet.Desired) (bool, string) {
					if desired.ID == "previous" {
						return false, "test failure"
					}
					return true, ""
				})
			}
			err := manager.syncHostNetwork(context.Background(), desiredstate.Network{})
			if !test.helper {
				if err == nil || !strings.Contains(err.Error(), "did not report") {
					t.Fatalf("a clear nobody applied was accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			desired, readErr := hostnet.ReadDesired(directory)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if desired.ID == "" || desired.ID == "previous" || !desired.Clear() {
				t.Fatalf("an unconfirmed clear was not published again: %+v", desired)
			}
		})
	}
}

// TestApplyRollsBackRuntimeAndHostNetworkWhenTheHelperFails pins the call site:
// the helper is driven after a successful apply and before the manifest commit,
// and a failure restores both the previous runtime and the previous host rules
// under a temporary code that is retried.
func TestApplyRollsBackRuntimeAndHostNetworkWhenTheHelperFails(t *testing.T) {
	ctx := context.Background()
	adapter := &fakeCoreAdapter{}
	manager, err := New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	var synced []desiredstate.Network
	manager.HostNetworkSync = func(_ context.Context, network desiredstate.Network) error {
		synced = append(synced, network)
		if network.HostSourceIPv4 != "" {
			return errors.New("the host-network helper failed")
		}
		return nil
	}
	// The node-local availability check of the core outbound address is covered
	// by its own tests; here the address is not assigned to the test host.
	manager.NetworkAvailable = func(desiredstate.Network) error { return nil }
	if result, err := manager.Apply(ctx, state(freePort(t), 1)); err != nil || !result.Success {
		t.Fatalf("the first revision failed: %+v %v", result, err)
	}
	second := state(freePort(t), 2)
	second.Network = &desiredstate.Network{HostSourceIPv4: "198.51.100.4", OutboundSourceIP: "198.51.100.9"}
	result, err := manager.Apply(ctx, second)
	if err == nil || result.Code != hostNetworkFailedCode {
		t.Fatalf("a helper failure was not reported: %+v %v", result, err)
	}
	if permanentFailure(result.Code) {
		t.Fatal("a helper failure must not be cached permanently")
	}
	if !result.RolledBack || manager.Current() != 1 {
		t.Fatalf("the failed apply did not roll back: %+v revision %d", result, manager.Current())
	}
	if len(synced) != 3 || synced[0] != (desiredstate.Network{}) || synced[1].HostSourceIPv4 != "198.51.100.4" || synced[2] != (desiredstate.Network{}) {
		t.Fatalf("the helper was not driven back to the previous revision: %+v", synced)
	}
	if adapter.applied[len(adapter.applied)-1].Revision != 1 {
		t.Fatalf("the core was not restored to the previous revision: %+v", adapter.applied)
	}
	if _, err := os.Stat(filepath.Join(manager.Directory, "revisions", "2", "result.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the temporary helper failure was cached: %v", err)
	}
}

// TestRollbackDrivesTheHelperToTheRestoredRevision covers the manual rollback
// path: it ends in restoreRevision, which must leave the helper on the revision
// the core now runs.
func TestRollbackDrivesTheHelperToTheRestoredRevision(t *testing.T) {
	ctx := context.Background()
	adapter := &fakeCoreAdapter{}
	manager, err := New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	var synced []desiredstate.Network
	manager.HostNetworkSync = func(_ context.Context, network desiredstate.Network) error {
		synced = append(synced, network)
		return nil
	}
	manager.NetworkAvailable = func(desiredstate.Network) error { return nil }
	if result, err := manager.Apply(ctx, state(freePort(t), 1)); err != nil || !result.Success {
		t.Fatalf("the first revision failed: %+v %v", result, err)
	}
	second := state(freePort(t), 2)
	second.Network = &desiredstate.Network{HostSourceIPv4: "198.51.100.4"}
	if result, err := manager.Apply(ctx, second); err != nil || !result.Success {
		t.Fatalf("the second revision failed: %+v %v", result, err)
	}
	if err := manager.Rollback(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if manager.Current() != 1 {
		t.Fatalf("rollback left revision %d", manager.Current())
	}
	if len(synced) != 3 || synced[1].HostSourceIPv4 != "198.51.100.4" || synced[2] != (desiredstate.Network{}) {
		t.Fatalf("rollback did not restore the previous host rules: %+v", synced)
	}
}

// TestRestoreWithoutARetainedRevisionClearsTheHostNetwork covers the revision-0
// branch of Restore, and TestRestoreRuntimeLockedWithoutARevision covers the one
// of the runtime restore: nothing is retained, so nothing stays enforced.
func TestRestoreWithoutARetainedRevisionClearsTheHostNetwork(t *testing.T) {
	manager, err := New(t.TempDir(), "node-test", &fakeCoreAdapter{})
	if err != nil {
		t.Fatal(err)
	}
	var synced []desiredstate.Network
	manager.HostNetworkSync = func(_ context.Context, network desiredstate.Network) error {
		synced = append(synced, network)
		return nil
	}
	if err := manager.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(synced) != 1 || synced[0] != (desiredstate.Network{}) {
		t.Fatalf("an empty restore synced %+v", synced)
	}
}

func TestRestoreRuntimeLockedWithoutARevisionClearsTheHostNetwork(t *testing.T) {
	adapter := &fakeCoreAdapter{}
	manager, err := New(t.TempDir(), "node-test", adapter)
	if err != nil {
		t.Fatal(err)
	}
	var synced []desiredstate.Network
	manager.HostNetworkSync = func(_ context.Context, network desiredstate.Network) error {
		synced = append(synced, network)
		return nil
	}
	if err := manager.restoreRuntimeLocked(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if !adapter.closed {
		t.Fatal("the empty runtime was not closed")
	}
	if len(synced) != 1 || synced[0] != (desiredstate.Network{}) {
		t.Fatalf("an empty runtime restore synced %+v", synced)
	}
}
