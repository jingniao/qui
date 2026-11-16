package testutil

import (
	"math/rand/v2"
	"net"
	"sync"
	"testing"
)

// Ports handed to tests come from below the kernel's ephemeral range (Linux
// 32768–60999, Windows 49152–65535). A port taken from the ephemeral range with
// "127.0.0.1:0" and released for a core to bind later can be reused by any
// outgoing connection or ":0" listener in the meantime, and the real-core
// packages run in parallel with thousands of such connections; the GOST X0
// test lost its port that way. The kernel never auto-assigns a port from this
// range, so only another explicit bind can take it.
const (
	freePortLow  = 24000
	freePortHigh = 32000
)

var (
	freePortMu   sync.Mutex
	freePortUsed = map[int]bool{}
)

// FreePort returns a loopback port that was free for both TCP and UDP and that
// this process has not handed out before. The probe sockets are closed, so a
// core can bind the port.
func FreePort(t testing.TB) int {
	t.Helper()
	freePortMu.Lock()
	defer freePortMu.Unlock()
	for attempt := 0; attempt < 200; attempt++ {
		port := freePortLow + rand.IntN(freePortHigh-freePortLow)
		if freePortUsed[port] {
			continue
		}
		tcp, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if err != nil {
			continue
		}
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		tcp.Close()
		if err != nil {
			continue
		}
		udp.Close()
		freePortUsed[port] = true
		return port
	}
	t.Fatal("could not select a loopback test port free on TCP and UDP")
	return 0
}
