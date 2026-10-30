package core

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/devicegate"
	"proxycontrol/pkg/contracts/scheduler"
	"proxycontrol/pkg/contracts/traffic"
)

type fakeMultiCore struct {
	name                    string
	events                  *[]string
	failValidate, failApply bool
	healthy                 bool
	revision                int64
	gate                    devicegate.Request
	hooks                   RuntimeHooks
}

func (f *fakeMultiCore) Name() string { return f.name }
func (f *fakeMultiCore) DetectCapabilities(context.Context) (*pb.Capabilities, error) {
	return &pb.Capabilities{CoreName: f.name, CoreVersion: "1.2.3"}, nil
}
func (f *fakeMultiCore) Render(_ context.Context, s desiredstate.State) (RenderedConfig, error) {
	return RenderedConfig{NodeID: s.NodeID, Revision: s.Revision, Data: []byte(`{"inbounds":[]}`), StatsAccounts: map[string]traffic.Counter{"user": {CredentialID: f.name}}}, nil
}
func (f *fakeMultiCore) Validate(context.Context, RenderedConfig) error {
	*f.events = append(*f.events, "validate:"+f.name)
	if f.failValidate {
		return errors.New("invalid")
	}
	return nil
}
func (f *fakeMultiCore) Apply(_ context.Context, c RenderedConfig) (ApplyResult, error) {
	*f.events = append(*f.events, "apply:"+f.name)
	if f.failApply && c.Revision == 2 {
		return ApplyResult{}, errors.New("start failed")
	}
	f.revision = c.Revision
	f.healthy = true
	return ApplyResult{Success: true}, nil
}
func (f *fakeMultiCore) Rollback(context.Context, int64) error { return nil }
func (f *fakeMultiCore) Health(context.Context) (HealthStatus, error) {
	return HealthStatus{Healthy: f.healthy}, nil
}
func (f *fakeMultiCore) CollectStats(context.Context) (StatsBatch, error) {
	return StatsBatch{Upload: 3}, nil
}
func (f *fakeMultiCore) ConfigureRuntime(h RuntimeHooks) { f.hooks = h }
func (f *fakeMultiCore) Close() error {
	*f.events = append(*f.events, "close:"+f.name)
	f.healthy = false
	return nil
}
func (f *fakeMultiCore) SampleTraffic(ctx context.Context) error {
	return f.hooks.TrafficSink(ctx, []traffic.Counter{{CoreEpoch: f.name}})
}
func (f *fakeMultiCore) SupportsAccountTelemetry() bool              { return true }
func (f *fakeMultiCore) OutboundHealth() map[string]scheduler.Health { return nil }
func (f *fakeMultiCore) SetDeviceGate(_ context.Context, data []byte) error {
	var err error
	f.gate, err = devicegate.Decode(data)
	return err
}

