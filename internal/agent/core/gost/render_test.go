package gost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"proxycontrol/pkg/contracts/desiredstate"
)

// goldenDirectory is the runtime directory the golden files were produced with.
const goldenDirectory = "/var/lib/proxy-agent/runtime/gost"

// goldenMetricsAddress replaces the rendered socket address in the golden files: it
// embeds the runtime directory, so it is the one field whose value depends on the
// platform separator. The round-trip test asserts the real value instead.
const goldenMetricsAddress = "unix:///var/lib/proxy-agent/runtime/gost/metrics.sock"

func forwardInbound(id, listen string, port int, host string, target int, network string, enabled bool) desiredstate.Inbound {
	return desiredstate.Inbound{Core: "gost", ID: id, Protocol: "forward", Listen: listen, Port: port, Enabled: enabled, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: host, Port: target, Network: network}}}
}

// goldenState is one TCP forward, one forward that serves TCP and UDP, and one
// disabled forward that must not render anything.
func goldenState() desiredstate.State {
	return desiredstate.State{
		SchemaVersion: 1,
		Revision:      7,
		NodeID:        "node-gost",
		Logging:       desiredstate.Logging{DomainOnly: true},
		Inbounds: []desiredstate.Inbound{
			forwardInbound("tcp-forward", "0.0.0.0", 20000, "203.0.113.10", 8443, "tcp", true),
			forwardInbound("both-forward", "::", 20100, "target.example.net", 53, "tcp,udp", true),
			forwardInbound("off-forward", "127.0.0.1", 20200, "203.0.113.11", 9000, "udp", false),
		},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
}

// TestRenderGolden pins the revision's native JSON and the YAML the process reads.
// Both come from the same typed configuration, so a golden mismatch means the
// renderer or the encoding changed.
func TestRenderGolden(t *testing.T) {
	adapter := &Adapter{Directory: goldenDirectory}
	rendered, err := adapter.Render(context.Background(), goldenState())
	if err != nil {
		t.Fatal(err)
	}
	config := goldenConfig(t, rendered.Data)
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, golden := range []struct {
		name string
		data []byte
	}{{"forward.json", encoded}, {"forward.yaml", goldenYAML(t, config)}} {
		path := filepath.Join("testdata", golden.name)
		if os.Getenv("UPDATE_GOST_GOLDEN") == "1" {
			if err := os.MkdirAll("testdata", 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, golden.data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(golden.data) {
			t.Fatalf("%s mismatch:\n%s", golden.name, golden.data)
		}
	}
}

// goldenConfig decodes a rendered revision with the platform dependent socket
// address replaced by the fixed golden value.
func goldenConfig(t *testing.T, data []byte) Config {
	t.Helper()
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config.Metrics.Addr = goldenMetricsAddress
	return config
}

func goldenYAML(t *testing.T, config Config) []byte {
	t.Helper()
	data, err := EncodeYAML(config)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestRenderHealthEndpointsFollowTheTransport pins which listener each rendered
// service contributes: a TCP dial for the TCP service, a socket-ownership check for
// the UDP one, and a loopback dial address for a wildcard bind.
func TestRenderHealthEndpointsFollowTheTransport(t *testing.T) {
	rendered, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), goldenState())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rendered.HealthEndpoints, ",") != "127.0.0.1:20000,[::1]:20100" {
		t.Fatalf("tcp health endpoints: %v", rendered.HealthEndpoints)
	}
	if strings.Join(rendered.UDPHealthEndpoints, ",") != "[::1]:20100" {
		t.Fatalf("udp health endpoints: %v", rendered.UDPHealthEndpoints)
	}
}

// TestConfigRoundTripsThroughJSONAndYAML proves that the revision storage and the
// process file describe the same configuration: the JSON native form is what the
// revision keeps, and the YAML is derived from it for GOST.
func TestConfigRoundTripsThroughJSONAndYAML(t *testing.T) {
	rendered, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), goldenState())
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := json.Unmarshal(rendered.Data, &decoded); err != nil {
		t.Fatal(err)
	}
	data, err := EncodeYAML(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var reencoded Config
	if err := yaml.Unmarshal(data, &reencoded); err != nil {
		t.Fatal(err)
	}
	if len(reencoded.Services) != len(decoded.Services) || reencoded.Metrics != decoded.Metrics || reencoded.Log != decoded.Log {
		t.Fatalf("yaml round trip lost data: %+v", reencoded)
	}
	if reencoded.Log.Level != "warn" || reencoded.Log.Format != "text" {
		t.Fatalf("log section: %+v", reencoded.Log)
	}
	if reencoded.Metrics.Addr != "unix://"+filepath.Join(goldenDirectory, MetricsSocket) {
		t.Fatalf("metrics address: %s", reencoded.Metrics.Addr)
	}
	// Every service keeps its name, its transports and its single target.
	for index, service := range decoded.Services {
		other := reencoded.Services[index]
		if service.Name != other.Name || service.Addr != other.Addr || service.Handler != other.Handler || service.Listener != other.Listener || service.Limiter != other.Limiter || service.Climiter != other.Climiter || !sameNodes(service.Forwarder.Nodes, other.Forwarder.Nodes) || (service.Forwarder.Selector == nil) != (other.Forwarder.Selector == nil) || service.Forwarder.Selector != nil && *service.Forwarder.Selector != *other.Forwarder.Selector {
			t.Fatalf("service %d changed: %+v", index, other)
		}
	}
	if len(reencoded.Limiters) != 0 || len(reencoded.Climiters) != 0 {
		t.Fatalf("a legacy forward rendered limits: %+v %+v", reencoded.Limiters, reencoded.Climiters)
	}
}

