package system

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRealityPresetsRoundTrip(t *testing.T) {
	f := newInstallHTTPFixture(t)
	ctx := context.Background()
	r := f.repository
	empty, err := r.ReadRealityPresets(ctx)
	if err != nil || len(empty.Items) != 0 || empty.Items == nil || empty.Revision != 0 {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	saved, err := r.SaveRealityPresets(ctx, RealityPresetsInput{Items: []RealityPreset{{Name: " 默认 ", HandshakeTarget: "www.amd.com:443"}}}, "owner", "ip")
	if err != nil || saved.Revision != 1 || saved.Items[0].ID == "" || saved.Items[0].Name != "默认" || !reflect.DeepEqual(saved.Items[0].ServerNames, []string{"www.amd.com"}) {
		t.Fatalf("save: %+v %v", saved, err)
	}
	input := RealityPresetsInput{Items: saved.Items, ExpectedRevision: 1}
	again, err := r.SaveRealityPresets(ctx, input, "owner", "ip")
	if err != nil || !reflect.DeepEqual(again, saved) {
		t.Fatalf("unchanged: %+v %v", again, err)
	}
	input.DefaultID = saved.Items[0].ID
	input.ExpectedRevision = 0
	if _, err := r.SaveRealityPresets(ctx, input, "owner", "ip"); !errors.Is(err, ErrRealityPresetsConflict) {
		t.Fatal(err)
	}
	input.ExpectedRevision = 1
	saved, err = r.SaveRealityPresets(ctx, input, "owner", "ip")
	if err != nil || saved.Revision != 2 || saved.DefaultID != input.DefaultID {
		t.Fatalf("default: %+v %v", saved, err)
	}
	current, err := r.ReadRealityPresets(ctx)
	if err != nil || !reflect.DeepEqual(current, saved) {
		t.Fatalf("read: %+v %v", current, err)
	}
	var count int
	if err := r.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改 Reality 预设' AND target='reality_presets'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("audit: %d %v", count, err)
	}
	saved, err = r.SaveRealityPresets(ctx, RealityPresetsInput{ExpectedRevision: 2}, "owner", "ip")
	if err != nil || saved.Revision != 3 || len(saved.Items) != 0 {
		t.Fatalf("clear: %+v %v", saved, err)
	}
}

func TestRealityPresetsHTTPValidationAndPermissions(t *testing.T) {
	f := newInstallHTTPFixture(t)
	const path = "/api/v1/system/reality-presets"
	const valid = `{"items":[{"name":"AMD","handshakeTarget":"www.amd.com:443"}],"expectedRevision":0}`
	for _, request := range []struct {
		method, body, cookie, csrf string
		code                       int
	}{
		{"GET", "", "", "", 401}, {"GET", "", f.viewer, "", 403}, {"PUT", valid, f.viewer, f.viewerCSRF, 403}, {"PUT", valid, f.owner, "", 403},
	} {
		if res := f.at(t, request.method, path, request.body, request.cookie, request.csrf); res.Code != request.code {
			t.Fatalf("permission: %d %s", res.Code, res.Body.String())
		}
	}
	base := RealityPreset{Name: "AMD", HandshakeTarget: "www.amd.com:443", ServerNames: []string{"www.amd.com"}}
	invalid := []RealityPreset{}
	for _, change := range []func(*RealityPreset){
		func(p *RealityPreset) { p.Name = " " }, func(p *RealityPreset) { p.Name = strings.Repeat("中", 65) },
		func(p *RealityPreset) { p.HandshakeTarget = "bad" }, func(p *RealityPreset) { p.HandshakeTarget = "host:0" }, func(p *RealityPreset) { p.HandshakeTarget = "host:65536" },
		func(p *RealityPreset) { p.ServerNames = []string{"127.0.0.1"} }, func(p *RealityPreset) { p.ServerNames = []string{"*.example.com"} }, func(p *RealityPreset) { p.ServerNames = []string{"host:443"} }, func(p *RealityPreset) { p.ServerNames = []string{""} }, func(p *RealityPreset) { p.ServerNames = make([]string, 17) },
		func(p *RealityPreset) { p.Transport = "ws" }, func(p *RealityPreset) { p.Flow = "invalid" }, func(p *RealityPreset) { p.Flow = "xtls-rprx-vision"; p.Transport = "xhttp" }, func(p *RealityPreset) { p.Flow = "xtls-rprx-vision" }, func(p *RealityPreset) { p.ID = "invented" },
	} {
		p := base
		change(&p)
		invalid = append(invalid, p)
	}
	inputs := []RealityPresetsInput{{Items: []RealityPreset{base, base}}, {Items: []RealityPreset{base}, DefaultID: "missing"}, {Items: []RealityPreset{base}, ExpectedRevision: -1}, {Items: make([]RealityPreset, 21)}}
	for _, p := range invalid {
		inputs = append(inputs, RealityPresetsInput{Items: []RealityPreset{p}})
	}
	for _, input := range inputs {
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		res := f.at(t, "PUT", path, string(body), f.owner, f.ownerCSRF)
		if res.Code != 400 || !strings.Contains(res.Body.String(), "invalid_settings") {
			t.Fatalf("invalid %s: %d %s", body, res.Code, res.Body.String())
		}
	}
	for _, body := range []string{`{"extra":1}`, `{"items":[{"name":"AMD","handshakeTarget":"www.amd.com:443","extra":1}]}`} {
		if res := f.at(t, "PUT", path, body, f.owner, f.ownerCSRF); res.Code != 400 {
			t.Fatalf("unknown field: %d", res.Code)
		}
	}
	res := f.at(t, "PUT", path, valid, f.owner, f.ownerCSRF)
	if res.Code != 200 {
		t.Fatalf("save: %d %s", res.Code, res.Body.String())
	}
	if res = f.at(t, "PUT", path, `{"items":[],"expectedRevision":0}`, f.owner, f.ownerCSRF); res.Code != 409 || !strings.Contains(res.Body.String(), "settings_conflict") {
		t.Fatalf("conflict: %d %s", res.Code, res.Body.String())
	}
}
