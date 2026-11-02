package gost

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

// TestRealForwardPoolsFailoverAndLimits is the G2 native suite. It runs the pinned
// binary against real TCP and UDP echo targets and checks what only a real core can
// show: which pool member answers, that a fifo pool survives a dead member, that a
// connection cap accepts and then closes, that both byte rates are enforced, and
// that the node counter reports the direction the bytes went.
func TestRealForwardPoolsFailoverAndLimits(t *testing.T) {
	binary := os.Getenv("GOST_BINARY")
	if binary == "" {
		t.Skip("GOST_BINARY is unset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: "node-gost-g2"}
	saved := []traffic.Counter{}
	adapter.ConfigureRuntime(core.RuntimeHooks{TrafficSink: func(_ context.Context, counters []traffic.Counter) error {
		saved = append(saved, counters...)
		return nil
	}})
	defer adapter.Close()
	apply := func(t *testing.T, state desiredstate.State) {
		t.Helper()
		rendered, err := adapter.Render(ctx, state)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
			t.Fatalf("%+v %v %q", result, err, adapter.log.tail())
		}
	}
	first, stopFirst := taggedTarget(t, 'A')
	second, _ := taggedTarget(t, 'B')
	poolState := func(revision int64, port int, targets []desiredstate.ForwardTarget, strategy string) desiredstate.State {
		strategy, failTimeout, maxFails := strategy, "1s", 1
		return desiredstate.State{
			SchemaVersion: 1, Revision: revision, NodeID: "node-gost-g2", Logging: desiredstate.Logging{DomainOnly: true},
			Inbounds: []desiredstate.Inbound{{
				Core: CoreName, ID: "pool", Protocol: "forward", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct",
				Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{
					Network: "tcp,udp", Targets: targets, Strategy: &strategy, MaxFails: &maxFails, FailTimeout: &failTimeout,
				}},
			}},
			Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
		}
	}
	t.Run("round robin reaches every member", func(t *testing.T) {
		port := freePort(t)
		apply(t, poolState(1, port, []desiredstate.ForwardTarget{{Host: "127.0.0.1", Port: first}, {Host: "127.0.0.1", Port: second}}, "round"))
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		seen := map[byte]bool{}
		deadline := time.Now().Add(20 * time.Second)
		for attempts := 0; attempts < 12 && len(seen) < 2 && time.Now().Before(deadline); attempts++ {
			if tag, err := banner(address); err == nil && tag != 0 {
				seen[tag] = true
			}
		}
		if !seen['A'] || !seen['B'] {
			t.Fatalf("round robin used %v", seen)
		}
		// UDP is delivered to the pool as well; a missing responder is not an error
		// here, only a missing answer would be.
		packet, err := net.DialTimeout("udp", address, 3*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer packet.Close()
		packet.SetDeadline(time.Now().Add(5 * time.Second))
		packet.Write([]byte("udp"))
		if read, err := packet.Read(make([]byte, 16)); err != nil || read == 0 {
			t.Fatalf("udp forward answer: %d %v", read, err)
		}
	})
	t.Run("fifo fails over after one failed attempt", func(t *testing.T) {
		port := freePort(t)
		apply(t, poolState(2, port, []desiredstate.ForwardTarget{{Host: "127.0.0.1", Port: first}, {Host: "127.0.0.1", Port: second}}, "fifo"))
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		for attempt := 0; attempt < 3; attempt++ {
			if tag, err := banner(address); err != nil || tag != 'A' {
				t.Fatalf("fifo did not stay on the first member: %q %v", tag, err)
			}
		}
		// Stop the first member. G0 measured that the switch costs one failed
		// attempt per failure window, so failures are counted, not forbidden.
		stopFirst()
		failed, switched := 0, false
		deadline := time.Now().Add(30 * time.Second)
		for attempt := 0; attempt < 20 && time.Now().Before(deadline); attempt++ {
			tag, err := banner(address)
			if err != nil || tag == 0 {
				failed++
				time.Sleep(500 * time.Millisecond)
				continue
			}
			if tag == 'B' {
				switched = true
				break
			}
		}
		if !switched {
			t.Fatalf("fifo never failed over (failed attempts: %d)", failed)
		}
		if failed > 3 {
			t.Fatalf("failover cost %d failed attempts", failed)
		}
	})
	t.Run("connection cap accepts then closes", func(t *testing.T) {
		port := freePort(t)
		state := poolState(3, port, []desiredstate.ForwardTarget{{Host: "127.0.0.1", Port: second}}, "round")
		cap := 2
		state.Inbounds[0].Settings.Forward.MaxConnections = &cap
		apply(t, state)
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		held := []net.Conn{}
		// The Agent's listener health dial briefly occupies a slot too, so a held
		// connection that loses that race is closed and dialled again.
		deadline := time.Now().Add(30 * time.Second)
		for index := 0; index < cap; {
			connection, err := net.DialTimeout("tcp", address, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			connection.SetDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.ReadFull(connection, make([]byte, 1)); err != nil {
				connection.Close()
				if time.Now().After(deadline) {
					t.Fatalf("held connection %d: %v", index, err)
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}
			defer connection.Close()
			held = append(held, connection)
			index++
		}
		// G0 measured that the over-limit connection is accepted and then closed,
		// so the handshake may succeed and the request must not.
		over, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err == nil {
			over.SetDeadline(time.Now().Add(10 * time.Second))
			over.Write([]byte("ping"))
			if _, err := io.ReadFull(over, make([]byte, 6)); err == nil {
				over.Close()
				t.Fatal("a connection over the cap was served")
			}
			over.Close()
		}
	})
}

// TestRealForwardRateLimitsAndNodeCounter checks the two byte rates in each
// direction and the direction the node counter reports. GOST's limiter starts with
// a burst of one second, so the assertion is an average over a multi-MiB transfer,
// not an instantaneous rate.
func TestRealForwardRateLimitsAndNodeCounter(t *testing.T) {
	binary := os.Getenv("GOST_BINARY")
	if binary == "" {
		t.Skip("GOST_BINARY is unset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	adapter := &Adapter{Path: binary, Directory: t.TempDir(), NodeID: "node-gost-rate"}
	saved := []traffic.Counter{}
	adapter.ConfigureRuntime(core.RuntimeHooks{TrafficSink: func(_ context.Context, counters []traffic.Counter) error {
		saved = append(saved, counters...)
		return nil
	}})
	defer adapter.Close()
	const (
		rate    = int64(1024 * 1024)
		payload = int64(3 * 1024 * 1024)
	)
	source := sourceTarget(t, payload)
	sink, received := sinkTarget(t)
	download, upload := freePort(t), freePort(t)
	rateValue := rate
	inbound := func(id string, port, target int) desiredstate.Inbound {
		return desiredstate.Inbound{
			Core: CoreName, ID: id, Protocol: "forward", Listen: "127.0.0.1", Port: port, Enabled: true, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{
				Host: "127.0.0.1", Port: target, Network: "tcp", BandwidthInBps: &rateValue, BandwidthOutBps: &rateValue,
			}},
		}
	}
	state := desiredstate.State{
		SchemaVersion: 1, Revision: 1, NodeID: "node-gost-rate", Logging: desiredstate.Logging{DomainOnly: true},
		Inbounds:  []desiredstate.Inbound{inbound("download", download, source), inbound("upload", upload, sink)},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
	rendered, err := adapter.Render(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.Apply(ctx, rendered); err != nil || !result.Success {
		t.Fatalf("%+v %v %q", result, err, adapter.log.tail())
	}
	// target to client: read the payload and measure the average rate.
	started := time.Now()
	got := 0
	if connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(download)), 5*time.Second); err != nil {
		t.Fatal(err)
	} else {
		connection.SetDeadline(time.Now().Add(90 * time.Second))
		written, err := io.Copy(io.Discard, connection)
		connection.Close()
		if err != nil || written != payload {
			t.Fatalf("download read %d/%d bytes: %v", written, payload, err)
		}
		got = int(written)
	}
	elapsed := time.Since(started)
	// The burst is one second of bytes, so the remaining payload is paced. A
	// generous ceiling still fails loudly when no limit is applied at all.
	if !paced(int64(got), rate, elapsed) {
		t.Fatalf("download of %d bytes took %s at a limit of %d B/s", got, elapsed, rate)
	}
	// client to target: the same limit in the other direction.
	started = time.Now()
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(upload)), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	connection.SetDeadline(time.Now().Add(90 * time.Second))
	chunk := make([]byte, 256*1024)
	for written := int64(0); written < payload; {
		size := int64(len(chunk))
		if payload-written < size {
			size = payload - written
		}
		if _, err := connection.Write(chunk[:size]); err != nil {
			t.Fatalf("upload write: %v", err)
		}
		written += size
	}
	connection.Close()
	// The client's writes land in socket buffers at once; the limit shows where the
	// bytes arrive, so the clock stops when the target has read all of them. The
	// Agent's listener health dials reach the target too, as empty sessions.
	timeout := time.After(90 * time.Second)
	for got := int64(0); got != payload; {
		select {
		case got = <-received:
		case <-timeout:
			t.Fatal("upload never reached the target")
		}
	}
	elapsed = time.Since(started)
	if !paced(payload, rate, elapsed) {
		t.Fatalf("upload of %d bytes took %s at a limit of %d B/s", payload, elapsed, rate)
	}
	// Input maps to upload and output to download: the payload travelled from the
	// target to the client, so the node counter must report more download bytes.
	if err := adapter.SampleTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Kind != "node" || saved[0].UploadBytes == 0 || saved[0].DownloadBytes <= saved[0].UploadBytes {
		t.Fatalf("node counter direction: %+v", saved)
	}
}

// taggedTarget answers TCP and UDP on one loopback port with its tag as the first
// byte and echoes what it reads afterwards, so a test can tell which pool member
// served a request. The returned function stops the target the way an outage would.
func taggedTarget(t *testing.T, tag byte) (int, func()) {
	t.Helper()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := tcp.Addr().(*net.TCPAddr).Port
	udp, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		tcp.Close()
		t.Fatal(err)
	}
	stop := func() {
		tcp.Close()
		udp.Close()
	}
	t.Cleanup(stop)
	go func() {
		for {
			connection, err := tcp.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				if _, err := connection.Write([]byte{tag}); err != nil {
					return
				}
				io.Copy(connection, connection)
			}()
		}
	}()
	go func() {
		buffer := make([]byte, 1500)
		for {
			read, from, err := udp.ReadFrom(buffer)
			if err != nil {
				return
			}
			reply := append([]byte{tag}, buffer[:read]...)
			udp.WriteTo(reply, from)
		}
	}()
	return port, stop
}

// banner dials a forwarded listener and returns the tag its target answered with.
func banner(address string) (byte, error) {
	connection, err := net.DialTimeout("tcp", address, 3*time.Second)
	if err != nil {
		return 0, err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(5 * time.Second))
	var tag [1]byte
	if _, err := io.ReadFull(connection, tag[:]); err != nil {
		return 0, err
	}
	return tag[0], nil
}

// sourceTarget sends a fixed payload to every connection and closes it, so the
// forward's output direction carries the bytes.
func sourceTarget(t *testing.T, payload int64) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		chunk := make([]byte, 256*1024)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				for sent := int64(0); sent < payload; {
					size := int64(len(chunk))
					if payload-sent < size {
						size = payload - sent
					}
					if _, err := connection.Write(chunk[:size]); err != nil {
						return
					}
					sent += size
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

// sinkTarget reads and discards everything, so the forward's input direction
// carries the bytes and nothing comes back.
func sinkTarget(t *testing.T) (int, <-chan int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan int64, 16)
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				n, _ := io.Copy(io.Discard, connection)
				received <- n
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, received
}

// paced reports whether a transfer took about as long as the limit allows. GOST's
// limiter starts with a burst of one second of bytes, so only the remainder is
// paced; the upper bound only catches a stall, not a slow test machine.
func paced(bytes, rate int64, elapsed time.Duration) bool {
	floor := time.Duration(float64(bytes-rate) / float64(rate) * 0.75 * float64(time.Second))
	return elapsed >= floor && elapsed <= 3*time.Duration(bytes/rate)*time.Second+5*time.Second
}
