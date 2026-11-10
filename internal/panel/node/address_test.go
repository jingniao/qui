package node

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/peer"
)

// recordPublicHeartbeat stores one heartbeat carrying the address the Agent
// detected, exactly as the control stream does.
func recordPublicHeartbeat(t *testing.T, r *Repository, ctx context.Context, id string, sequence uint64, publicIP string) {
	t.Helper()
	message := heartbeatMessage(id, sequence, time.Now(), "", nil)
	message.GetHeartbeat().PublicIp = publicIP
	if _, err := r.Record(ctx, id, message); err != nil {
		t.Fatal(err)
	}
}

func storedNode(t *testing.T, r *Repository, id string) Node {
	t.Helper()
	n, err := r.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A detected address is used while nothing manual is stored, and a value the
// Owner typed is never replaced. Clearing it drops the manual mark, so the next
// heartbeat takes over again.
func TestHeartbeatFillsPublicIPUnlessItIsManual(t *testing.T) {
	ctx := context.Background()
	r, id := machineFixture(t)
	recordPublicHeartbeat(t, r, ctx, id, 1, "203.0.113.10")
	if n := storedNode(t, r, id); n.PublicIP != "203.0.113.10" || n.PublicIPSource != "auto" {
		t.Fatalf("detected address: %+v", n)
	}
	manual := "198.51.100.7"
	if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true, PublicIP: &manual}); err != nil {
		t.Fatal(err)
	}
	if n := storedNode(t, r, id); n.PublicIP != manual || n.PublicIPSource != "selected" {
		t.Fatalf("manual address: %+v", n)
	}
	recordPublicHeartbeat(t, r, ctx, id, 2, "203.0.113.11")
	if n := storedNode(t, r, id); n.PublicIP != manual || n.PublicIPSource != "selected" {
		t.Fatalf("a heartbeat replaced the manual address: %+v", n)
	}
	empty := ""
	if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true, PublicIP: &empty}); err != nil {
		t.Fatal(err)
	}
	if n := storedNode(t, r, id); n.PublicIP != "" || n.PublicIPSource != "" {
		t.Fatalf("cleared address: %+v", n)
	}
	recordPublicHeartbeat(t, r, ctx, id, 3, "203.0.113.12")
	if n := storedNode(t, r, id); n.PublicIP != "203.0.113.12" || n.PublicIPSource != "auto" {
		t.Fatalf("detection did not resume: %+v", n)
	}
}

// An Agent that cannot detect its own address is placed by the address it opened
// the control connection from, but only when that address is public: a loopback
// or private peer says nothing about how a client could reach the node.
func TestHeartbeatFallsBackToAPublicPeerAddress(t *testing.T) {
	for _, test := range []struct{ name, peer, want string }{
		{name: "public peer", peer: "203.0.113.9", want: "203.0.113.9"},
		{name: "loopback peer", peer: "127.0.0.1"},
		{name: "private peer", peer: "10.1.2.3"},
		{name: "cgnat peer", peer: "100.64.7.7"},
		{name: "link-local peer", peer: "169.254.7.7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, id := machineFixture(t)
			ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP(test.peer), Port: 51234}})
			recordPublicHeartbeat(t, r, ctx, id, 1, "")
			if n := storedNode(t, r, id); n.PublicIP != test.want || n.PublicIPSource != publicIPSource(test.want, "") {
				t.Fatalf("peer fallback: %+v", n)
			}
		})
	}
}

// The connect address is a domain or an IP literal without a scheme, port or
// brackets; an omitted field keeps the stored value.
func TestConnectHostAcceptsNamesAndLiteralsOnly(t *testing.T) {
	ctx := context.Background()
	r, id := machineFixture(t)
	for _, value := range []string{"edge.example.com", "203.0.113.7", "2001:db8::1"} {
		if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true, ConnectHost: &value}); err != nil {
			t.Fatalf("%q refused: %v", value, err)
		}
		if n := storedNode(t, r, id); n.ConnectHost != value || n.ConnectAddress() != value {
			t.Fatalf("%q not stored: %+v", value, n)
		}
	}
	for _, value := range []string{"http://x", "a:80", "[::1]", "bad host", ".edge.example.com", "edge.example.com:443"} {
		if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true, ConnectHost: &value}); !errors.Is(err, ErrSettings) {
			t.Fatalf("%q accepted: %v", value, err)
		}
	}
	// An omitted field changes nothing; the empty string clears it, and the
	// public address becomes the client address again.
	if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if n := storedNode(t, r, id); n.ConnectHost != "2001:db8::1" {
		t.Fatalf("an omitted connect host was cleared: %+v", n)
	}
	public := "203.0.113.20"
	if err := r.UpdateSettings(ctx, id, SettingsInput{Name: "machine", Enabled: true, ConnectHost: new(string), PublicIP: &public}); err != nil {
		t.Fatal(err)
	}
	n := storedNode(t, r, id)
	if n.ConnectHost != "" || n.ConnectAddress() != public || n.PublicIPSource != "selected" {
		t.Fatalf("fallback to the public address: %+v", n)
	}
}
