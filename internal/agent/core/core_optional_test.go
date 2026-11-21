package core

import (
	"context"
	"errors"
	"testing"

	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
)

func TestOptionalRuntimeAndFirstUserCore(t *testing.T) {
	for _, gostFirst := range []bool{false, true} {
		events := []string{}
		m, err := NewMultiRuntime()
		if err != nil {
			t.Fatal(err)
		}
		if gostFirst {
			if err := m.AddIdle(&fakeMultiCore{name: "gost", events: &events}); err != nil {
				t.Fatal(err)
			}
		}
		if m.Name() != "" || m.Component("") != nil {
			t.Fatal("GOST became primary")
		}
		ctx := context.Background()
		s := desiredstate.State{NodeID: "node", SchemaVersion: 1, Logging: desiredstate.Logging{DomainOnly: true}, Revision: 1, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}}
		c, err := m.Render(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Validate(ctx, c); err != nil {
			t.Fatal(err)
		}
		if result, err := m.Apply(ctx, c); err != nil || !result.Success {
			t.Fatal(result, err)
		}
		s.Inbounds = []desiredstate.Inbound{{ID: "user", Protocol: "socks5", Listen: "127.0.0.1", Port: 1080, DefaultOutbound: "direct", Enabled: true}}
		if _, err := m.Render(ctx, s); !errors.Is(err, ErrNoUserCore) {
			t.Fatal(err)
		}
		user := &fakeMultiCore{name: "sing-box", events: &events}
		if err := m.AddIdle(user); err != nil {
			t.Fatal(err)
		}
		if m.Name() != "sing-box" || m.Component("") != user {
			t.Fatal("first user core did not become primary")
		}
	}
}

func TestGostOnlyTrafficAndHealth(t *testing.T) {
	ctx := context.Background()
	events := []string{}
	gost := &fakeMultiCore{name: "gost", events: &events}
	m, err := NewMultiRuntime(gost)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	m.ConfigureRuntime(RuntimeHooks{TrafficSink: func(_ context.Context, counters []traffic.Counter) error {
		count++
		if len(counters) != 1 || counters[0].CoreEpoch != "gost" {
			t.Fatal(counters)
		}
		return nil
	}})
	s := desiredstate.State{NodeID: "node", SchemaVersion: 1, Logging: desiredstate.Logging{DomainOnly: true}, Revision: 1, Outbounds: []desiredstate.Outbound{{ID: "direct", Protocol: "direct", Enabled: true}}, Inbounds: []desiredstate.Inbound{{ID: "forward", Core: "gost", Protocol: "forward", Listen: "127.0.0.1", Port: 1080, DefaultOutbound: "direct", Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}, Enabled: true}}}
	c, err := m.Render(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := m.Apply(ctx, c); err != nil || !result.Success {
		t.Fatal(result, err)
	}
	if err := m.SampleTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("GOST traffic loop was skipped", count)
	}
	gost.healthy = false
	status, err := m.Health(ctx)
	if status.Healthy || err == nil {
		t.Fatal("GOST health loop was skipped", status, err)
	}
}
