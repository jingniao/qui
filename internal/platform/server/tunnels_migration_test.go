package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"testing/fstest"

	"proxycontrol/internal/testutil/canvasfixture"
)

func TestStartupReconcilesCanvasForwards(t *testing.T) {
	app := testServer(t)
	defer app.closeReadPool()
	seeded := canvasfixture.PublishedForwards(t, app.Database, app.deployments.Vault)
	if _, err := app.Database.Exec("UPDATE canvas_forward_reconcile SET done=0"); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(app.Config, app.Database, fstest.MapFS{"index.html": {Data: []byte("panel")}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.closeReadPool()
	canvasfixture.AssertConverted(t, app.Database, app.deployments.Vault, seeded, false)
	var done bool
	if err = app.Database.QueryRowContext(context.Background(), "SELECT done FROM canvas_forward_reconcile WHERE id=1").Scan(&done); err != nil || !done {
		t.Fatal(done, err)
	}
}
