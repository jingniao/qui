package gost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
)

// relayPayload is what every echo in this suite sends and expects back.
const relayPayload = "gost-relay"

// TestRealRelayCarriesForwardsAndRejectsUntrustedClients is the G3 native suite.
// It runs the pinned binary twice on one host: an exit that renders a relay
// listener requiring a client certificate, and an entry whose forward dials its
// target through that relay. Only the real binary can show what the renderer
// cannot: that the payload really traverses the relay over TCP and UDP, and that a
// hop without a client certificate, or with one from another CA, is answered with
// nothing. G0 measured that such a rejection arrives as an empty reply rather than
// a refused connection, so both refusals are asserted with bounded attempts.
func TestRealRelayCarriesForwardsAndRejectsUntrustedClients(t *testing.T) {
	binary := os.Getenv("GOST_BINARY")
	if binary == "" {
		t.Skip("GOST_BINARY is unset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	const serverName = "t-test.gost.qui.internal"
	ca, server, client := testCA(t, serverName)
	_, _, rogue := testCA(t, serverName)
	target := echoTarget(t)
	relayPort, entryPort := freePort(t), freePort(t)
	// Two adapters, two runtime directories: the exit listens and the entry dials,
	// as two hosts would.
	exit := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: "node-gost-relay-exit"}
	defer exit.Close()
	entry := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: "node-gost-relay-entry"}
	defer entry.Close()
	apply := func(t *testing.T, adapter *Adapter, state desiredstate.State) core.RenderedConfig {
		t.Helper()
		rendered, err := adapter.Render(ctx, state)
		if err != nil {
			t.Fatalf("render revision %d: %v", state.Revision, err)
		}
		if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
			t.Fatalf("apply revision %d: %+v %v %q", state.Revision, result, err, adapter.log.tail())
		}
		return rendered
	}
	apply(t, exit, realRelayState(1, relayPort, desiredstate.GostRelayTLS{CertificatePEM: server.cert, PrivateKeyPEM: server.key, ClientCAPEM: ca.cert}))
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(entryPort))
	hop := desiredstate.GostHop{Host: "127.0.0.1", Port: relayPort, ServerName: serverName, CACertPEM: ca.cert, ClientCertPEM: client.cert, ClientKeyPEM: client.key}
	apply(t, entry, realEntryState(1, entryPort, target, []desiredstate.GostHop{hop}))
	requireEcho(t, address)

	// A certificate signed by another CA is not trusted: the relay's client CA is
	// what admits the entry node, and the exit keeps serving its own revision.
	foreign := hop
	foreign.ClientCertPEM, foreign.ClientKeyPEM = rogue.cert, rogue.key
	apply(t, entry, realEntryState(2, entryPort, target, []desiredstate.GostHop{foreign}))
	requireNoEcho(t, address)

	// The Agent refuses a hop without client material when it validates the desired
	// state, so the only way to reach the relay without a certificate is to strip it
	// from a rendered configuration.
	rendered := apply(t, entry, realEntryState(3, entryPort, target, []desiredstate.GostHop{hop}))
	if result, err := entry.Apply(ctx, withoutClientCertificate(t, rendered)); err != nil || !result.Success {
		t.Fatalf("apply without a client certificate: %+v %v %q", result, err, entry.log.tail())
	}
	requireNoEcho(t, address)
}

// realRelayState is the exit half of one tunnel: a relay handler on a TLS listener
// that requires the client certificate of the entry node.
func realRelayState(revision int64, port int, material desiredstate.GostRelayTLS) desiredstate.State {
	return desiredstate.State{
		SchemaVersion: 1, Revision: revision, NodeID: "node-gost-relay-exit", Logging: desiredstate.Logging{DomainOnly: true},
		Inbounds: []desiredstate.Inbound{{
			Core: CoreName, ID: "relay-exit", Protocol: "forward", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Mode: desiredstate.GostForwardModeRelay, Relay: &material}},
		}},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
}

