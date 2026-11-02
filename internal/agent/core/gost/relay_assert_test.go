package gost

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The exit half is a relay handler on a TLS listener that requires a client
// certificate; the entry half keeps its target pool and carries the chain on the
// handler, never at service level. GOST silently forwards directly when it finds
// no chain there, so the shape is asserted structurally.
func TestRelayAndChainRenderShape(t *testing.T) {
	config, rendered := renderState(t, relayState(t))
	if len(config.Services) != 1 {
		t.Fatalf("relay state rendered %d services", len(config.Services))
	}
	service := config.Services[0]
	if service.Handler.Type != relayHandler || service.Forwarder != nil {
		t.Fatalf("relay service %+v", service)
	}
	if service.Listener.Type != tlsListener || service.Listener.TLS == nil || service.Listener.TLS.CAFile == "" || service.Listener.TLS.ServerName != "" {
		t.Fatalf("relay listener %+v", service.Listener)
	}
	if len(rendered.HealthEndpoints) != 1 {
		t.Fatalf("relay listener is not health checked: %v", rendered.HealthEndpoints)
	}
	if forwardServiceNames(config)[service.Name] {
		t.Fatal("relay service entered the node traffic totals")
	}

	for _, hops := range []int{1, 2} {
		config, _ = renderState(t, entryState(t, hops))
		if len(config.Chains) != 1 || len(config.Chains[0].Hops) != hops {
			t.Fatalf("%d hop chain: %+v", hops, config.Chains)
		}
		service := config.Services[0]
		if service.Handler.Chain != chainName("fwd-entry") {
			t.Fatalf("chain is not attached to the handler: %+v", service.Handler)
		}
		if service.Forwarder == nil || len(service.Forwarder.Nodes) != 1 {
			t.Fatalf("entry target pool lost: %+v", service.Forwarder)
		}
		for _, hop := range config.Chains[0].Hops {
			node := hop.Nodes[0]
			if node.Connector.Type != relayConnector || node.Dialer.Type != tlsListener || node.Dialer.TLS.CAFile == "" || node.Dialer.TLS.ServerName == "" {
				t.Fatalf("hop is not a verified relay dialer: %+v", node)
			}
			if node.Addr == service.Forwarder.Nodes[0].Addr {
				t.Fatal("the target pool member became the relay address")
			}
		}
	}
}

// The material files are 0600 under runtime/gost/tls and referenced by absolute
// path, so GOST reads exactly the set the revision carried.
func TestRelayMaterialFilesArePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows reports synthetic permission bits, so the 0600 assertion only
		// means something on the Linux test machine and on a node.
		t.Skip("file modes are not meaningful on Windows")
	}
	config, _ := renderState(t, relayState(t))
	service := config.Services[0]
	for _, path := range []string{service.Listener.TLS.CertFile, service.Listener.TLS.KeyFile, service.Listener.TLS.CAFile} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s has mode %v", path, info.Mode().Perm())
		}
		if !strings.HasPrefix(path, filepath.Join(filepath.Dir(config.Metrics.Addr[len("unix://"):]), materialDir)) {
			t.Fatalf("%s is outside the material directory", path)
		}
	}
}

// A hop without a CA file would silently become an unverified TLS dialer, so the
// renderer refuses the state instead of rendering one.
func TestUnverifiedHopIsRefused(t *testing.T) {
	state := entryState(t, 1)
	state.Inbounds[0].Settings.Forward.Hops[0].CACertPEM = ""
	if err := state.Validate(); err == nil {
		t.Fatal("a hop without a CA was accepted")
	}
}
