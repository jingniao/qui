package subscription

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// neutralPackages must stay independent of the panel's modules and of HTTP:
// they are the format-neutral model and the exporter registry (A1.5).
var neutralPackages = []string{
	"proxycontrol/internal/panel/subscription/proxy",
	"proxycontrol/internal/panel/subscription/rules",
	"proxycontrol/internal/panel/subscription/format",
	"proxycontrol/internal/panel/subscription/format/uri",
	"proxycontrol/internal/panel/subscription/format/singbox",
	"proxycontrol/internal/panel/subscription/format/clash",
	"proxycontrol/internal/panel/subscription/source",
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("source location unavailable")
	}
	directory := filepath.Dir(file)
	for depth := 0; depth < 8; depth++ {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		directory = filepath.Dir(directory)
	}
	t.Fatal("go.mod not found above the test file")
	return ""
}

// A1.5: `proxy/` and `format/` (and their exporters) don't import the panel's
// modules or `net/http`, so they can be reused and tested in isolation.
func TestNeutralPackagesDoNotImportThePanel(t *testing.T) {
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go toolchain is not on PATH")
	}
	forbidden := []string{
		"proxycontrol/internal/panel/user",
		"proxycontrol/internal/panel/node",
		"proxycontrol/internal/panel/system",
		"proxycontrol/internal/panel/deployment",
		"proxycontrol/pkg/contracts/desiredstate",
		"net/http",
	}
	root := repositoryRoot(t)
	for _, pkg := range neutralPackages {
		command := exec.Command(goBinary, "list", "-p", "1", "-deps", pkg)
		command.Dir = root
		output, err := command.Output()
		if err != nil {
			t.Fatalf("%s: %v", pkg, err)
		}
		dependencies := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, dependency := range dependencies {
			for _, banned := range forbidden {
				if dependency == banned {
					t.Errorf("%s depends on %s", pkg, banned)
				}
			}
		}
	}
}

// U5 lifts the import reservation without introducing unverified client UAs.
func TestImportFeatureParsersRegistered(t *testing.T) {
	registry := DefaultFormats()
	if len(registry.Parsers()) != 3 {
		t.Fatal("URI, Base64 and Clash parsers must be registered")
	}
	for _, exporter := range registry.Exporters() {
		if len(exporter.UserAgents()) != 0 {
			t.Fatal("unverified UA pattern")
		}
	}
}
