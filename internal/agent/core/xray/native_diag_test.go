package xray

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// nativeTestOutput reruns `xray run -test` on a configuration the adapter
// rejected and returns what Xray printed. Validate discards that output in
// production because it may echo credentials; test configurations only carry
// fixture secrets, and the message is the only way to see why CI's official
// core refused a render.
func nativeTestOutput(t *testing.T, binary string, data []byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "rejected.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		return "write failed: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "run", "-test", "-config", file).CombinedOutput()
	if len(out) > 4096 {
		out = out[len(out)-4096:]
	}
	if err != nil {
		return string(out) + "\n" + err.Error()
	}
	return string(out) + "\n(exit 0)"
}
