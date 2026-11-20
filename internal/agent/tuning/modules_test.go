package tuning

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestModulePreparationRequiresSupportedTargetAndAvailableKey(t *testing.T) {
	host := t.TempDir()
	proc := filepath.Join(host, "proc/sys")
	state := t.TempDir()
	write := func(p, v string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(sysPath(proc, "kernel.random.boot_id"), "boot")
	write(sysPath(proc, "kernel.osrelease"), "6.8.0-100-generic")
	write(sysPath(proc, "net.ipv4.tcp_available_congestion_control"), "cubic reno")
	settings := map[string]string{"net.ipv4.tcp_congestion_control": "bbr", "net.core.default_qdisc": "fq"}
	d := Desired{RequestID: "module-test", Settings: settings}
	if _, err := d.missingModules(proc); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing qdisc entry", err)
	}
	write(sysPath(proc, "net.core.default_qdisc"), "fq")
	if missing, err := d.missingModules(proc); err != nil || !slices.Equal(missing, []string{"bbr"}) {
		t.Fatal(missing, err)
	}
	settings["net.ipv4.tcp_ecn"] = "999"
	if _, err := PrepareModules(context.Background(), d, state, proc, host, true); err == nil {
		t.Fatal("unrelated invalid key accepted")
	}
	delete(settings, "net.ipv4.tcp_ecn")
	for _, current := range []string{"registered", "loadable"} {
		if current == "registered" {
			write(sysPath(proc, "net.ipv4.tcp_available_congestion_control"), "cubic bbr")
		} else {
			write(sysPath(proc, "net.ipv4.tcp_available_congestion_control"), "cubic")
			write(filepath.Join(host, "lib/modules/6.8.0-100-generic/kernel/net/ipv4/tcp_bbr.ko.zst"), "module")
		}
		packages, err := PrepareModules(context.Background(), d, state, proc, host, true)
		if err != nil || len(packages) != 0 {
			t.Fatal(current, packages, err)
		}
	}
	if _, err := os.Stat(filepath.Join(state, moduleRequestName)); !os.IsNotExist(err) {
		t.Fatal("unexpected package request", err)
	}
}

func TestModuleRequestDuplicateUsesFinishedOrInterruptedReceipt(t *testing.T) {
	proc := t.TempDir()
	for path, value := range map[string]string{"kernel/random/boot_id": "boot", "kernel/osrelease": "6.8.0-100-generic", "net/core/default_qdisc": "pfifo_fast"} {
		path = filepath.Join(proc, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	state := t.TempDir()
	req := moduleRequest{ID: "once", Boot: "boot", Release: "6.8.0-100-generic", Targets: []string{"fq"}}
	settings := map[string]string{"net.core.default_qdisc": "fq"}
	calls := 0
	install := func(targets []string) ([]string, error) {
		calls++
		if !slices.Equal(targets, []string{"fq"}) {
			t.Fatal(targets)
		}
		return []string{"linux-modules-6.8.0-100-generic"}, nil
	}
	for i := 0; i < 2; i++ {
		if err := executeModuleRequest(req, settings, state, proc, install); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := readModuleReceipt(state)
	if err != nil || !receipt.Finished || len(receipt.Packages) != 1 || calls != 1 {
		t.Fatal(receipt, calls, err)
	}
	req.ID = "interrupted"
	if err := writeJSON(filepath.Join(state, "status.json"), moduleReceipt{moduleRequest: req}, 0644); err != nil {
		t.Fatal(err)
	}
	if err := executeModuleRequest(req, settings, state, proc, install); err == nil || !strings.Contains(err.Error(), "未确认") || calls != 1 {
		t.Fatal(calls, err)
	}
	req.Boot = "previous"
	if err := executeModuleRequest(req, settings, state, proc, install); err == nil || calls != 1 {
		t.Fatal("boot replay", err)
	}
}
