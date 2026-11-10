package node

import (
	"context"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func TestMergeCandidatesKeepsLastSeenAndDropsGone(t *testing.T) {
	t1 := "2026-01-01T00:00:00Z"
	now := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	nowStr := now.Format(time.RFC3339)

	previous := []PublicAddress{
		{Address: "198.51.100.1", Source: "interface:eth0", Family: "ipv4", LastSeen: t1},
		{Address: "198.51.100.2", Source: "interface:eth0", Family: "ipv4", LastSeen: t1},
	}
	reported := []*pb.PublicAddress{
		{Address: "198.51.100.1", Interface: "eth0"},
		{Address: "198.51.100.3", Interface: "eth1"},
	}
	peer := "203.0.113.1"

	merged := mergeCandidates(previous, reported, peer, now)

	// Expect 198.51.100.1 (t1), 198.51.100.3 (nowStr), 203.0.113.1 (nowStr, source: peer)
	// 198.51.100.2 should be dropped.
	if len(merged) != 3 {
		t.Fatalf("expected 3 candidates, got %d: %+v", len(merged), merged)
	}

	m := make(map[string]PublicAddress)
	for _, c := range merged {
		m[c.Address] = c
	}

	if a, ok := m["198.51.100.1"]; !ok || a.LastSeen != t1 || a.Source != "interface:eth0" || a.Family != "ipv4" {
		t.Errorf("198.51.100.1 mismatch: %+v", a)
	}
	if _, ok := m["198.51.100.2"]; ok {
		t.Errorf("198.51.100.2 was not dropped")
	}
	if a, ok := m["198.51.100.3"]; !ok || a.LastSeen != nowStr || a.Source != "interface:eth1" || a.Family != "ipv4" {
		t.Errorf("198.51.100.3 mismatch: %+v", a)
	}
	if a, ok := m["203.0.113.1"]; !ok || a.LastSeen != nowStr || a.Source != "peer" || a.Family != "ipv4" {
		t.Errorf("203.0.113.1 mismatch: %+v", a)
	}
}

func TestEffectivePublicIPPrefersSelectedThenIPv4(t *testing.T) {
	cands := []PublicAddress{
		{Address: "2001:db8::1", Family: "ipv6"},
		{Address: "198.51.100.1", Family: "ipv4"},
		{Address: "198.51.100.2", Family: "ipv4"},
	}

	// 1. empty selected with IPv4 available -> first IPv4
	if ip := effectivePublicIP("", cands); ip != "198.51.100.1" {
		t.Errorf("expected 198.51.100.1, got %q", ip)
	}

	// 2. empty selected with only IPv6 -> first IPv6
	if ip := effectivePublicIP("", cands[:1]); ip != "2001:db8::1" {
		t.Errorf("expected 2001:db8::1, got %q", ip)
	}

	// 3. selected non-empty -> selected wins
	if ip := effectivePublicIP("203.0.113.99", cands); ip != "203.0.113.99" {
		t.Errorf("expected 203.0.113.99, got %q", ip)
	}

	// 4. empty candidates and empty selected -> ""
	if ip := effectivePublicIP("", nil); ip != "" {
		t.Errorf("expected empty string, got %q", ip)
	}
}

func TestMigratedManualAddressIsKept(t *testing.T) {
	r, id := machineFixture(t)
	ctx := context.Background()

	// Simulate migration: manually set public_ip and public_ip_selected
	manualIP := "203.0.113.10"
	if _, err := r.Database.Exec("UPDATE nodes SET public_ip=?, public_ip_selected=?, public_ip_manual=1 WHERE id=?", manualIP, manualIP, id); err != nil {
		t.Fatal(err)
	}

	// Node receives heartbeat with reported address 198.51.100.5 (different from manualIP)
	msg := heartbeatMessage(id, 1, time.Now(), "", nil)
	msg.GetHeartbeat().PublicAddresses = []*pb.PublicAddress{{Address: "198.51.100.5", Interface: "eth0"}}
	if _, err := r.Record(ctx, id, msg); err != nil {
		t.Fatal(err)
	}

	// Read node
	n, err := r.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.PublicIP != manualIP {
		t.Errorf("expected PublicIP %q, got %q", manualIP, n.PublicIP)
	}
	if n.PublicIPSelected != manualIP {
		t.Errorf("expected PublicIPSelected %q, got %q", manualIP, n.PublicIPSelected)
	}
	if !n.PublicIPSelectionMissing {
		t.Errorf("expected PublicIPSelectionMissing to be true when manual IP is not in candidates")
	}
	if n.PublicIPSource != "selected" {
		t.Errorf("expected PublicIPSource 'selected', got %q", n.PublicIPSource)
	}
}
