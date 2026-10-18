package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestReadsAndSubscriptionsAnswerWhileTheWriterIsBusy holds the panel's single
// writer connection inside a write transaction and checks that an authenticated
// GET and a subscription fetch still answer within a second. The read-only pool
// serves the session lookup, the list and the whole subscription read path; the
// last-fetched marker is bounded and best effort.
func TestReadsAndSubscriptionsAnswerWhileTheWriterIsBusy(t *testing.T) {
	app := testServer(t)
	if app.readPool == nil {
		t.Fatal("read-only pool was not opened")
	}
	owner := flowOwner(t, app)

	response := owner.call("POST", "/api/v1/users", map[string]any{"username": "read_pool_user", "displayName": "Read pool", "status": "active", "maxDevices": 2, "maxConnections": 8, "allowedProtocols": []string{"socks5"}, "nodeGroupIds": []string{}})
	if response.Code != 201 {
		t.Fatalf("create user: %d %s", response.Code, response.Body.String())
	}
	var account struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	response = owner.call("POST", "/api/v1/users/"+account.ID+"/devices", map[string]any{"name": "Laptop", "protocols": []string{"socks5"}})
	if response.Code != 201 {
		t.Fatalf("create device: %d %s", response.Code, response.Body.String())
	}
	var device struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	response = owner.call("POST", "/api/v1/users/"+account.ID+"/subscription-token", map[string]any{"deviceId": device.ID})
	if response.Code != 201 {
		t.Fatalf("rotate token: %d %s", response.Code, response.Body.String())
	}
	var token struct {
		Token string `json:"token"`
		Path  string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	// Warm the in-memory setup state so the probes measure steady state.
	if warm := owner.call("GET", "/api/v1/auth/session", nil); warm.Code != 200 {
		t.Fatalf("warm-up session: %d %s", warm.Code, warm.Body.String())
	}

	holding := make(chan struct{})
	release := make(chan struct{})
	held := make(chan error, 1)
	go func() {
		tx, err := app.Database.BeginTx(context.Background(), nil)
		if err != nil {
			held <- err
			close(holding)
			return
		}
		if _, err := tx.ExecContext(context.Background(), "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES('2026-10-17T00:00:00Z','read-pool-test','占用写连接','writer','成功','127.0.0.1')"); err != nil {
			tx.Rollback()
			held <- err
			close(holding)
			return
		}
		close(holding)
		<-release
		held <- tx.Rollback()
	}()
	<-holding
	defer func() { close(release); <-held }()

	for _, probe := range []struct {
		name, method, path string
		status             int
	}{
		{"authenticated list", "GET", "/api/v1/subscriptions", 200},
		{"authenticated session", "GET", "/api/v1/auth/session", 200},
		{"subscription content", "GET", token.Path, 200},
	} {
		start := time.Now()
		response := owner.call(probe.method, probe.path, nil)
		elapsed := time.Since(start)
		if response.Code != probe.status {
			t.Fatalf("%s: %d %s", probe.name, response.Code, response.Body.String())
		}
		if elapsed > time.Second {
			t.Fatalf("%s answered only after %s while a write transaction was held", probe.name, elapsed)
		}
		t.Logf("%s answered %d in %s", probe.name, response.Code, elapsed)
	}
}

// TestReadPoolIsClosedWithTheServer keeps the pool's lifetime tied to serving.
func TestReadPoolIsClosedWithTheServer(t *testing.T) {
	app := testServer(t)
	pool := app.readPool
	if pool == nil {
		t.Fatal("read-only pool was not opened")
	}
	app.closeReadPool()
	if err := pool.PingContext(context.Background()); err == nil {
		t.Fatal("read-only pool still answers after close")
	}
	app.closeReadPool()
}
