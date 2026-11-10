package capability

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tuningcontract "proxycontrol/pkg/contracts/tuning"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestVirtualization(t *testing.T) {
	t.Run("lxc", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "proc/1/environ"), "PATH=/bin\x00container=lxc\x00")
		if got := detectVirtualization(root); got != "lxc" {
			t.Fatalf("got %q, want lxc", got)
		}
	})
	t.Run("docker", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ".dockerenv"), "")
		if got := detectVirtualization(root); got != "docker" {
			t.Fatalf("got %q, want docker", got)
		}
	})
	t.Run("openvz", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "proc/vz"), 0700); err != nil {
			t.Fatal(err)
		}
		if got := detectVirtualization(root); got != "openvz" {
			t.Fatalf("got %q, want openvz", got)
		}
	})
	t.Run("kvm", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "sys/class/dmi/id/sys_vendor"), "QEMU\n")
		if got := detectVirtualization(root); got != "kvm" {
			t.Fatalf("got %q, want kvm", got)
		}
	})
	t.Run("none", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "proc/cpuinfo"), "flags\t\t: fpu vme de pse\n")
		if got := detectVirtualization(root); got != "none" {
			t.Fatalf("got %q, want none", got)
		}
	})
	t.Run("unknown_missing", func(t *testing.T) {
		root := t.TempDir()
		if got := detectVirtualization(root); got != "unknown" {
			t.Fatalf("got %q, want unknown", got)
		}
	})
}

func TestKeyStates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc/sys/net/ipv4/tcp_ecn"), "2\n")
	writeFile(t, filepath.Join(root, "proc/self/mountinfo"), "20 1 0:19 / /proc/sys rw,nosuid,nodev,noexec,relatime - proc proc rw\n")
	states := tuningKeyStates(root)
	if len(states) != 19 {
		t.Fatalf("got %d states, want 19", len(states))
	}
	var ecn, somax *struct{ present, writable bool }
	for _, st := range states {
		if st.Key == "net.ipv4.tcp_ecn" {
			ecn = &struct{ present, writable bool }{st.Present, st.Writable}
		}
		if st.Key == "net.core.somaxconn" {
			somax = &struct{ present, writable bool }{st.Present, st.Writable}
		}
	}
	if ecn == nil || !ecn.present || !ecn.writable {
		t.Fatalf("expected tcp_ecn present and writable, got %+v", ecn)
	}
	if somax == nil || somax.present || somax.writable {
		t.Fatalf("expected somaxconn absent and not writable, got %+v", somax)
	}

	writeFile(t, filepath.Join(root, "proc/self/mountinfo"), "20 1 0:19 / /proc/sys ro,nosuid - proc proc ro\n")
	for _, st := range tuningKeyStates(root) {
		if st.Key == "net.ipv4.tcp_ecn" && (!st.Present || st.Writable) {
			t.Fatalf("expected read-only mount to mark tcp_ecn non-writable, got %+v", st)
		}
	}
}

func TestKeyStatesMountOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, mounts string
		fs, net      bool
	}{
		{"readonly_fs", "20 1 0:19 / /proc/sys rw - proc proc rw\n21 20 0:19 /fs /proc/sys/fs ro - proc proc rw\n", false, true},
		{"lxc_proc_mixed", "21 20 0:19 /net /proc/sys/net rw - proc proc rw\n20 1 0:19 / /proc/sys ro - proc proc rw\n", false, true},
		{"readonly_sys", "20 1 0:19 / /proc/sys ro - proc proc rw\n", false, false},
		{"unrelated_mount", "20 1 0:19 / /other ro - proc proc ro\n", true, true},
		{"missing_mountinfo", "", true, true},
		{"readonly_proc_superblock", "20 1 0:19 / /proc rw - proc proc ro\n", false, false},
		{"readonly_fs_superblock", "20 1 0:19 / /proc/sys rw - proc proc rw\n21 20 0:19 /fs /proc/sys/fs rw - proc proc ro\n", false, true},
		{"prefix_boundary", "20 1 0:19 / /proc/sys/ne ro - proc proc ro\n", true, true},
		{"stacked_sys_ro_over_rw", "20 1 0:19 / /proc/sys rw - proc proc rw\n22 20 0:19 / /proc/sys ro - proc proc rw\n23 22 0:19 /net /proc/sys/net rw - proc proc rw\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, k := range tuningcontract.Keys() {
				writeFile(t, filepath.Join(root, "proc/sys", strings.ReplaceAll(k, ".", "/")), "1\n")
			}
			if tc.mounts != "" {
				writeFile(t, filepath.Join(root, "proc/self/mountinfo"), tc.mounts)
			}
			for _, st := range tuningKeyStates(root) {
				if !st.Present {
					t.Errorf("%s should be present", st.Key)
				}
				want := tc.net
				if st.Key == "fs.file-max" {
					want = tc.fs
				}
				if st.Writable != want {
					t.Errorf("%s writable=%v, want %v", st.Key, st.Writable, want)
				}
			}
		})
	}
}

func TestProcPathReadOnlyPrefixBoundary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc/self/mountinfo"), "20 1 0:19 / /proc/sys/net ro - proc proc rw\n")
	mounts := procMounts(root)
	for _, path := range []string{"/proc/sys/net", "/proc/sys/net/ipv4/tcp_ecn"} {
		if !procPathReadOnly(mounts, path) {
			t.Errorf("%s should be read-only", path)
		}
	}
	if procPathReadOnly(mounts, "/proc/sys/network/key") {
		t.Fatal("/proc/sys/net must not cover /proc/sys/network")
	}
}

