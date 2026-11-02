//go:build !windows

package core

import (
	"errors"
	"net"
	"syscall"
)

// UDPEndpointHealthy reports whether a configured UDP listener is present. An
// adapter can only own a UDP port exclusively, so a bind that fails with
// EADDRINUSE proves that some process is listening on that address; a bind that
// succeeds proves nothing is, and the socket is closed again.
func UDPEndpointHealthy(endpoint string, _ int) bool {
	socket, err := net.ListenPacket("udp", endpoint)
	if err == nil {
		socket.Close()
		return false
	}
	return errors.Is(err, syscall.EADDRINUSE)
}