func sameNodes(a, b []Node) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// poolState is one G2 forward: an ordered IPv6/IPv4 pool, a two-port range, a
// fifo selector, both byte rates and a TCP connection cap.
func poolState() desiredstate.State {
	strategy, failTimeout := "fifo", "30s"
	maxFails, portEnd, connections := 2, 20401, 64
	in, out := int64(1048576), int64(2097152)
	return desiredstate.State{
		SchemaVersion: 1,
		Revision:      9,
		NodeID:        "node-gost",
		Logging:       desiredstate.Logging{DomainOnly: true},
		Inbounds: []desiredstate.Inbound{{
			Core: "gost", ID: "pool-forward", Protocol: "forward", Listen: "127.0.0.1", Port: 20400, Enabled: true, DefaultOutbound: "direct",
			Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{
				Network:         "tcp,udp",
				Targets:         []desiredstate.ForwardTarget{{Host: "2001:db8::1", Port: 8443}, {Host: "target.example.net", Port: 8444}},
				Strategy:        &strategy,
				MaxFails:        &maxFails,
				FailTimeout:     &failTimeout,
				PortEnd:         &portEnd,
				BandwidthInBps:  &in,
				BandwidthOutBps: &out,
				MaxConnections:  &connections,
			}},
		}},
		Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}},
	}
}

// TestRenderPoolRangeAndLimits pins the G2 native shape: one service per
// (port, transport) with the whole ordered pool, its own selector and its own
// limiter references, and no range written into an address.
func TestRenderPoolRangeAndLimits(t *testing.T) {
	rendered, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), poolState())
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(rendered.Data, &config); err != nil {
		t.Fatal(err)
	}
	want := []string{"fwd-pool-forward-20400-tcp", "fwd-pool-forward-20400-udp", "fwd-pool-forward-20401-tcp", "fwd-pool-forward-20401-udp"}
	if len(config.Services) != len(want) {
		t.Fatalf("services: %+v", config.Services)
	}
	for index, name := range want {
		service := config.Services[index]
		if service.Name != name {
			t.Fatalf("service %d is %q, want %q", index, service.Name, name)
		}
		if service.Forwarder.Selector == nil || *service.Forwarder.Selector != (Selector{Strategy: "fifo", MaxFails: 2, FailTimeout: "30s"}) {
			t.Fatalf("service %s selector: %+v", name, service.Forwarder.Selector)
		}
		if len(service.Forwarder.Nodes) != 2 || service.Forwarder.Nodes[0] != (Node{Name: "target-0", Addr: "[2001:db8::1]:8443"}) || service.Forwarder.Nodes[1] != (Node{Name: "target-1", Addr: "target.example.net:8444"}) {
			t.Fatalf("service %s pool: %+v", name, service.Forwarder.Nodes)
		}
		wantClimiter := ""
		if strings.HasSuffix(name, "-tcp") {
			wantClimiter = "clim-" + name
		}
		if service.Limiter != "lim-"+name || service.Climiter != wantClimiter {
			t.Fatalf("service %s limiters: %q %q", name, service.Limiter, service.Climiter)
		}
		if strings.Contains(service.Addr, "-") || strings.Contains(service.Addr, ",") {
			t.Fatalf("a range reached addr: %s", service.Addr)
		}
	}
	if config.Services[0].Addr != "127.0.0.1:20400" || config.Services[2].Addr != "127.0.0.1:20401" {
		t.Fatalf("addrs: %+v", config.Services)
	}
	// Each service carries its own limits: a range of four services is four
	// independent limiters, and the connection cap covers the TCP services only.
	if len(config.Limiters) != 4 || len(config.Climiters) != 2 {
		t.Fatalf("limiters: %+v %+v", config.Limiters, config.Climiters)
	}
	// GOST parses a rate with a byte-unit parser that rejects a bare integer, so
	// the canonical encoding is the byte count with the `B` suffix.
	if limits := config.Limiters[0].Limits; len(limits) != 1 || limits[0] != "$ 1048576B 2097152B" {
		t.Fatalf("bandwidth limit: %v", limits)
	}
	if limits := config.Climiters[0].Limits; len(limits) != 1 || limits[0] != "$ 64" {
		t.Fatalf("connection limit: %v", limits)
	}
	if strings.Join(rendered.HealthEndpoints, ",") != "127.0.0.1:20400,127.0.0.1:20401" || strings.Join(rendered.UDPHealthEndpoints, ",") != "127.0.0.1:20400,127.0.0.1:20401" {
		t.Fatalf("health endpoints: %v %v", rendered.HealthEndpoints, rendered.UDPHealthEndpoints)
	}
}

