package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCanvasRemoveRoutesOwnerCSRFAndStrictJSON(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	owner, csrf := p.session("Owner")
	viewer, viewerCSRF := p.session("Viewer")
	for _, action := range []string{"remove-plan", "remove"} {
		path := "/api/v1/canvas-schemes/missing/" + action
		for _, tc := range []struct {
			name           string
			viewer, noCSRF bool
			want           int
		}{
			{"viewer", true, false, 403}, {"csrf", false, true, 403},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				cookie, token := owner, csrf
				if tc.viewer {
					cookie, token = viewer, viewerCSRF
				}
				if tc.noCSRF {
					token = ""
				}
				response := p.call("POST", path, map[string]any{"updatedAt": "version"}, cookie, token)
				if response.Code != tc.want {
					t.Fatalf("%d %s", response.Code, response.Body.String())
				}
			})
		}
		response := p.call("POST", path, map[string]any{"updatedAt": "version", "unexpected": true}, owner, csrf)
		if response.Code != 400 {
			t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
		}
	}
}

func TestCanvasRemovePlanHTTPReadOnlyAndStale(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "reuse", InboundID: p.inbound.ID, PortMode: "auto", GroupMode: "none"}))
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	owner, csrf := p.session("Owner")
	p.database.SetMaxOpenConns(1)
	before := quickCanvasChanges(t, p)
	response := p.call("POST", "/api/v1/canvas-schemes/"+scheme.ID+"/remove-plan", map[string]any{"updatedAt": scheme.UpdatedAt}, owner, csrf)
	var body map[string]any
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if after := quickCanvasChanges(t, p); after != before {
		t.Fatalf("preview wrote database: %d -> %d", before, after)
	}
	for _, action := range []string{"remove-plan", "remove"} {
		response = p.call("POST", "/api/v1/canvas-schemes/"+scheme.ID+"/"+action, map[string]any{"updatedAt": "stale"}, owner, csrf)
		if response.Code != 409 {
			t.Fatalf("%s: %d %s", action, response.Code, response.Body.String())
		}
		for _, body := range []map[string]any{{}, {"updatedAt": ""}} {
			missing := p.call("POST", "/api/v1/canvas-schemes/"+scheme.ID+"/"+action, body, owner, csrf)
			if missing.Code != 400 || !strings.Contains(missing.Body.String(), "invalid_request") {
				t.Errorf("missing updatedAt %s: %d %s", action, missing.Code, missing.Body.String())
			}
		}
	}
}

func TestCanvasDeleteOnlyKeepsManagedEntry(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}))
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	owner, csrf := p.session("Owner")
	response := p.call("DELETE", "/api/v1/canvas-schemes/"+scheme.ID, nil, owner, csrf)
	if response.Code != 204 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	after, err := p.service.Repository.CanvasEntryBindingByInbound(p.ctx, binding.InboundID)
	if err != nil || after.SchemeID != nil || after.State != "detached" {
		t.Fatalf("binding %+v: %v", after, err)
	}
	inbound, err := p.service.Repository.Inbound(p.ctx, binding.InboundID, true)
	if err != nil || inbound.DefaultOutbound != binding.AppliedDefaultOutbound {
		t.Fatalf("inbound %+v: %v", inbound, err)
	}
	var count int
	if err := p.database.QueryRow("SELECT COUNT(*) FROM canvas_tunnel_objects").Scan(&count); err != nil || count == 0 {
		t.Fatalf("channel objects %d: %v", count, err)
	}
}

func TestCanvasRemoveHTTPChangesConflictDetails(t *testing.T) {
	p, _ := quickCanvasFixture(t)
	scheme := savePlanScheme(t, p, entryPlanLayout(p, &CanvasEntryBinding{Mode: "create", Protocol: "socks5", PortMode: "manual", Port: canvasTestPort(20000), GroupMode: "none"}))
	plan, err := p.service.canvasBuildPlan(p.ctx, scheme)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.service.canvasApplyPlan(p.ctx, plan); err != nil {
		t.Fatal(err)
	}
	binding, err := p.service.Repository.CanvasEntryBinding(p.ctx, scheme.ID, "i-a")
	if err != nil {
		t.Fatal(err)
	}
	entryPlanGroup(t, p, "manual-group", "mixed", binding.InboundID)
	if _, err := p.database.Exec("UPDATE inbounds SET default_outbound='block' WHERE id=?", binding.InboundID); err != nil {
		t.Fatal(err)
	}
	owner, csrf := p.session("Owner")
	response := p.call("POST", "/api/v1/canvas-schemes/"+scheme.ID+"/remove", map[string]any{"updatedAt": scheme.UpdatedAt}, owner, csrf)
	if response.Code != 409 {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Details struct {
				Entries []struct {
					Changes []string `json:"changes"`
				} `json:"entries"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Error.Details.Entries) != 1 || len(body.Error.Details.Entries[0].Changes) < 2 {
		t.Fatalf("missing change details: %s", response.Body.String())
	}
	if _, err := p.service.Repository.CanvasScheme(p.ctx, scheme.ID); err != nil {
		t.Fatal(err)
	}
}
