package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNodeLatencyTargetsValidationPermissionsAndHistory(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	n, _, _, err := app.nodeRepository.CreateEnrollment(context.Background(), "targets", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 0; i < 6; i++ {
		id := fmt.Sprint(i)
		ids = append(ids, id)
		if _, err = app.Database.Exec("INSERT INTO probe_targets(id,label,host,port,scheduled) VALUES(?,?,'1.1.1.1',443,?)", id, id, i == 0); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/nodes/" + n.ID + "/latency-targets"
	check := func(custom bool, targets, defaults int) {
		t.Helper()
		got := owner.call("GET", path, nil)
		var body struct {
			Custom                      bool
			TargetIDs, DefaultTargetIDs []string
		}
		if json.Unmarshal(got.Body.Bytes(), &body) != nil || got.Code != 200 || body.Custom != custom || len(body.TargetIDs) != targets || len(body.DefaultTargetIDs) != defaults {
			t.Fatal(got.Code, got.Body.String())
		}
	}
	check(false, 0, 1)
	body := map[string]any{"custom": true, "targetIds": []string{"1"}}
	noCSRF := owner
	noCSRF.csrf = ""
	if got := noCSRF.call("PUT", path, body); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	for _, tc := range []struct {
		ids    []string
		status int
		code   string
	}{
		{ids, 409, "node_target_limit"},
		{[]string{"missing"}, 400, "unknown_target"},
		{[]string{"1", "1"}, 400, "invalid_request"},
	} {
		got := owner.call("PUT", path, map[string]any{"custom": true, "targetIds": tc.ids})
		if got.Code != tc.status || !strings.Contains(got.Body.String(), tc.code) {
			t.Fatal(got.Code, got.Body.String())
		}
		check(false, 0, 1)
	}
	if got := owner.call("PUT", path, body); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	check(true, 1, 1)
	if _, err = app.Database.Exec("INSERT INTO node_latency_samples(node_id,target_id,at,sent,failed) VALUES(?,'2',?,3,3),(?,'3',?,3,3)", n.ID, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), n.ID, time.Now().UTC().Add(-48*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	got := owner.call("GET", "/api/v1/nodes/"+n.ID+"/latency", nil)
	var latency struct{ Targets []struct{ ID string } }
	if json.Unmarshal(got.Body.Bytes(), &latency) != nil || len(latency.Targets) != 2 || latency.Targets[0].ID != "1" || latency.Targets[1].ID != "2" {
		t.Fatal(got.Body.String())
	}
	if got = owner.call("PUT", path, map[string]any{"custom": true, "targetIds": []string{}}); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	check(true, 0, 1)
	if got = owner.call("PUT", path, body); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	if got = owner.call("PUT", path, map[string]any{"custom": false, "targetIds": []string{"1"}}); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	check(false, 0, 1)
	var rows int
	if err = app.Database.QueryRow("SELECT count(*) FROM node_latency_targets").Scan(&rows); err != nil || rows != 0 {
		t.Fatal(rows, err)
	}
	if err = app.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改节点延迟目标'").Scan(&rows); err != nil || rows != 4 {
		t.Fatal("audit", rows, err)
	}
	if _, err = app.Database.Exec("UPDATE admins SET role='Viewer'"); err != nil {
		t.Fatal(err)
	}
	check(false, 0, 1)
	if got = owner.call("PUT", path, body); got.Code != 403 {
		t.Fatal("Viewer PUT", got.Code)
	}
	if _, err = app.Database.Exec("UPDATE admins SET role='Owner'"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), n.ID); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "PUT"} {
		if got = owner.call(method, path, body); got.Code != 404 {
			t.Fatal(method, got.Code, got.Body.String())
		}
	}
}
