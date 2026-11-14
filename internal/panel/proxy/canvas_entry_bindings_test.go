package proxy

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCanvasEntryBindingLayout(t *testing.T) {
	for _, binding := range []string{
		`{"mode":"create","protocol":"socks5","portMode":"auto","groupMode":"exit_groups"}`,
		`{"mode":"reuse","inboundId":"inbound-1","portMode":"manual","port":1080,"groupMode":"explicit","groupIds":["g"]}`,
		`{"mode":"reuse","inboundId":"inbound-1","core":"xray","portMode":"auto","groupMode":"none"}`,
	} {
		layout, err := decodeCanvasLayout([]byte(`{"instances":[{"instanceId":"entry","ref":"server:node","role":"entry","x":0,"y":0,"entryBinding":` + binding + `}],"connections":[]}`))
		if err != nil {
			t.Fatal(err)
		}
		if err := layout.validateEntryBindings("server"); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(layout)
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := decodeCanvasLayout(data)
		if err != nil || !reflect.DeepEqual(roundTrip, layout) {
			t.Fatalf("layout round trip: %+v, %v", roundTrip, err)
		}
		if err := layout.validateEntryBindings("client"); err == nil {
			t.Fatal("client binding accepted")
		}
		layout.Instances[0].Role = "exit"
		if err := layout.validateEntryBindings("server"); err == nil {
			t.Fatal("exit binding accepted")
		}
	}
	for _, binding := range []string{
		`{}`, `{"mode":"bad"}`, `{"mode":"create","portMode":"auto","groupMode":"none"}`,
		`{"mode":"reuse","portMode":"auto","groupMode":"none"}`,
		`{"mode":"create","protocol":"socks5","portMode":"manual","groupMode":"none"}`,
		`{"mode":"create","protocol":"socks5","portMode":"manual","port":0,"groupMode":"none"}`,
		`{"mode":"create","protocol":"socks5","portMode":"manual","port":65536,"groupMode":"none"}`,
		`{"mode":"create","protocol":"socks5","portMode":"bad","groupMode":"none"}`,
		`{"mode":"create","protocol":"socks5","portMode":"auto","groupMode":"bad"}`,
		`{"mode":"create","protocol":"socks5","portMode":"auto","groupMode":"none","groupIds":["g"]}`,
		`{"mode":"create","protocol":"socks5","portMode":"auto","port":1080,"groupMode":"none"}`,
		`{"mode":"create","inboundId":"i","protocol":"socks5","portMode":"auto","groupMode":"none"}`,
	} {
		var b CanvasEntryBinding
		if err := json.Unmarshal([]byte(binding), &b); err != nil {
			t.Fatal(err)
		}
		layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "server:node", Role: "entry", EntryBinding: &b}}}
		if err := layout.validateEntryBindings("server"); err == nil {
			t.Fatalf("accepted %s", binding)
		}
	}
	if _, err := decodeCanvasLayout([]byte(`{"instances":[{"entryBinding":{"unknown":true}}]}`)); err == nil {
		t.Fatal("unknown binding field accepted")
	}
	old, err := decodeCanvasLayout([]byte(`{"instances":[{"instanceId":"entry","ref":"inbound:i","role":"entry"}],"connections":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := old.validateEntryBindings("client"); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(old)
	if err != nil || strings.Contains(string(data), "entryBinding") {
		t.Fatalf("old layout changed: %s, %v", data, err)
	}
}

func TestCanvasEntryBindingValidationPaths(t *testing.T) {
	b := &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "none"}
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "a", Ref: "inbound:a", Role: "entry", EntryBinding: b}, {InstanceID: "b", Ref: "inbound:b", Role: "exit"}}, Connections: []CanvasConnection{{ID: "edge", Source: "a", Target: "b", Type: "proxy_chain"}}}
	if err := ValidateClientLayout(layout); err == nil {
		t.Fatal("client publication accepted binding")
	}
	layout.Instances[0].Ref = "server:a"
	layout.Instances[0].Role = "relay"
	if err := layout.validate(); err == nil {
		t.Fatal("generic layout validation accepted relay binding")
	}
}

func TestCanvasEntryBindingRepository(t *testing.T) {
	p := newCanvasPanel(t)
	r := p.service.Repository
	scheme, err := r.SaveCanvasScheme(p.ctx, "", CanvasSchemeInput{Name: "binding", Execution: "server"})
	if err != nil {
		t.Fatal(err)
	}
	b := CanvasEntryBindingRecord{ID: "binding", SchemeID: &scheme.ID, InstanceID: "entry", InboundID: p.inbound.ID, Mode: "reuse", OriginalDefaultOutbound: "direct", State: "active"}
	if err := r.InsertCanvasEntryBinding(p.ctx, b); err != nil {
		t.Fatal(err)
	}
	got, err := r.CanvasEntryBinding(p.ctx, scheme.ID, "entry")
	if err != nil || got.OriginalDefaultOutbound != "direct" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := r.UpdateCanvasEntryBinding(p.ctx, b.ID, "published", "chain:next"); err != nil {
		t.Fatal(err)
	}
	got, err = r.CanvasEntryBindingByInbound(p.ctx, p.inbound.ID)
	if err != nil || got.State != "published" || got.AppliedDefaultOutbound != "chain:next" || got.OriginalDefaultOutbound != "direct" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := r.DeleteCanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
	got, err = r.CanvasEntryBindingByInbound(p.ctx, p.inbound.ID)
	if err != nil || got.SchemeID != nil || got.State != "detached" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := r.CanvasEntryBinding(p.ctx, scheme.ID, "entry"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := r.UpdateCanvasEntryBinding(p.ctx, "missing", "active", "direct"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSaveCanvasSchemeEntryBinding(t *testing.T) {
	p := newCanvasPanel(t)
	input := CanvasSchemeInput{Name: "entry binding", Execution: "server", Layout: CanvasLayout{Instances: []CanvasInstance{{InstanceID: "entry", Ref: "server:" + p.nodeA, Role: "entry", EntryBinding: &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "auto", GroupMode: "exit_groups"}}}}}
	scheme, err := p.service.SaveCanvasScheme(p.ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	if scheme.Layout.Instances[0].EntryBinding == nil || scheme.Layout.Instances[0].EntryBinding.Protocol != "socks5" {
		t.Fatalf("binding lost: %+v", scheme)
	}
	input.Execution = "client"
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", input); err == nil {
		t.Fatal("client binding saved")
	}
	input.Execution = "server"
	input.Layout.Instances[0].Role = "relay"
	if _, err := p.service.SaveCanvasScheme(p.ctx, "", input); err == nil {
		t.Fatal("relay binding saved")
	}
}
