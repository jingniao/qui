package tuning

import (
	"path/filepath"
	"testing"
)

func TestValidateAcceptsLoadableAlgorithm(t *testing.T) {
	root := t.TempDir()
	put(t, sysPath(root, "net.ipv4.tcp_available_congestion_control"), "reno cubic\n")
	put(t, sysPath(root, "kernel.osrelease"), "6.1.0-test\n")
	put(t, filepath.Join(root, "lib/modules/6.1.0-test/kernel/net/ipv4/tcp_bbr.ko.xz"), "")
	put(t, filepath.Join(root, "lib/modules/6.1.0-test/kernel/net/sched/sch_cake.ko.zst"), "")

	if err := Validate(map[string]string{
		"net.ipv4.tcp_congestion_control": "bbr",
		"net.core.default_qdisc":          "cake",
	}, root); err != nil {
		t.Fatalf("expected loadable bbr and cake to be accepted, got %v", err)
	}
	if err := Validate(map[string]string{"net.ipv4.tcp_congestion_control": "bbr3"}, root); err == nil {
		t.Fatal("expected unavailable bbr3 without module to be rejected")
	}
	if err := Validate(map[string]string{"net.core.default_qdisc": "fq"}, root); err == nil {
		t.Fatal("expected unavailable fq without module in module-aware root to be rejected")
	}
}
