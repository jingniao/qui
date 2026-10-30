package server

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestK2bHTTPPermissionsValidationAndAudit(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	n, _, _, err := app.nodeRepository.CreateEnrollment(context.Background(), "tuning", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/nodes/" + n.ID
	body := map[string]any{"settings": map[string]string{"net.core.somaxconn": "128"}, "confirmNodeId": n.ID}
	check := func(method, path string, body any, code int, message string) {
		t.Helper()
		w := owner.call(method, path, body)
		if w.Code != code || !strings.Contains(w.Body.String(), message) {
			t.Fatal(method, path, w.Code, w.Body.String())
		}
	}
	check("GET", base+"/tuning", nil, 200, `"supported":false`)
	check("GET", base+"/metrics?hours=12", nil, 200, `"points":[]`)
	for _, hours := range []string{"0", "169", "abc", ""} {
		check("GET", base+"/metrics?hours="+hours, nil, 400, "invalid_hours")
	}
	noCSRF := owner
	noCSRF.csrf = ""
	if w := noCSRF.call("PUT", base+"/tuning", body); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	check("PUT", base+"/tuning", body, 409, "tuning_unsupported")
	if _, err = app.Database.Exec(`UPDATE nodes SET capabilities_json='{"tuningSupported":true}' WHERE id=?`, n.ID); err != nil {
		t.Fatal(err)
	}
	check("PUT", base+"/tuning", body, 409, "node_offline")
	if _, err = app.Database.Exec("UPDATE nodes SET status='online',last_heartbeat=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), n.ID); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []any{
		map[string]any{"settings": map[string]string{"net.core.somaxconn": "127"}, "confirmNodeId": n.ID},
		map[string]any{"settings": map[string]string{"arbitrary": "128"}, "confirmNodeId": n.ID},
		map[string]any{"settings": map[string]string{}, "confirmNodeId": "other"},
		map[string]any{"settings": map[string]string{}, "confirmNodeId": n.ID, "unknown": true},
		map[string]any{"settings": map[string]string{"net.core.somaxconn": "128"}, "clear": true, "confirmNodeId": n.ID},
	} {
		check("PUT", base+"/tuning", bad, 400, "")
	}
	check("PUT", base+"/tuning", body, 202, "pending")
	check("PUT", base+"/tuning", body, 409, "tuning_busy")
	var count int
	if err = app.Database.QueryRow("SELECT count(*) FROM admin_operations WHERE action='修改服务器调优'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = app.Database.Exec("UPDATE admins SET role='Viewer'"); err != nil {
		t.Fatal(err)
	}
	check("GET", base+"/tuning", nil, 200, `"desired":{"net.core.somaxconn":"128"}`)
	check("GET", base+"/metrics", nil, 200, "sampleSeconds")
	check("PUT", base+"/tuning", body, 403, "")
	if _, err = app.Database.Exec("UPDATE admins SET role='Owner'; UPDATE node_tuning SET status='unconfirmed'"); err != nil {
		t.Fatal(err)
	}
	check("PUT", base+"/tuning", map[string]any{"clear": true, "confirmNodeId": n.ID}, 202, "pending")
	check("GET", base+"/tuning", nil, 200, `"desired":{}`)
	if _, err = app.Database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), n.ID); err != nil {
		t.Fatal(err)
	}
	check("GET", base+"/tuning", nil, 404, "")
	check("PUT", base+"/tuning", body, 404, "")
	check("GET", base+"/metrics", nil, 404, "")
}