func TestAlgorithms(t *testing.T) {
	root := t.TempDir()
	kernel := "6.1.0-test"
	writeFile(t, filepath.Join(root, "proc/sys/net/ipv4/tcp_available_congestion_control"), "reno cubic\n")
	writeFile(t, filepath.Join(root, "lib/modules", kernel, "modules.builtin"), "kernel/net/sched/sch_fq_codel.ko\n")
	writeFile(t, filepath.Join(root, "proc/modules"), "sch_fq 20480 0 - Live 0x0\n")
	writeFile(t, filepath.Join(root, "lib/modules", kernel, "kernel/net/ipv4/tcp_bbr.ko.xz"), "")
	writeFile(t, filepath.Join(root, "lib/modules", kernel, "kernel/net/sched/sch_cake.ko.zst"), "")

	ccAvail, ccLoad, qAvail, qLoad := algorithms(root, kernel)
	if !slices.Equal(ccAvail, []string{"cubic", "reno"}) {
		t.Fatalf("ccAvail = %v, want [cubic reno]", ccAvail)
	}
	if !slices.Equal(ccLoad, []string{"bbr"}) {
		t.Fatalf("ccLoad = %v, want [bbr]", ccLoad)
	}
	if !slices.Equal(qAvail, []string{"fq", "fq_codel", "pfifo_fast"}) {
		t.Fatalf("qAvail = %v, want [fq fq_codel pfifo_fast]", qAvail)
	}
	if !slices.Equal(qLoad, []string{"cake"}) {
		t.Fatalf("qLoad = %v, want [cake]", qLoad)
	}

	ccAvail2, ccLoad2, qAvail2, qLoad2 := algorithms(t.TempDir(), "missing")
	if len(ccAvail2) != 0 || len(ccLoad2) != 0 || !slices.Equal(qAvail2, []string{"pfifo_fast"}) || len(qLoad2) != 0 {
		t.Fatalf("unexpected fallback on empty root: %v %v %v %v", ccAvail2, ccLoad2, qAvail2, qLoad2)
	}
}

func TestSysctlSources(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "usr/lib/sysctl.d/10-base.conf"), "net.ipv4.tcp_congestion_control = cubic\nnet.core.somaxconn = 256\n")
	writeFile(t, filepath.Join(root, "run/sysctl.d/50-run.conf"), "net.core.somaxconn = 512\n")
	writeFile(t, filepath.Join(root, "usr/lib/sysctl.d/99-bbr.conf"), "net.ipv4.tcp_congestion_control = reno\n")
	writeFile(t, filepath.Join(root, "etc/sysctl.d/99-bbr.conf"), "net.ipv4.tcp_congestion_control = bbr\nkernel.unknown = 1\n")
	writeFile(t, filepath.Join(root, "etc/sysctl.conf"), "net.ipv4.tcp_ecn = 1\n")

	sources := sysctlSources(root)
	got := map[string][2]string{}
	for _, s := range sources {
		got[s.Key] = [2]string{s.File, s.Value}
	}
	if got["net.ipv4.tcp_congestion_control"] != [2]string{"99-bbr.conf", "bbr"} {
		t.Fatalf("cc source = %v, want 99-bbr.conf=bbr (/etc overrides /usr/lib)", got["net.ipv4.tcp_congestion_control"])
	}
	if got["net.core.somaxconn"] != [2]string{"50-run.conf", "512"} {
		t.Fatalf("somaxconn source = %v, want 50-run.conf=512 (later overrides earlier)", got["net.core.somaxconn"])
	}
	if got["net.ipv4.tcp_ecn"] != [2]string{"sysctl.conf", "1"} {
		t.Fatalf("tcp_ecn source = %v, want sysctl.conf=1", got["net.ipv4.tcp_ecn"])
	}
	if _, ok := got["kernel.unknown"]; ok {
		t.Fatal("non-whitelist key included")
	}
}

func TestProcessLimits(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "proc/100/comm"), "proxy-agent\n")
	writeFile(t, filepath.Join(root, "proc/100/limits"), "Limit                     Soft Limit           Hard Limit           Units\nMax open files            524287               524288               files\n")
	writeFile(t, filepath.Join(root, "proc/100/task/100/children"), "200 201\n")
	writeFile(t, filepath.Join(root, "proc/200/comm"), "xray\n")
	writeFile(t, filepath.Join(root, "proc/200/limits"), "Limit                     Soft Limit           Hard Limit           Units\nMax open files            65536                524288               files\n")

	limits := processLimits(root, 100)
	if len(limits) != 2 {
		t.Fatalf("got %d limits, want 2: %+v", len(limits), limits)
	}
	if limits[0].Name != "proxy-agent" || limits[0].Soft != 524287 || limits[0].Hard != 524288 {
		t.Fatalf("unexpected self limit: %+v", limits[0])
	}
	if limits[1].Name != "xray" || limits[1].Soft != 65536 || limits[1].Hard != 524288 {
		t.Fatalf("unexpected child limit: %+v", limits[1])
	}

	if got := processLimits(t.TempDir(), 999); len(got) != 0 {
		t.Fatalf("expected empty limits on missing proc, got %+v", got)
	}
}
