//go:build windows

package singbox

import (
	"net"
	"os"
	"testing"
)

func TestUDPHealthUsesActualProcessAndWildcardAddressOwnership(t *testing.T) {
	socket, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	address := socket.LocalAddr().String()
	if !udpEndpointHealthy(address, os.Getpid()) {
		t.Fatal("owned wildcard UDP socket reported unhealthy")
	}
	if udpEndpointHealthy(address, 1) {
		t.Fatal("another process was credited with socket ownership")
	}
}
