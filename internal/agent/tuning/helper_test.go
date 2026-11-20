package tuning

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tuningcontract "proxycontrol/pkg/contracts/tuning"
)

func TestProbeOpensWithoutChangingValuesAndRejectsStaleBoot(t *testing.T) {
	h := fakeHelper(t)
	dir := t.TempDir()
	helper := filepath.Join(dir, installedHelperPath)
	put(t, helper, "bootstrap")
	missing := "net.core.default_qdisc"
	if err := os.Remove(sysPath(h.ProcRoot, missing)); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(h.ProcRoot, "kernel/random/boot_id"), "boot-a")
	before := Current(h.ProcRoot)
	report, err := collectProbe(h.ProcRoot, helper)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(before, Current(h.ProcRoot)) {
		t.Fatal("probe wrote sysctls")
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "probe.json"), string(raw))
	keys, err := ReadProbe(dir, h.ProcRoot, dir)
	if err != nil || len(keys) != 19 {
		t.Fatal(keys, err)
	}
	for _, k := range keys {
		if k.Key == missing {
			if k.Present || k.Writable || !strings.Contains(k.ProbeError, "缺少入口") {
				t.Fatal(k)
			}
		} else if !k.Present || !k.Writable || k.ProbeError != "" {
			t.Fatal(k)
		}
	}
	put(t, filepath.Join(h.ProcRoot, "kernel/random/boot_id"), "boot-b")
	if _, err = ReadProbe(dir, h.ProcRoot, dir); err == nil {
		t.Fatal("accepted stale boot")
	}
	report.Keys = report.Keys[:18]
	report.BootID = "boot-b"
	raw, _ = json.Marshal(report)
	put(t, filepath.Join(dir, "probe.json"), string(raw))
	if _, err = ReadProbe(dir, h.ProcRoot, dir); err == nil {
		t.Fatal("accepted incomplete probe")
	}
}

func TestProbeRejectsLegacyUpdaterHelperReplacement(t *testing.T) {
	h := fakeHelper(t)
	dir := t.TempDir()
	put(t, filepath.Join(h.ProcRoot, "kernel/random/boot_id"), "unchanged-boot")
	helper := filepath.Join(dir, installedHelperPath)
	put(t, helper, "candidate")
	report, err := collectProbe(h.ProcRoot, helper)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "probe.json"), string(raw))
	if keys, err := ReadProbe(dir, h.ProcRoot, dir); err != nil || len(keys) != 19 {
		t.Fatal(keys, err)
	}
	// A released updater restores an older bootstrap, preserving its timestamps.
	// It does not know that the new same-boot receipt should be invalidated.
	old := filepath.Join(dir, "rollback")
	put(t, old, "old-agent")
	info, err := os.Stat(helper)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(old, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, helper); err != nil {
		t.Fatal(err)
	}
	if keys, err := ReadProbe(dir, h.ProcRoot, dir); err == nil || len(keys) != 0 {
		t.Fatal("stale rights survived rollback of fixed helper")
	}
}

func TestOpenRCProbeReadiness(t *testing.T) {
	h := fakeHelper(t)
	dir := t.TempDir()
	helper := filepath.Join(dir, "usr/local/bin/proxy-agent")
	put(t, helper, "bootstrap")
	put(t, filepath.Join(h.ProcRoot, "kernel/random/boot_id"), "unchanged-boot")
	report, err := collectProbe(h.ProcRoot, helper)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(dir, "probe.json"), string(raw))
	if err := os.MkdirAll(filepath.Join(dir, "run/openrc"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"missing", "pending", "failed", "ready"} {
		if state != "missing" {
			put(t, filepath.Join(dir, "run/proxy-agent-tuning/probe-finished"), state+"\n")
		}
		keys, err := ReadProbe(dir, h.ProcRoot, dir)
		if (err == nil) != (state == "ready") || (len(keys) == 19) != (state == "ready") {
			t.Fatalf("OpenRC %s: keys=%d err=%v", state, len(keys), err)
		}
	}
}

func TestKernelWriteAndReadbackFailuresRollback(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmtUint(uint64(map[bool]int{false: 0, true: 1}[mismatch])), func(t *testing.T) {
			h := fakeHelper(t)
			plain := h
			failed := false
			h.writeOverride = func(key, value string) error {
				if key == "net.ipv4.tcp_ecn" && !failed {
					failed = true
					if mismatch {
						return nil
					}
					return errors.New("fixture kernel rejected write")
				}
				return plain.write(key, value)
			}
			r := h.Apply(Desired{RequestID: "failed", Settings: map[string]string{"net.core.somaxconn": "1024", "net.ipv4.tcp_ecn": "2"}})
			if r.Status != "failed" || !failed {
				t.Fatal(r)
			}
			for _, key := range []string{"net.core.somaxconn", "net.ipv4.tcp_ecn"} {
				if value, _ := h.read(key); value != "1" {
					t.Fatal("rollback failed", key, value)
				}
			}
			if _, err := os.Stat(filepath.Join(h.ConfigDir, ConfigName)); !os.IsNotExist(err) {
				t.Fatal("persisted failed transaction", err)
			}
			if r := plain.Apply(Desired{RequestID: "clear", Clear: true}); r.Status != "cleared" {
				t.Fatal(r)
			}
		})
	}
}

