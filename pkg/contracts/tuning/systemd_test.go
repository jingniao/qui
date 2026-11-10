package tuning

import (
	"os"
	"strings"
	"testing"
)

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
