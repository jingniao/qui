//go:build windows

package singbox

import "proxycontrol/internal/agent/core"

// udpEndpointHealthy keeps the adapter's own call site; the check itself is
// shared with the GOST core, which owns UDP listeners the same way.
func udpEndpointHealthy(endpoint string, pid int) bool {
	return core.UDPEndpointHealthy(endpoint, pid)
}
