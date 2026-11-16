package singbox

import (
	"testing"

	"proxycontrol/internal/testutil"
)

// freePort picks a loopback port outside the ephemeral range; see
// testutil.FreePort for why released ":0" ports are not used.
func freePort(t *testing.T) int {
	t.Helper()
	return testutil.FreePort(t)
}
