package server

import (
	"encoding/json"
	"testing"
	"time"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

func TestSingboxOnlyTrafficCrossesQuotaAndRemovesCredential(t *testing.T) {
	f := newQuotaFixture(t)
	account, credential := f.addUser(t, "singbox_quota", "")
	if _, err := f.app.Database.Exec("UPDATE nodes SET capabilities_json=? WHERE id=?", `{"core_name":"sing-box","core_version":"1.14.4","singbox_user_stats":true}`, f.nodeID); err != nil {
		t.Fatal(err)
	}
	revision := f.publish(t)
	setQuota(t, f.ctx, f, account.ID, 1000, 0)
	batch := traffic.Batch{ID: "singbox-only", NodeID: f.nodeID, Counters: []traffic.Counter{{CoreEpoch: "singbox-process-1", Revision: revision, CredentialID: credential.ID, UserID: account.ID, DeviceID: credential.DeviceID, Protocol: credential.Protocol, UploadBytes: 100, DownloadBytes: 1000, ObservedAt: time.Now().UTC()}}}
	data, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.app.trafficService.RecordTraffic(f.ctx, f.nodeID, &pb.TrafficBatch{BatchJson: data}, true); err != nil {
			t.Fatal(err)
		}
	}
	flowWait(t, "sing-box quota automatic revision", func() bool { return len(f.autoRevisions(t)) == 1 })
	automatic := f.autoRevisions(t)[0]
	if automatic.NodeID != f.nodeID || stateHasCredential(f.state(t, automatic.ID), credential) {
		t.Fatal("sing-box over-quota credential survived")
	}
	var used int64
	if err := f.app.Database.QueryRow("SELECT used_traffic FROM users WHERE id=?", account.ID).Scan(&used); err != nil || used != 1100 {
		t.Fatal("batch retry doubled usage", used, err)
	}
	// The restarted core's smaller cumulative values belong to a new epoch.
	// Replaying either epoch must not add its previously accounted bytes again.
	batch.ID = "singbox-after-restart"
	batch.Counters[0].CoreEpoch = "singbox-process-2"
	batch.Counters[0].UploadBytes = 20
	batch.Counters[0].DownloadBytes = 200
	data, err = json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := f.app.trafficService.RecordTraffic(f.ctx, f.nodeID, &pb.TrafficBatch{BatchJson: data}, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.app.Database.QueryRow("SELECT used_traffic FROM users WHERE id=?", account.ID).Scan(&used); err != nil || used != 1320 {
		t.Fatal("core restart lost or doubled usage", used, err)
	}
}
