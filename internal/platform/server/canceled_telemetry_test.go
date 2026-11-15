package server

import (
	"context"
	"encoding/json"
	"fmt"
	"proxycontrol/internal/panel/user"
	contract "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
	"testing"
	"time"
)

func TestCanceledRevisionAccessAndTrafficAttribution(t *testing.T) {
	app := testServer(t)
	ctx := context.Background()
	n, _, _, err := app.nodeRepository.CreateEnrollment(ctx, "撤除节点", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	revision := accessRevision(t, app, n.ID)
	users := &user.Repository{Database: app.Database, Vault: app.deployments.Vault}
	u, err := users.Save(ctx, "", user.Input{Username: "canceled_user", Status: "active", AllowedProtocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	device, err := users.CreateDevice(ctx, u.ID, user.DeviceInput{Name: "test", Protocols: []string{"socks5"}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.deployments.State(ctx, revision)
	if err != nil {
		t.Fatal(err)
	}
	state.Users[0].ID = u.ID
	state.Credentials[0].ID = device.Credentials[0].ID
	state.Credentials[0].UserID = u.ID
	state.Credentials[0].DeviceID = device.ID
	state.Inbounds[0].CredentialIDs = []string{device.Credentials[0].ID}
	created, err := app.deployments.Create(ctx, state, "owner", "canceled telemetry")
	if err != nil {
		t.Fatal(err)
	}
	revision = created.ID
	if err := app.deployments.Deploy(ctx, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Database.Exec("UPDATE deployment_targets SET status='failed',result_code='canvas_remove_canceled' WHERE revision=?", revision); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, node string
		revision   int64
		accepted   bool
	}{{"canceled", n.ID, revision, true}, {"newer", n.ID, revision + 100, false}, {"foreign", "other-node", revision, false}, {"malformed", n.ID, revision, false}} {
		t.Run(tc.name, func(t *testing.T) {
			batch := contract.Batch{ID: "access-" + tc.name, NodeID: tc.node, Records: []contract.Rollup{telemetryRow(tc.node, tc.revision, "example.com")}}
			batch.Records[0].UserID = u.ID
			batch.Records[0].DeviceID = device.ID
			if tc.name == "malformed" {
				batch.Records[0].Domain = "https://example.com/private"
			}
			raw, _ := json.Marshal(batch)
			_, err := app.trafficService.RecordAccess(ctx, tc.node, &pb.AccessBatch{BatchJson: raw}, true)
			if (err == nil) != tc.accepted {
				t.Fatalf("access: %v", err)
			}
			counters := traffic.Batch{ID: "traffic-" + tc.name, NodeID: tc.node, Counters: []traffic.Counter{{CoreEpoch: "canceled-epoch", Revision: tc.revision, CredentialID: "audit-credential", UserID: "audit-user", DeviceID: "audit-device", Protocol: "socks5", UploadBytes: 10, DownloadBytes: 20, ObservedAt: time.Now().UTC()}}}
			counters.Counters[0].UserID = u.ID
			counters.Counters[0].DeviceID = device.ID
			counters.Counters[0].CredentialID = device.Credentials[0].ID
			if tc.name == "malformed" {
				counters.Counters[0].UploadBytes = -1
			}
			raw, _ = json.Marshal(counters)
			_, err = app.trafficService.RecordTraffic(ctx, tc.node, &pb.TrafficBatch{BatchJson: raw}, true)
			if (err == nil) != tc.accepted {
				t.Fatalf("traffic: %v", err)
			}
			if tc.accepted {
				if _, err = app.trafficService.RecordTraffic(ctx, tc.node, &pb.TrafficBatch{BatchJson: raw}, true); err != nil {
					t.Fatal("replay", err)
				}
			}
		})
	}
	got, err := users.Get(ctx, u.ID)
	if err != nil || got.UsedTraffic != 30 {
		t.Fatal("usage must be charged exactly once", got.UsedTraffic, err)
	}
	for _, table := range []string{"access_batches", "traffic_batches"} {
		var count int
		if err := app.Database.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count); err != nil || count != 1 {
			t.Fatal(table, count, err)
		}
	}
}