func fakeHelper(t *testing.T) Helper {
	t.Helper()
	h := Helper{ProcRoot: t.TempDir(), ConfigDir: t.TempDir()}
	for _, k := range Keys() {
		v := "1"
		if k == "net.ipv4.tcp_congestion_control" {
			v = "cubic"
		}
		if k == "net.core.default_qdisc" {
			v = "fq_codel"
		}
		if vector(k) {
			v = "4096 87380 6291456"
		}
		put(t, sysPath(h.ProcRoot, k), v+"\n")
	}
	put(t, sysPath(h.ProcRoot, "net.ipv4.tcp_available_congestion_control"), "reno cubic bbr\n")
	return h
}
func put(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestWhitelist(t *testing.T) {
	h := fakeHelper(t)
	for _, settings := range []map[string]string{{"kernel.core_pattern": "evil"}, {"net.ipv4.tcp_ecn": "3"}, {"fs.file-max": "65535"}, {"net.core.default_qdisc": "fq\n"}, {"net.ipv4.tcp_congestion_control": "bbr3"}, {"net.ipv4.tcp_rmem": "1 2 4194304"}, {"net.core.somaxconn": "+128"}, {"net.core.wmem_max": "268435457"}} {
		if err := Validate(settings, h.ProcRoot); err == nil {
			t.Fatalf("accepted %v", settings)
		}
	}
	for k, b := range tuningcontract.Bounds {
		for _, v := range []uint64{b.Min, b.Max} {
			if err := Validate(map[string]string{k: fmtUint(v)}, h.ProcRoot); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := Validate(map[string]string{"net.ipv4.tcp_congestion_control": "bbr", "net.core.default_qdisc": "fq"}, h.ProcRoot); err != nil {
		t.Fatal(err)
	}
}

func TestClearDoesNotWriteUnchangedHostOnlyKeys(t *testing.T) {
	h := fakeHelper(t)
	plain := h
	h.writeOverride = func(key, value string) error {
		if key == "fs.file-max" {
			t.Error("rewrote an unchanged host-only key")
			return errors.New("read-only fixture")
		}
		return plain.write(key, value)
	}
	if r := h.Apply(Desired{RequestID: "apply", Settings: map[string]string{"net.ipv4.tcp_ecn": "2"}}); r.Status != "applied" {
		t.Fatal(r)
	}
	if r := h.Apply(Desired{RequestID: "clear", Clear: true}); r.Status != "cleared" {
		t.Fatal(r)
	}
}
func fmtUint(n uint64) string { return strconv.FormatUint(n, 10) }
func TestApplyReplaceAndClearPreservesFirstSnapshot(t *testing.T) {
	h := fakeHelper(t)
	settings := map[string]string{"net.ipv4.tcp_congestion_control": "bbr", "net.ipv4.tcp_rmem": "16777216"}
	if r := h.Apply(Desired{RequestID: "first", Settings: settings}); r.Status != "applied" {
		t.Fatal(r)
	}
	if v, _ := h.read("net.ipv4.tcp_rmem"); v != "4096 87380 16777216" {
		t.Fatal(v)
	}
	raw, _ := os.ReadFile(filepath.Join(h.ConfigDir, ConfigName))
	if !strings.Contains(string(raw), "net.ipv4.tcp_rmem = 4096 87380 16777216") {
		t.Fatal(string(raw))
	}
	put(t, sysPath(h.ProcRoot, "net.ipv4.tcp_ecn"), "0\n")
	if r := h.Apply(Desired{RequestID: "second", Settings: map[string]string{"net.ipv4.tcp_congestion_control": "reno", "net.ipv4.tcp_ecn": "2"}}); r.Status != "applied" {
		t.Fatal(r)
	}
	if v, _ := h.read("net.ipv4.tcp_rmem"); v != "4096 87380 6291456" {
		t.Fatal("removed key not restored", v)
	}
	if r := h.Apply(Desired{RequestID: "clear", Clear: true}); r.Status != "cleared" {
		t.Fatal(r)
	}
	if v, _ := h.read("net.ipv4.tcp_congestion_control"); v != "cubic" {
		t.Fatal(v)
	}
	if v, _ := h.read("net.ipv4.tcp_ecn"); v != "1" {
		t.Fatal("clear lost first-apply baseline", v)
	}
	for _, p := range []string{ConfigName, snapshotName} {
		if _, err := os.Stat(filepath.Join(h.ConfigDir, p)); !os.IsNotExist(err) {
			t.Fatal("not removed", p, err)
		}
	}
}
func TestApplyFailureRestoresAllAndKeepsPersistence(t *testing.T) {
	h := fakeHelper(t)
	if r := h.Apply(Desired{RequestID: "one", Settings: map[string]string{"net.ipv4.tcp_ecn": "2"}}); r.Status != "applied" {
		t.Fatal(r)
	}
	// Fail persistence after kernel writes: the old config is now a directory.
	path := filepath.Join(h.ConfigDir, ConfigName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	r := h.Apply(Desired{RequestID: "two", Settings: map[string]string{"net.ipv4.tcp_ecn": "0", "net.core.somaxconn": "1024"}})
	if r.Status != "failed" || len(r.Keys) == 0 {
		t.Fatal(r)
	}
	if v, _ := h.read("net.ipv4.tcp_ecn"); v != "2" {
		t.Fatal("not rolled back", v)
	}
	if v, _ := h.read("net.core.somaxconn"); v != "1" {
		t.Fatal("not rolled back", v)
	}
	for _, k := range r.Keys {
		if k.Applied || k.Error == "" {
			t.Fatal(k)
		}
	}
}
func TestReadDesiredStrictAndAtomic(t *testing.T) {
	dir := t.TempDir()
	d := Desired{RequestID: "one", Settings: map[string]string{"net.ipv4.tcp_ecn": "1"}}
	if err := WriteDesired(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := readDesired(dir)
	if err != nil || got.RequestID != "one" {
		t.Fatal(got, err)
	}
	for _, bad := range []string{`{"request_id":"x","unknown":1}`, `{"request_id":"x"} {}`, strings.Repeat("x", 17000)} {
		put(t, filepath.Join(dir, "desired.json"), bad)
		if _, err := readDesired(dir); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
}

func TestApplyReturnsInitialValues(t *testing.T) {
	h := fakeHelper(t)
	r1 := h.Apply(Desired{RequestID: "r1", Settings: map[string]string{"net.ipv4.tcp_congestion_control": "bbr", "net.ipv4.tcp_ecn": "2"}})
	if r1.Status != "applied" {
		t.Fatalf("r1 failed: %+v", r1)
	}
	if r1.Initial["net.ipv4.tcp_congestion_control"] != "cubic" || r1.Initial["net.ipv4.tcp_ecn"] != "1" {
		t.Fatalf("unexpected r1.Initial: %+v", r1.Initial)
	}

	r2 := h.Apply(Desired{RequestID: "r2", Settings: map[string]string{"net.ipv4.tcp_congestion_control": "reno"}})
	if r2.Status != "applied" {
		t.Fatalf("r2 failed: %+v", r2)
	}
	if r2.Initial["net.ipv4.tcp_congestion_control"] != "cubic" {
		t.Fatalf("r2.Initial lost baseline cubic: %+v", r2.Initial)
	}

	rClear := h.Apply(Desired{RequestID: "clear", Clear: true})
	if rClear.Status != "cleared" {
		t.Fatalf("rClear failed: %+v", rClear)
	}
	if rClear.Initial["net.ipv4.tcp_congestion_control"] != "cubic" {
		t.Fatalf("rClear.Initial lost baseline cubic: %+v", rClear.Initial)
	}
}

func TestApplyRollsBackWholeRequestOnAbsentKey(t *testing.T) {
	h := fakeHelper(t)
	if err := os.Remove(sysPath(h.ProcRoot, "net.core.somaxconn")); err != nil {
		t.Fatal(err)
	}
	r := h.Apply(Desired{
		RequestID: "absent",
		Settings: map[string]string{
			"net.ipv4.tcp_ecn":   "2",
			"net.core.somaxconn": "1024",
		},
	})
	if r.Status != "failed" {
		t.Fatalf("expected failed status when key is absent, got %+v", r)
	}
	if v, _ := h.read("net.ipv4.tcp_ecn"); v != "1" {
		t.Fatalf("expected tcp_ecn to remain 1, got %q", v)
	}
	foundError := false
	for _, kr := range r.Keys {
		if strings.Contains(kr.Error, "net.core.somaxconn") {
			foundError = true
		}
	}
	if !foundError {
		t.Fatalf("expected error to name absent key net.core.somaxconn, got %+v", r.Keys)
	}
}
