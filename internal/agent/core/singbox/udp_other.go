//go:build !windows

package singbox

import (
	"errors"
	"net"
	"syscall"
)

func udpEndpointHealthy(endpoint string, _ int) bool {
	socket, err := net.ListenPacket("udp", endpoint)
	if err == nil {
		socket.Close()
		return false
	}
	return errors.Is(err, syscall.EADDRINUSE)
}
