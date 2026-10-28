package connections

import (
	"testing"
	"time"
)

func TestAdmissionOrderStickyReleasePromotionLimitsResetAndIPv6(t *testing.T) {
	now := time.Unix(1000, 0)
	a := &Admission{Now: func() time.Time { return now }}
	a.Observe("u", "n", []string{"198.51.100.1", "198.51.100.2", "198.51.100.3", "198.51.100.4"}, false)
	v, g, n := a.State("u", 2)
	if n == nil || *n != 4 || len(g) != 2 || !v[0].Allowed || !v[1].Allowed {
		t.Fatal("first-seen admission failed")
	}
	now = now.Add(61 * time.Second)
	a.Observe("u", "n", []string{"198.51.100.3", "198.51.100.4"}, false)
	v, g, _ = a.State("u", 2)
	if !v[0].Allowed || len(g) != 2 {
		t.Fatal("inactive allowed key lost sticky slot")
	}
	now = now.Add(119 * time.Second)
	a.Observe("u", "n", []string{"198.51.100.3", "198.51.100.4"}, false)
	v, g, _ = a.State("u", 2)
	if len(v) != 2 || len(g) != 0 {
		t.Fatal("release failed to promote oldest gated keys")
	}
	_, g, _ = a.State("u", 1)
	if len(g) != 1 {
		t.Fatal("lower limit")
	}
	_, g, _ = a.State("u", 3)
	if len(g) != 0 {
		t.Fatal("raise limit")
	}
	a.Reset("u")
	_, _, n = a.State("u", 2)
	if n != nil {
		t.Fatal("reset retained observations")
	}
	a.Observe("u", "relay", []string{"198.51.100.8"}, true)
	a.Observe("u", "n", []string{"2001:db8:1:2::1", "2001:db8:1:2::2"}, false)
	a.Observe("u", "n2", []string{"2001:db8:1:2::3"}, false)
	v, _, n = a.State("u", 1)
	if n == nil || *n != 1 || len(v) != 1 || v[0].Address != "2001:db8:1::/48" || len(v[0].Nodes) != 2 {
		t.Fatal("IPv6 union, relay exclusion or mask failed")
	}
	now = now.Add(60 * time.Second)
	_, _, n = a.State("u", 1)
	if n != nil {
		t.Fatal("stale observation fabricated known count")
	}
}
