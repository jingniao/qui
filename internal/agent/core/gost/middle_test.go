package gost

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
)

func TestMiddleRelayRender(t *testing.T) {
	state := relayState(t)
	state.Inbounds[0].Settings.Forward.Hops = entryState(t, 1).Inbounds[0].Settings.Forward.Hops
	config, rendered := renderState(t, state)
	service := config.Services[0]
	if service.Handler.Type != "relay" || service.Handler.Chain != config.Chains[0].Name || service.Forwarder != nil {
		t.Fatalf("middle service: %+v", service)
	}
	if len(config.Chains) != 1 || len(config.Chains[0].Hops) != 1 {
		t.Fatalf("middle knows more than its next hop: %+v", config.Chains)
	}
	listener := service.Listener.TLS
	dialer := config.Chains[0].Hops[0].Nodes[0].Dialer.TLS
	if filepath.Dir(listener.CertFile) == filepath.Dir(dialer.CertFile) {
		t.Fatal("previous and next leg share a material directory")
	}
	for path, want := range map[string]string{
		listener.CertFile: state.Inbounds[0].Settings.Forward.Relay.CertificatePEM,
		dialer.CertFile:   state.Inbounds[0].Settings.Forward.Hops[0].ClientCertPEM,
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("material %s: %v", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		// Windows does not expose Unix permission bits.
		if os.PathSeparator == '/' && info.Mode().Perm() != 0600 {
			t.Fatalf("material permissions: %v", info.Mode())
		}
	}
	if len(rendered.HealthEndpoints) != 1 || rendered.HealthEndpoints[0] != "127.0.0.1:22000" || len(rendered.UDPHealthEndpoints) != 0 {
		t.Fatalf("relay over TLS needs one TCP health endpoint: %+v", rendered)
	}
	if len(forwardServiceNames(config)) != 0 {
		t.Fatal("middle relay bytes included in node totals")
	}
	service.Handler.Chain = "missing"
	config.Services[0] = service
	if err := validateReferences(config); err == nil {
		t.Fatal("accepted missing middle chain")
	}
	service.Handler.Chain = config.Chains[0].Name
	config.Services[0] = service
	config.Chains[0].Hops = append(config.Chains[0].Hops, config.Chains[0].Hops[0])
	if err := validateReferences(config); err == nil {
		t.Fatal("accepted two next hops on a middle listener")
	}
}

// This fixture freezes the contract-3 two-hop entry plus exit render. Only
// material paths (random test certificates) and the runtime directory are
// normalized. JSON bytes and YAML bytes must both equal the frozen shape.
func TestContract3RenderUnchanged(t *testing.T) {
	state := entryState(t, 2)
	state.Inbounds[0].Settings.Forward.Network = "tcp,udp"
	state.Inbounds = append(state.Inbounds, relayState(t).Inbounds...)
	adapter := &Adapter{Directory: t.TempDir()}
	rendered, err := adapter.Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	config := goldenConfig(t, rendered.Data)
	normalize := func(tls *TLS) {
		tls.CertFile, tls.KeyFile, tls.CAFile = "cert.pem", "key.pem", "ca.pem"
	}
	for _, service := range config.Services {
		if service.Listener.TLS != nil {
			normalize(service.Listener.TLS)
		}
	}
	for _, chain := range config.Chains {
		for _, hop := range chain.Hops {
			for _, node := range hop.Nodes {
				normalize(node.Dialer.TLS)
			}
		}
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "contract3-relay.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, fixture); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, compact.Bytes()) {
		t.Fatalf("contract-3 JSON changed:\n%s", got)
	}
	var want Config
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(goldenYAML(t, config), goldenYAML(t, want)) {
		t.Fatal("contract-3 YAML changed")
	}
	if err := state.Inbounds[0].Settings.Forward.ValidateGostContract(3); err != nil {
		t.Fatal(err)
	}
	if desiredstate.GostMaxRelayHops != 2 {
		t.Fatal("legacy chain limit changed")
	}
}
