package server

import (
	"context"
	"encoding/json"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"testing"
	"time"
)

func TestNodeDeletionPendingNodeWithInboundUsesCascade(t *testing.T) {
	app := testServer(t)
	owner := budgetOwner(t, app)
	ctx := context.Background()
	repo := &node.Repository{Database: app.Database}
	n, _, _, err := repo.CreateEnrollment(ctx, "pending configured", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	inbound, err := app.tunnelPublications.SaveInbound(ctx, "", proxy.InboundInput{Name: "pending inbound", NodeID: n.ID, Protocol: "socks5", Listen: "0.0.0.0", Port: 23456, Enabled: true, DefaultOutbound: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	response := owner.call("GET", "/api/v1/nodes/"+n.ID+"/deletion-preview", nil)
	var preview node.DeletionPreview
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &preview) != nil || preview.Mode != "delete" || len(preview.Inbounds) != 1 {
		t.Fatalf("preview %d %s", response.Code, response.Body.String())
	}
	response = owner.call("DELETE", "/api/v1/nodes/"+n.ID, nil)
	if response.Code != 200 {
		t.Fatalf("delete %d %s", response.Code, response.Body.String())
	}
	var count int
	if err = app.Database.QueryRow("SELECT count(*) FROM inbounds WHERE id=?", inbound.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inbound survived %d %v", count, err)
	}
}
