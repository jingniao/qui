package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeContinuesAfterTunnelFinalizationFailure(t *testing.T) {
	app := testServer(t)
	if _, err := app.Database.Exec(`INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at,tunnel_publication_json,tunnel_finalized) VALUES('corrupt-publication','','corrupt','owner','draft','2000-01-01T00:00:00Z','{"phase":"preparing","nodes":[],"owners":[],"sealed":"Y29ycnVwdA=="}',0)`); err != nil {
		t.Fatal(err)
	}
	httpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { httpListener.Close() })
	grpcListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { grpcListener.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, httpListener, grpcListener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Serve did not stop")
		}
	})
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + httpListener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status: %d", response.StatusCode)
	}
	var status string
	var finalized bool
	if err := app.Database.QueryRow("SELECT status,tunnel_finalized FROM deployment_batches WHERE id='corrupt-publication'").Scan(&status, &finalized); err != nil || status != "failed" || finalized {
		t.Fatalf("failed finalization not retained: %s %v %v", status, finalized, err)
	}
}
