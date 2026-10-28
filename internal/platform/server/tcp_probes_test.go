package server

import (
	"context"
	"encoding/json"
	"fmt"
	"proxycontrol/internal/panel/node"
	"testing"
	"time"
)

func TestTCPProbeOwnerLimitsRetentionAndOldAgent(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "probe", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "10.1.1.1", "100.64.1.1", "169.254.1.1", "224.0.0.1", "::", "::1"} {
		if r := owner.call("POST", "/api/v1/probe-targets", map[string]any{"label": "refused", "host": host, "port": 443}); r.Code != 400 {
			t.Fatal(host, r.Code, r.Body.String())
		}
	}
	r := owner.call("POST", "/api/v1/probe-targets", map[string]any{"label": "loopback explicit", "host": "127.0.0.1", "port": 443, "allowNonPublic": true})
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var target node.ProbeTarget
	json.Unmarshal(r.Body.Bytes(), &target)
	path := "/api/v1/nodes/" + n.ID + "/tcp-probes"
	body := map[string]any{"targetId": target.ID, "attempts": 3, "timeoutMs": 100}
	if r = owner.call("POST", path, body); r.Code != 409 {
		t.Fatal("old Agent accepted", r.Code)
	}
	if _, err = app.Database.Exec("UPDATE nodes SET tcp_probe_supported=1,last_heartbeat=? WHERE id=?", time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"), n.ID); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]any{{"targetId": target.ID, "attempts": 11, "timeoutMs": 100}, {"targetId": target.ID, "attempts": 1, "timeoutMs": 3001}} {
		if r = owner.call("POST", path, bad); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	for i := 0; i < 105; i++ {
		r = owner.call("POST", path, body)
		if r.Code != 202 {
			t.Fatal(i, r.Code, r.Body.String())
		}
		if r2 := owner.call("POST", path, body); r2.Code != 409 {
			t.Fatal("overlapping probe", r2.Code)
		}
		if _, err = app.Database.Exec("UPDATE node_tcp_probes SET status='complete',result_json=json_object('id',id,'attempted',3,'lost',3,'code','complete') WHERE node_id=? AND status='queued'", n.ID); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = app.Database.QueryRow("SELECT count(*) FROM node_tcp_probes WHERE node_id=? AND target_id=?", n.ID, target.ID).Scan(&count); err != nil || count != 100 {
		t.Fatal(count, err)
	}
	if r = owner.call("GET", path+"?targetId="+target.ID, nil); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if _, err = app.Database.Exec("UPDATE admins SET role='Viewer'"); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path string }{{"POST", "/api/v1/probe-targets"}, {"DELETE", "/api/v1/probe-targets/" + target.ID}, {"POST", path}, {"POST", "/api/v1/inbounds/missing/reachability"}} {
		if got := owner.call(route.method, route.path, body); got.Code != 403 {
			t.Fatal(fmt.Sprint(route), got.Code)
		}
	}
}
