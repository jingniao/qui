package tuning

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
	for k, b := range bounds {
		for _, v := range []uint64{b.min, b.max} {
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
