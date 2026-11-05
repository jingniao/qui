package gost

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"proxycontrol/internal/agent/reconcile"
	"proxycontrol/internal/agent/supervisor"
	"proxycontrol/pkg/contracts/desiredstate"
)

// realTunnelNode retains both the Agent's encrypted revision storage and GOST's
// runtime directory. Reconstructing it exercises Agent startup Restore, while
// killing only its child exercises the actual supervisor without a republish.
type realTunnelNode struct {
	adapter   *Adapter
	manager   *reconcile.Reconciler
	directory string
	state     desiredstate.State
	cancel    context.CancelFunc
	done      chan struct{}
}

func newRealTunnelNode(t *testing.T, ctx context.Context, binary string, state desiredstate.State) *realTunnelNode {
	t.Helper()
	n := &realTunnelNode{directory: t.TempDir(), state: state}
	n.adapter = &Adapter{Path: binary, Directory: t.TempDir(), NodeID: state.NodeID}
	var err error
	n.manager, err = reconcile.New(n.directory, state.NodeID, n.adapter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.stop(t) })
	result, err := n.manager.Apply(ctx, state)
	if err != nil || !result.Success {
		t.Fatalf("apply %s: %+v %v", state.NodeID, result, err)
	}
	n.supervise(ctx)
	return n
}

func (n *realTunnelNode) supervise(ctx context.Context) {
	ctx, n.cancel = context.WithCancel(ctx)
	n.done = make(chan struct{})
	go func() {
		defer close(n.done)
		supervisor.Run(ctx, n.adapter, n.manager, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
}

func (n *realTunnelNode) stop(t *testing.T) {
	t.Helper()
	if n.cancel != nil {
		n.cancel()
		<-n.done
		n.cancel = nil
	}
	if err := n.adapter.Close(); err != nil {
		t.Errorf("close %s: %v", n.state.NodeID, err)
	}
}

func (n *realTunnelNode) restartAgent(t *testing.T, ctx context.Context) {
	t.Helper()
	n.stop(t)
	n.adapter = &Adapter{Path: n.adapter.Path, Directory: n.adapter.Directory, NodeID: n.state.NodeID}
	var err error
	n.manager, err = reconcile.New(n.directory, n.state.NodeID, n.adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.manager.Restore(ctx); err != nil {
		t.Fatalf("startup restore %s: %v", n.state.NodeID, err)
	}
	n.supervise(ctx)
}

func (n *realTunnelNode) killAndWaitForSupervisor(t *testing.T, ctx context.Context) {
	t.Helper()
	before := n.adapter.CoreStartedAt()
	n.adapter.mu.Lock()
	if n.adapter.process == nil {
		n.adapter.mu.Unlock()
		t.Fatal("no GOST process to kill")
	}
	err := n.adapter.process.Process.Kill()
	n.adapter.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatalf("supervisor did not recover %s", n.state.NodeID)
		case <-tick.C:
			if n.adapter.CoreStartedAt().After(before) {
				if health, err := n.adapter.Health(ctx); err == nil && health.Healthy {
					if n.manager.Current() != n.state.Revision {
						t.Fatal("supervisor recovery changed the revision")
					}
					return
				}
			}
		}
	}
}

func TestRealX0TunnelPathsAndRecovery(t *testing.T) {
	binary := os.Getenv("GOST_BINARY")
	if binary == "" {
		t.Skip("GOST_BINARY is unset")
	}
	for _, middle := range []bool{false, true} {
		name := "entry-exit"
		if middle {
			name = "entry-mid-exit"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
			defer cancel()
			// Both leaves on a leg are distinct. The two test CAs deliberately
			// differ, proving that the middle listener and dialer do not mix roots.
			const exitName = "exit.x0.test"
			ca, server, client := testCA(t, exitName)
			exitPort := freePort(t)
			exit := newRealTunnelNode(t, ctx, binary, realRelayState(1, exitPort, desiredstate.GostRelayTLS{CertificatePEM: server.cert, PrivateKeyPEM: server.key, ClientCAPEM: ca.cert}))
			hop := desiredstate.GostHop{Host: "127.0.0.1", Port: exitPort, ServerName: exitName, CACertPEM: ca.cert, ClientCertPEM: client.cert, ClientKeyPEM: client.key}
			nodes := []*realTunnelNode{exit}
			if middle {
				const midName = "mid.x0.test"
				midCA, midServer, midClient := testCA(t, midName)
				midPort := freePort(t)
				state := realRelayState(1, midPort, desiredstate.GostRelayTLS{CertificatePEM: midServer.cert, PrivateKeyPEM: midServer.key, ClientCAPEM: midCA.cert})
				state.NodeID, state.Inbounds[0].ID = "node-gost-mid", "relay-mid"
				state.Inbounds[0].Settings.Forward.Hops = []desiredstate.GostHop{hop}
				nodes = append(nodes, newRealTunnelNode(t, ctx, binary, state))
				hop = desiredstate.GostHop{Host: "127.0.0.1", Port: midPort, ServerName: midName, CACertPEM: midCA.cert, ClientCertPEM: midClient.cert, ClientKeyPEM: midClient.key}
			}
			// The target is loopback on the exit host, as required when the
			// destination inbound and tunnel exit are on the same server.
			port := freePort(t)
			entry := newRealTunnelNode(t, ctx, binary, realEntryState(1, port, echoTarget(t), []desiredstate.GostHop{hop}))
			nodes = append(nodes, entry)
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
			requireEcho(t, address)
			for _, node := range nodes {
				// No new desired state is published in either recovery path.
				entryStarted := entry.adapter.CoreStartedAt()
				node.killAndWaitForSupervisor(t, ctx)
				requireEcho(t, address)
				if node != entry && !entry.adapter.CoreStartedAt().Equal(entryStarted) {
					t.Fatal("entry restarted during downstream recovery")
				}
				if node != entry {
					// All simulated hosts share loopback. Without this negative
					// check, an ignored handler.chain could dial the local target
					// directly and make every positive echo pass accidentally.
					node.stop(t)
					if err := tcpEcho(address); err == nil {
						t.Fatal("TCP bypassed a stopped downstream relay")
					}
					if err := udpEcho(address); err == nil {
						t.Fatal("UDP bypassed a stopped downstream relay")
					}
				}
				node.restartAgent(t, ctx)
				requireEcho(t, address)
				if node != entry && !entry.adapter.CoreStartedAt().Equal(entryStarted) {
					t.Fatal("entry restarted during downstream Agent restart")
				}
			}
		})
	}
	t.Run("direct-forward", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		port := freePort(t)
		node := newRealTunnelNode(t, ctx, binary, realEntryState(1, port, echoTarget(t), nil))
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		requireEcho(t, address)
		node.killAndWaitForSupervisor(t, ctx)
		requireEcho(t, address)
		node.restartAgent(t, ctx)
		requireEcho(t, address)
	})
}
