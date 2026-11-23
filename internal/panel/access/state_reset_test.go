package access

import (
	"context"
	"proxycontrol/internal/platform/agentidentity"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
	"proxycontrol/pkg/contracts/traffic"
	"strings"
	"testing"
	"time"
)

func TestResetSealRejectsOldStreamAndResumesOriginalPending(t *testing.T) {
	ctx := context.Background()
	db, err := dbtest.Open(ctx, t.TempDir()+"/panel.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO nodes(id,name,status,created_at) VALUES('n','test','offline','now'); INSERT INTO node_identities(node_id,certificate_pem,fingerprint,expires_at) VALUES('n','pem','new','2999-01-01T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	key, err := vault.FromKey(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	r := &Repository{Database: db, Vault: key}
	batch := traffic.Batch{ID: "reset-pending", NodeID: "n", Counters: []traffic.Counter{{Kind: "node", CredentialID: "node", UserID: "node", DeviceID: "node", Core: "xray", CoreEpoch: "old.node", Revision: 1, ObservedAt: time.Now().UTC(), UploadBytes: 23, DownloadBytes: 42}}}
	if err := r.SavePendingTraffic(ctx, batch, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE traffic_pending_batches SET reset_id='reset'; INSERT INTO traffic_delivery_queues(node_id,queue_id,updated_at) VALUES('n','new-queue','2026-11-23T00:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	stale := agentidentity.WithIdentity(ctx, "n", strings.Repeat("a", 64))
	if err := r.IngestTraffic(stale, batch, time.Now()); err == nil {
		t.Fatal("old stream crossed reset seal")
	}
	resumed, err := agentidentity.WithPending(ctx, batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.IngestTraffic(resumed, batch, time.Now()); err != nil {
		t.Fatalf("original pending blocked by new queue: %v", err)
	}
	if err := r.CompletePendingTraffic(resumed, batch); err != nil {
		t.Fatal(err)
	}
	var reset string
	if err := db.QueryRow("SELECT reset_id FROM traffic_batches WHERE node_id='n' AND batch_id=?", batch.ID).Scan(&reset); err != nil || reset != "reset" {
		t.Fatalf("resumed receipt lost partition: %q %v", reset, err)
	}
}