// realEntryState is the entry half: a forward that keeps its own target pool and
// reaches it through the ordered hops.
func realEntryState(revision int64, port, target int, hops []desiredstate.GostHop) desiredstate.State {
	return desiredstate.State{
		SchemaVersion: 1, Revision: revision, NodeID: "node-gost-relay-entry", Logging: desiredstate.Logging{DomainOnly: true},
		Users:       []desiredstate.User{},
		Credentials: []desiredstate.Credential{},
		Inbounds: []desiredstate.Inbound{{
			Core: CoreName, ID: "fwd-entry", Protocol: "forward", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "127.0.0.1", Port: target, Network: "tcp,udp", Hops: hops}},
		}},
		Outbounds:      []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
		OutboundGroups: []desiredstate.OutboundGroup{},
		ProxyChains:    []desiredstate.ProxyChain{},
		Policies:       []desiredstate.Policy{},
	}
}

// withoutClientCertificate drops the client key pair of every hop from a rendered
// entry configuration, leaving the dialer with its CA and server name only.
func withoutClientCertificate(t *testing.T, rendered core.RenderedConfig) core.RenderedConfig {
	t.Helper()
	config := Config{}
	if err := json.Unmarshal(rendered.Data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Chains) != 1 || len(config.Chains[0].Hops) != 1 || len(config.Chains[0].Hops[0].Nodes) != 1 || config.Chains[0].Hops[0].Nodes[0].Dialer.TLS == nil {
		t.Fatalf("rendered entry has no single verified hop: %+v", config.Chains)
	}
	dialer := config.Chains[0].Hops[0].Nodes[0].Dialer.TLS
	dialer.CertFile, dialer.KeyFile = "", ""
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	rendered.Data = data
	return rendered
}

// requireEcho waits until a forwarded listener answers over TCP and UDP and fails
// with the last error otherwise. The Agent's own health dials reach the target as
// empty sessions, so a single attempt could race a listener that has just settled.
func requireEcho(t *testing.T, address string) {
	t.Helper()
	if err := pollEcho(tcpEcho, address); err != nil {
		t.Fatalf("tcp echo through the relay: %v", err)
	}
	if err := pollEcho(udpEcho, address); err != nil {
		t.Fatalf("udp echo through the relay: %v", err)
	}
}

// requireNoEcho requires that neither transport carries an echo through the entry
// listener. A rejection cannot prove itself with one attempt, so the attempts are
// bounded and every one of them must come back empty.
func requireNoEcho(t *testing.T, address string) {
	t.Helper()
	for attempt := 0; attempt < 5; attempt++ {
		if err := tcpEcho(address); err == nil {
			t.Fatalf("tcp echo reached the target on attempt %d without a usable client certificate", attempt)
		}
		if err := udpEcho(address); err == nil {
			t.Fatalf("udp echo reached the target on attempt %d without a usable client certificate", attempt)
		}
	}
}

// pollEcho repeats one probe until it answers or the deadline passes.
func pollEcho(probe func(address string) error, address string) error {
	var last error
	deadline := time.Now().Add(20 * time.Second)
	for attempt := 0; attempt < 20; attempt++ {
		if last = probe(address); last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	return last
}

// tcpEcho writes the payload through a forwarded listener and requires it back.
func tcpEcho(address string) error {
	connection, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := connection.Write([]byte(relayPayload)); err != nil {
		return err
	}
	answered := make([]byte, len(relayPayload))
	if _, err := io.ReadFull(connection, answered); err != nil {
		return err
	}
	if string(answered) != relayPayload {
		return fmt.Errorf("tcp echo %q", answered)
	}
	return nil
}

// udpEcho is the same over UDP, where a rejected hop answers nothing at all.
func udpEcho(address string) error {
	packet, err := net.DialTimeout("udp", address, 3*time.Second)
	if err != nil {
		return err
	}
	defer packet.Close()
	packet.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := packet.Write([]byte(relayPayload)); err != nil {
		return err
	}
	answered := make([]byte, 64)
	read, err := packet.Read(answered)
	if err != nil {
		return err
	}
	if string(answered[:read]) != relayPayload {
		return fmt.Errorf("udp echo %q", answered[:read])
	}
	return nil
}