// TestRenderRangeExpandsTheSamePool proves the n-th port is not mapped onto the
// n-th target: every expanded service keeps the whole ordered pool, and a forward
// without optional settings renders the defaults.
func TestRenderRangeExpandsTheSamePool(t *testing.T) {
	state := poolState()
	end := 20002
	state.Inbounds[0].Port = 20000
	state.Inbounds[0].Settings.Forward = &desiredstate.ForwardSettings{Network: "tcp", Host: "203.0.113.10", Port: 443, PortEnd: &end}
	rendered, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(rendered.Data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Services) != 3 {
		t.Fatalf("services: %+v", config.Services)
	}
	for index, port := range []string{"20000", "20001", "20002"} {
		service := config.Services[index]
		if service.Name != "fwd-pool-forward-"+port+"-tcp" || service.Addr != "127.0.0.1:"+port {
			t.Fatalf("service %d: %+v", index, service)
		}
		if len(service.Forwarder.Nodes) != 1 || service.Forwarder.Nodes[0].Addr != "203.0.113.10:443" {
			t.Fatalf("service %d pool: %+v", index, service.Forwarder.Nodes)
		}
		if service.Forwarder.Selector == nil || *service.Forwarder.Selector != (Selector{Strategy: "round", MaxFails: 1, FailTimeout: "5s"}) {
			t.Fatalf("service %d selector: %+v", index, service.Forwarder.Selector)
		}
		if service.Limiter != "" || service.Climiter != "" {
			t.Fatalf("service %d rendered limits without settings", index)
		}
	}
	if len(config.Limiters) != 0 || len(config.Climiters) != 0 {
		t.Fatalf("limits without settings: %+v %+v", config.Limiters, config.Climiters)
	}
}

// TestRenderRefusesTooManyServices keeps the per-node bound on the typed settings
// instead of expanding a configuration the process would have to refuse.
func TestRenderRefusesTooManyServices(t *testing.T) {
	state := poolState()
	state.Inbounds = nil
	for index, port := range []int{20000, 20400, 20800} {
		inbound := poolState().Inbounds[0]
		inbound.ID = "range-" + strconv.Itoa(index)
		inbound.Port = port
		end := port + 255
		forward := *inbound.Settings.Forward
		forward.PortEnd = &end
		inbound.Settings.Forward = &forward
		state.Inbounds = append(state.Inbounds, inbound)
	}
	if _, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), state); err == nil || !strings.Contains(err.Error(), "at most 1024") {
		t.Fatalf("service cap: %v", err)
	}
}

// TestRenderRejectsUnsupportedStates covers every state the GOST renderer must
// refuse instead of silently ignoring: another core's inbound, a non-forward
// protocol, accounts, policies of either scope, and a default outbound that is not
// the implicit direct one.
func TestRenderRejectsUnsupportedStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*desiredstate.State)
		want   string
	}{
		{"other core", func(s *desiredstate.State) { s.Inbounds[0].Core = "xray" }, "another core"},
		{"user protocol", func(s *desiredstate.State) {
			s.Inbounds[0].Protocol = "socks5"
			s.Inbounds[0].Settings = desiredstate.ProtocolSettings{}
		}, "gost core requires a forward inbound"},
		{"account", func(s *desiredstate.State) {
			s.Credentials = []desiredstate.Credential{{ID: "c", Protocol: "forward", Secret: "s"}}
		}, "no credentials"},
		{"policy", func(s *desiredstate.State) {
			s.Policies = []desiredstate.Policy{{ID: "p", Enabled: true, Action: desiredstate.Action{Type: "direct"}}}
		}, "no credentials, policy"},
		{"indirect outbound", func(s *desiredstate.State) {
			s.Outbounds = []desiredstate.Outbound{{ID: "direct", Protocol: "block", Enabled: true}}
		}, "direct default outbound"},
		{"missing outbound", func(s *desiredstate.State) { s.Outbounds = nil }, "default outbound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := goldenState()
			tc.mutate(&s)
			_, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), s)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("render accepted the state: %v", err)
			}
		})
	}
}

// TestRenderRequiresARuntimeDirectory keeps the metrics socket inside the node's
// private runtime directory instead of rendering a relative path.
func TestRenderRequiresARuntimeDirectory(t *testing.T) {
	if _, err := (&Adapter{}).Render(context.Background(), goldenState()); err == nil {
		t.Fatal("render accepted an empty runtime directory")
	}
}

// TestDisabledForwardRendersNothing proves an Owner-disabled forward does not reach
// the GOST configuration at all.
func TestDisabledForwardRendersNothing(t *testing.T) {
	state := goldenState()
	for index := range state.Inbounds {
		state.Inbounds[index].Enabled = false
	}
	rendered, err := (&Adapter{Directory: goldenDirectory}).Render(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	var config Config
	if err := json.Unmarshal(rendered.Data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Services) != 0 || len(rendered.HealthEndpoints) != 0 || len(rendered.UDPHealthEndpoints) != 0 {
		t.Fatalf("disabled forwards rendered services: %+v", config.Services)
	}
}
