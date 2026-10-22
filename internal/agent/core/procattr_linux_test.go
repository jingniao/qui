//go:build linux

package core

import (
	"syscall"
	"testing"
)

// TestCoreProcessAttributesKillsTheCoreWithTheAgent proves the attribute the
// adapters hand to exec.Command is Pdeathsig SIGKILL. A missing attribute would
// let a core outlive a SIGKILLed Agent under a supervisor that only signals the
// Agent PID, keep its ports and block the replacement start.
func TestCoreProcessAttributesKillsTheCoreWithTheAgent(t *testing.T) {
	attributes := CoreProcessAttributes()
	if attributes == nil {
		t.Fatal("CoreProcessAttributes returned no attributes on linux")
	}
	if attributes.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("Pdeathsig = %v, want SIGKILL", attributes.Pdeathsig)
	}
}
