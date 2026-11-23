//go:build linux

package core

import "testing"

// The core detaches its session; service post-stop owns the stop barrier.
func TestCoreProcessAttributesKillsTheCoreWithTheAgent(t *testing.T) {
	attributes := CoreProcessAttributes()
	if attributes == nil {
		t.Fatal("CoreProcessAttributes returned no attributes on linux")
	}
	if !attributes.Setsid || attributes.Pdeathsig != 0 {
		t.Fatalf("attributes = %+v, want Setsid and no Pdeathsig", attributes)
	}
}