func multiFixture(t *testing.T) (*MultiRuntime, *fakeMultiCore, *fakeMultiCore, *[]string) {
	t.Helper()
	events := []string{}
	a := &fakeMultiCore{name: "xray", events: &events}
	b := &fakeMultiCore{name: "sing-box", events: &events}
	m, err := NewMultiRuntime(a, b)
	if err != nil {
		t.Fatal(err)
	}
	return m, a, b, &events
}
func multiConfig(revision int64) RenderedConfig {
	r := RenderedConfig{NodeID: "n", Revision: revision, Cores: map[string]RenderedConfig{}}
	for _, n := range []string{"xray", "sing-box"} {
		r.Cores[n] = RenderedConfig{Revision: revision, StatsAccounts: map[string]traffic.Counter{n: {CredentialID: n}}}
	}
	return r
}
func TestMultiValidateBeforeAnySwitch(t *testing.T) {
	m, _, b, events := multiFixture(t)
	b.failValidate = true
	if _, err := m.Apply(context.Background(), multiConfig(1)); err == nil {
		t.Fatal("accepted invalid core")
	}
	if !reflect.DeepEqual(*events, []string{"validate:xray", "validate:sing-box"}) {
		t.Fatal(*events)
	}
}
func TestMultiCompensatingRollback(t *testing.T) {
	m, a, b, events := multiFixture(t)
	ctx := context.Background()
	if _, err := m.Apply(ctx, multiConfig(1)); err != nil {
		t.Fatal(err)
	}
	*events = nil
	b.failApply = true
	r, err := m.Apply(ctx, multiConfig(2))
	if err == nil || !r.RolledBack || a.revision != 1 || b.revision != 1 {
		t.Fatalf("%+v %v %d %d", r, err, a.revision, b.revision)
	}
	want := []string{"validate:xray", "validate:sing-box", "apply:xray", "apply:sing-box", "apply:sing-box", "apply:xray"}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("%v", *events)
	}
}
func TestMultiHealthIdleStatsAndGates(t *testing.T) {
	m, a, b, _ := multiFixture(t)
	ctx := context.Background()
	_, _ = m.Apply(ctx, multiConfig(1))
	b.healthy = false
	h, _ := m.Health(ctx)
	if h.Healthy {
		t.Fatal("health was not AND")
	}
	statuses := m.CoreStatuses(ctx)
	if !statuses[0].Healthy || statuses[1].Healthy {
		t.Fatal(statuses)
	}
	got := []string{}
	m.ConfigureRuntime(RuntimeHooks{TrafficSink: func(_ context.Context, c []traffic.Counter) error {
		for _, v := range c {
			got = append(got, v.CoreEpoch)
		}
		return nil
	}})
	if err := m.SampleTraffic(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"xray", "sing-box"}) {
		t.Fatal(got)
	}
	data, _ := json.Marshal(devicegate.Request{TTLSeconds: 180, Entries: []devicegate.Entry{{CredentialID: "xray", Keys: []string{"8.8.8.8"}}, {CredentialID: "sing-box", Keys: []string{"1.1.1.1"}}}})
	if err := m.SetDeviceGate(ctx, data); err != nil {
		t.Fatal(err)
	}
	if len(a.gate.Entries) != 1 || a.gate.Entries[0].CredentialID != "xray" || len(b.gate.Entries) != 1 || b.gate.Entries[0].CredentialID != "sing-box" {
		t.Fatal(a.gate, b.gate)
	}
	c := multiConfig(2)
	delete(c.Cores, "sing-box")
	if _, err := m.Apply(ctx, c); err != nil {
		t.Fatal(err)
	}
	h, _ = m.Health(ctx)
	if !h.Healthy || b.healthy {
		t.Fatal("idle core started or marked unhealthy")
	}
}
func TestRevisionCoreMapAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		data  string
		count int
	}{{`{"xray":{"inbounds":[]},"sing-box":{"inbounds":[]}}`, 2}, {`{"inbounds":[],"routing":{}}`, 1}, {`{}`, 0}} {
		configs, err := DecodeCoreConfigs([]byte(tc.data), "xray")
		if err != nil || len(configs) != tc.count {
			t.Fatalf("%s %v %v", tc.data, configs, err)
		}
	}
	if _, err := DecodeCoreConfigs([]byte(`null`), "xray"); err == nil {
		t.Fatal("accepted null")
	}
}
func TestNodePortAllocator(t *testing.T) {
	p := &PortAllocator{}
	if err := p.ReserveInbounds([]int{10001, 10002}); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{10001: true, 10002: true}
	for i := 0; i < 100; i++ {
		port, err := p.Allocate()
		if err != nil || seen[port] {
			t.Fatal(port, err)
		}
		seen[port] = true
	}
	var private int
	for port := range seen {
		if port != 10001 && port != 10002 {
			private = port
			break
		}
	}
	if p.ReserveInbounds([]int{private}) == nil {
		t.Fatal("runtime port reused as inbound")
	}
}
func TestMergeCoreSnapshots(t *testing.T) {
	p := traffic.ConnectionSnapshot{NodeID: "n", Revision: 1, CoreEpoch: "a", Scope: traffic.ConnectionScope, ObservedAt: time.Now(), ObservationContractVersion: 2, DeviceGateSupported: true, Connections: 1, Accounts: []traffic.ConnectionAccount{{CredentialID: "c", Connections: 1, AddressesMeasured: true, AddressKeys: []string{"8.8.8.8"}}}}
	q := p
	q.CoreEpoch = "b"
	r, err := MergeConnections([]traffic.ConnectionSnapshot{p, q})
	if err != nil || r.Connections != 2 || len(r.Accounts) != 1 || len(r.Accounts[0].AddressKeys) != 1 {
		t.Fatal(r, err)
	}
	q.Revision = 2
	if _, err := MergeConnections([]traffic.ConnectionSnapshot{p, q}); err == nil {
		t.Fatal("merged different revisions")
	}
}

type fakeNodeTotalCore struct{ *fakeMultiCore }

func (f *fakeNodeTotalCore) NodeTrafficTotals(context.Context) (uint64, uint64, error) {
	return 100, 200, nil
}
func TestSingleCoreWrapperPreservesLegacyNodeTotals(t *testing.T) {
	events := []string{}
	m, err := NewMultiRuntime(&fakeNodeTotalCore{&fakeMultiCore{name: "sing-box", events: &events}})
	if err != nil {
		t.Fatal(err)
	}
	u, d, err := m.NodeTrafficTotals(context.Background())
	if err != nil || u != 100 || d != 200 {
		t.Fatal(u, d, err)
	}
	if err = m.AddIdle(&fakeMultiCore{name: "xray", events: &events}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = m.NodeTrafficTotals(context.Background()); err == nil {
		t.Fatal("mixed node fabricated total from one peer")
	}
}
