package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"proxycontrol/internal/panel/node"
	"testing"
	"time"
)

func TestLatencyAPIShapeBoundsTargetCapAndViewer(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "latency", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	targets := []node.ProbeTarget{}
	for i := 0; i < 6; i++ {
		result := owner.call("POST", "/api/v1/probe-targets", map[string]any{"label": fmt.Sprint(i), "host": "1.1.1.1", "port": 443, "scheduled": i < 5})
		if result.Code != 201 {
			t.Fatal(result.Code, result.Body.String())
		}
		var target node.ProbeTarget
		if err = json.Unmarshal(result.Body.Bytes(), &target); err != nil {
			t.Fatal(err)
		}
		targets = append(targets, target)
	}
	body := map[string]any{"label": "updated", "host": "1.0.0.1", "port": 80, "scheduled": true}
	if got := owner.call("POST", "/api/v1/probe-targets", body); got.Code != 409 {
		t.Fatal("create cap", got.Code, got.Body.String())
	}
	if got := owner.call("PUT", "/api/v1/probe-targets/"+targets[5].ID, body); got.Code != 409 {
		t.Fatal("update cap", got.Code, got.Body.String())
	}
	if got := owner.call("PUT", "/api/v1/probe-targets/"+targets[0].ID, body); got.Code != 200 {
		t.Fatal("edit at cap", got.Code, got.Body.String())
	}
	body["scheduled"] = false
	if got := owner.call("PUT", "/api/v1/probe-targets/"+targets[0].ID, body); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	now := time.Now().UTC()
	for _, at := range []time.Time{now.Add(-time.Hour), now.Add(-2 * time.Hour), now.Add(-25 * time.Hour)} {
		if _, err = app.Database.Exec("INSERT INTO node_latency_samples(node_id,target_id,at,sent,failed) VALUES(?,?,?,3,3)", n.ID, targets[0].ID, at.Format("2006-01-02T15:04:05.000000000Z")); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/nodes/" + n.ID + "/latency"
	for _, hours := range []string{"0", "169", "abc", "", "1.5", "-1"} {
		if got := owner.call("GET", path+"?hours="+hours, nil); got.Code != 400 {
			t.Fatal(hours, got.Code)
		}
	}
	for _, suffix := range []string{"", "?hours=1", "?hours=168"} {
		if got := owner.call("GET", path+suffix, nil); got.Code != 200 {
			t.Fatal(suffix, got.Code)
		}
	}
	got := owner.call("GET", path, nil)
	var data struct {
		IntervalSeconds int                `json:"intervalSeconds"`
		Targets         []node.ProbeTarget `json:"targets"`
		Series          []struct {
			TargetID string           `json:"targetId"`
			Points   []map[string]any `json:"points"`
		} `json:"series"`
	}
	if err = json.Unmarshal(got.Body.Bytes(), &data); err != nil || data.IntervalSeconds != 300 || len(data.Targets) != 5 || len(data.Series) != 5 {
		t.Fatal(data, err, got.Body.String())
	}
	for _, s := range data.Series {
		if s.TargetID == targets[0].ID {
			if len(s.Points) != 2 {
				t.Fatal(s.Points)
			}
			if s.Points[0]["at"].(string) >= s.Points[1]["at"].(string) {
				t.Fatal("points not ascending")
			}
			for _, p := range s.Points {
				for _, key := range []string{"at", "avgMs", "minMs", "maxMs", "medianMs", "sent", "failed"} {
					if _, ok := p[key]; !ok {
						t.Fatal("missing", key)
					}
				}
			}
		}
	}
	if _, err = app.Database.Exec("UPDATE admins SET role='Viewer'"); err != nil {
		t.Fatal(err)
	}
	for _, read := range []string{path, "/api/v1/probe-targets", "/api/v1/nodes/" + n.ID + "/tcp-probes?targetId=" + targets[0].ID} {
		if got := owner.call("GET", read, nil); got.Code != 200 {
			t.Fatal("Viewer read", got.Code, got.Body.String())
		}
	}
	for _, write := range []struct{ method, path string }{{"POST", "/api/v1/probe-targets"}, {"PUT", "/api/v1/probe-targets/" + targets[0].ID}, {"DELETE", "/api/v1/probe-targets/" + targets[0].ID}} {
		if got := owner.call(write.method, write.path, body); got.Code != 403 {
			t.Fatal("Viewer write", got.Code)
		}
	}
	if _, err = app.Database.Exec("UPDATE nodes SET deleted_at=? WHERE id=?", now.Format(time.RFC3339Nano), n.ID); err != nil {
		t.Fatal(err)
	}
	if got := owner.call("GET", path, nil); got.Code != 404 {
		t.Fatal("deleted node telemetry served", got.Code, got.Body.String())
	}
}

func TestLatencySchedulerStopsWithServingInstance(t *testing.T) {
	app := testServer(t)
	app.latencyScheduler.Interval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "latency", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.Database.Exec("INSERT INTO probe_targets(id,label,host,port,scheduled) VALUES('scheduled','test','1.1.1.1',443,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = app.Database.Exec("UPDATE nodes SET tcp_probe_supported=1,last_heartbeat=? WHERE id=?", time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z"), n.ID); err != nil {
		t.Fatal(err)
	}
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		httpListener.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, httpListener, grpcListener) }()
	count := 0
	deadline := time.Now().Add(10 * time.Second)
	for count == 0 && time.Now().Before(deadline) {
		if err = app.Database.QueryRow("SELECT count(*) FROM node_tcp_probes WHERE source='scheduled'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if count == 0 {
		t.Fatal("server did not start scheduler")
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop scheduler")
	}
	if _, err = app.Database.Exec("DELETE FROM node_tcp_probes"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if err = app.Database.QueryRow("SELECT count(*) FROM node_tcp_probes").Scan(&count); err != nil || count != 0 {
		t.Fatal("scheduler ran after shutdown", count, err)
	}
}
