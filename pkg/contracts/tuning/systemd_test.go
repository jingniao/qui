package tuning

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestSystemdModuleDiscoverySandbox(t *testing.T) {
	for _, name := range []string{"proxy-agent.service", "proxy-agent-tuning.service"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("../../../deploy/systemd/" + name)
			if err != nil {
				t.Fatal(err)
			}
			var filter, capabilities bool
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "ProtectKernelModules=true" {
					t.Error("module directory must remain readable for algorithm discovery")
				}
				if line == "SystemCallFilter=~@module" {
					filter = true
				}
				if value, ok := strings.CutPrefix(line, "CapabilityBoundingSet="); ok {
					capabilities = true
					if strings.HasPrefix(value, "~") || strings.TrimSpace(value) == "" {
						t.Error("expected an explicit capability allowlist")
					}
					for _, capability := range strings.Fields(value) {
						if capability == "CAP_SYS_MODULE" {
							t.Error("module loading capability must not be granted")
						}
					}
				}
			}
			if !filter || !capabilities {
				t.Errorf("missing module syscall filter or capability allowlist: filter=%t capabilities=%t", filter, capabilities)
			}
		})
	}
}

func TestProbeUnitHasApplySandbox(t *testing.T) {
	service := func(name string) string {
		raw, err := os.ReadFile("../../../deploy/systemd/" + name)
		if err != nil {
			t.Fatal(err)
		}
		_, service, ok := strings.Cut(string(raw), "[Service]")
		if !ok {
			t.Fatalf("no Service section in %s", name)
		}
		return service
	}
	apply := service("proxy-agent-tuning.service")
	probe := service("proxy-agent-tuning-probe.service")
	if strings.Replace(probe, "--probe-tuning", "--apply-tuning", 1) != apply || !strings.Contains(probe, "--probe-tuning") {
		t.Fatal("probe and apply require identical service permissions")
	}
}

func TestProbeRequiresUnprivilegedReset(t *testing.T) {
	const reset = "proxy-agent-tuning-probe-reset.service"
	input, err := os.ReadFile("../../../deploy/systemd/proxy-agent-tuning-probe.service")
	if err != nil {
		t.Fatal(err)
	}
	unit, _, _ := strings.Cut(string(input), "[Service]")
	requires, after := false, false
	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(line, "Requires=") && slices.Contains(strings.Fields(strings.TrimPrefix(line, "Requires=")), reset) {
			requires = true
		}
		if strings.HasPrefix(line, "After=") && slices.Contains(strings.Fields(strings.TrimPrefix(line, "After=")), reset) {
			after = true
		}
	}
	if !requires || !after {
		t.Fatal("ordinary probe activation must start and wait for the reset independently of its sysctl sandbox")
	}
	input, err = os.ReadFile("../../../deploy/systemd/" + reset)
	if err != nil {
		t.Fatal(err)
	}
	s := string(input)
	for _, v := range []string{"\nUser=proxy-agent\n", "\nCapabilityBoundingSet=\n", "\nExecStart=/bin/rm -f -- /var/lib/proxy-agent/tuning/probe.json\n"} {
		if !strings.Contains(s, v) {
			t.Fatalf("invalid reset privilege or operation: missing %q", v)
		}
	}
	if strings.Contains(s, "/etc/sysctl.d") || strings.Contains(s, "WantedBy=") {
		t.Fatal("reset must be independently runnable, only as a probe dependency")
	}
}

func TestSystemdWritePathsCoverWhitelist(t *testing.T) {
	data, err := os.ReadFile("../../../deploy/systemd/proxy-agent-tuning.service")
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "ReadWritePaths="); ok {
			for _, path := range strings.Fields(value) {
				paths[strings.TrimPrefix(path, "-")] = true
			}
		}
	}
	check := func(key string) {
		t.Helper()
		if !paths["/proc/sys/"+strings.ReplaceAll(key, ".", "/")] {
			t.Errorf("missing write path for %s", key)
		}
	}
	for key := range Bounds {
		check(key)
	}
	for key := range Choices {
		check(key)
	}
}
