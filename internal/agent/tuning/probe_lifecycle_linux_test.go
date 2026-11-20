package tuning

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTuningProbeLifecycle(t *testing.T) {
	for _, scenario := range []string{"install-failure", "legacy-rollback", "boot", "openrc-rollback", "openrc-boot", "openrc-restart-failure"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			h := fakeHelper(t)
			put(t, filepath.Join(h.ProcRoot, "kernel/random/boot_id"), "same-boot")
			helper := filepath.Join(dir, "proxy-agent")
			put(t, helper, "bootstrap")
			report, err := collectProbe(h.ProcRoot, helper)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(dir, "old-probe.json"), string(raw))
			cmd := exec.Command("bash", "../../../deploy/linux/tests/tuning-recovery-fixture.sh", scenario, dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v", out, err)
			}
			keys, err := ReadProbe(filepath.Join(dir, "host/var/lib/proxy-agent/tuning"), h.ProcRoot, filepath.Join(dir, "host"))
			if len(keys) != 0 || !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale probe after %s: %v %v", scenario, keys, err)
			}
		})
	}
}
