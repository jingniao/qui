package access

import (
	"context"
	"testing"
	"time"
)

func TestMonthlyNodeTotalsPanelTimezoneBoundaries(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	for i, at := range []string{"2024-01-31T15:59:59.999999999Z", "2024-01-31T16:00:00.000000000Z", "2024-02-29T15:59:59.999999999Z", "2024-02-29T16:00:00.000000000Z"} {
		if _, err := r.Database.Exec("INSERT INTO account_traffic_samples(node_id,epoch,credential_id,observed_at,user_id,device_id,upload_bytes,download_bytes) VALUES(?,'month',?,?,?, ?,?,?)", record.NodeID, i, at, record.UserID, record.DeviceID, (i+1)*10, (i+1)*100); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC)
	items, err := r.MonthlyNodes(ctx, now, "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	var upload, download int64
	if err = r.Database.QueryRow("SELECT sum(upload_bytes),sum(download_bytes) FROM account_traffic_samples WHERE observed_at>=? AND observed_at<? AND node_id=?", "2024-01-31T16:00:00.000000000Z", "2024-02-29T16:00:00.000000000Z", record.NodeID).Scan(&upload, &download); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.NodeID == record.NodeID {
			found = true
			if item.UploadBytes != upload || item.DownloadBytes != download || upload != 50 || download != 500 {
				t.Fatal(item, upload, download)
			}
		}
	}
	if !found {
		t.Fatal("missing node")
	}
}
