package tuning

import (
	"os"
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
