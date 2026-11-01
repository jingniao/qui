package testutil

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// MPTCPDial connects to address asking for MPTCP and reports whether the
// connection uses it. The kernel negotiates MPTCP only with an MPTCP listener,
// so the result tells whether that listener was opened with MPTCP.
func MPTCPDial(t *testing.T, address string) bool {
	t.Helper()
	dialer := net.Dialer{Timeout: 2 * time.Second}
	dialer.SetMultipathTCP(true)
	c, err := dialer.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	used, err := c.(*net.TCPConn).MultipathTCP()
	if err != nil {
		t.Fatal(err)
	}
	return used
}

// RequireKernelMPTCP skips the test unless this kernel negotiates MPTCP: a
// listener check proves nothing where no listener can use it. The cores job
// treats this skip as a failure.
func RequireKernelMPTCP(t *testing.T) {
	t.Helper()
	config := net.ListenConfig{}
	config.SetMultipathTCP(true)
	listener, err := config.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("kernel MPTCP unavailable:", err)
	}
	defer listener.Close()
	go func() {
		if c, err := listener.Accept(); err == nil {
			c.Close()
		}
	}()
	if !MPTCPDial(t, listener.Addr().String()) {
		t.Skip("kernel MPTCP unavailable: an MPTCP listener did not negotiate it")
	}
}

// CoreDefaultsToMPTCP runs a proxy core directly, with Go's own MPTCP default
// (multipathtcp=2) instead of the Agent's environment, and reports whether its
// listener on port uses MPTCP. It is the control for the listener checks.
func CoreDefaultsToMPTCP(t *testing.T, binary string, args []string, port int) bool {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = append(os.Environ(), "GODEBUG=multipathtcp=2")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		command.Process.Kill()
		command.Wait()
	}()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if c, err := net.DialTimeout("tcp", address, time.Second); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("core started directly never listened on", address)
		}
	}
	return MPTCPDial(t, address)
}
