package main

import (
	"net"
	"proxycontrol/internal/testutil"
	"testing"
)

// The //go:debug multipathtcp=0 directive keeps every listener of this binary
// plain TCP, while an explicit MPTCP listener still negotiates it (control).
func TestListenersArePlainTCPByDefault(t *testing.T) {
	testutil.RequireKernelMPTCP(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		if c, err := listener.Accept(); err == nil {
			c.Close()
		}
	}()
	if testutil.MPTCPDial(t, listener.Addr().String()) {
		t.Fatal("the default listener negotiated MPTCP (CVE-2026-98432 exposure)")
	}
}
