package dashboard

import (
	"context"
	"database/sql"
	"time"

	"proxycontrol/internal/panel/connections"
	"proxycontrol/pkg/contracts/traffic"
)

// Repository reads dashboard aggregates. Connections is the panel's in-memory
// live connection view; when nil, the measured numbers read as unknown.
type Repository struct {
	Database    *sql.DB
	Connections *connections.Store
}
type Counts struct {
	TotalNodes          int    `json:"totalNodes"`
	OnlineNodes         int    `json:"onlineNodes"`
	OfflineNodes        int    `json:"offlineNodes"`
	AbnormalNodes       int    `json:"abnormalNodes"`
	OnlineUsers         *int   `json:"onlineUsers"`
	OnlineDevices       *int   `json:"onlineDevices"`
	ActiveConnections   *int64 `json:"activeConnections"`
	MeasuredNodes       int    `json:"measuredNodes"`
	ExpectedNodes       int    `json:"expectedNodes"`
	MeasuredUsers       int    `json:"measuredUsers"`
	MeasuredDevices     int    `json:"measuredDevices"`
	MeasuredConnections int64  `json:"measuredConnections"`
	TodayUpload         int64  `json:"todayUpload"`
	TodayDownload       int64  `json:"todayDownload"`
	TodayBlocked        int64  `json:"todayBlocked"`
	TrafficObservations int64  `json:"trafficObservations"`
}
type Recent struct {
	Time        string `json:"time"`
	NodeName    string `json:"nodeName"`
	UserName    string `json:"userName"`
	DeviceName  string `json:"deviceName"`
	Domain      string `json:"domain"`
	TargetIP    string `json:"targetIp"`
	Action      string `json:"action"`
	Allowed     bool   `json:"allowed"`
	Connections int64  `json:"connections"`
}
type Hour struct {
	Time     string `json:"time"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
}

func (r *Repository) Counts(ctx context.Context, from, to, now time.Time) (Counts, error) {
	result := Counts{}
	threshold := now.Add(-45 * time.Second).UTC().Format("2006-01-02T15:04:05.000000000Z")
	err := r.Database.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(CASE WHEN enabled=1 AND status='online' AND last_heartbeat>=? THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN enabled=1 AND (status='offline' OR (status IN ('online','degraded') AND last_heartbeat<?)) THEN 1 ELSE 0 END),0),coalesce(sum(CASE WHEN enabled=1 AND (status='error' OR (status='degraded' AND last_heartbeat>=?)) THEN 1 ELSE 0 END),0) FROM nodes WHERE deleted_at IS NULL`, threshold, threshold, threshold).Scan(&result.TotalNodes, &result.OnlineNodes, &result.OfflineNodes, &result.AbnormalNodes)
	if err != nil {
		return result, err
	}
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM nodes WHERE enabled=1 AND deleted_at IS NULL").Scan(&result.ExpectedNodes); err != nil {
		return result, err
	}
	snapshots := []traffic.ConnectionSnapshot{}
	if r.Connections != nil {
		snapshots = r.Connections.Snapshots()
	}
	result.MeasuredNodes = len(snapshots)
	credentialIDs := []string{}
	for _, snapshot := range snapshots {
		result.MeasuredConnections += int64(snapshot.Connections)
		for _, account := range snapshot.Accounts {
			credentialIDs = append(credentialIDs, account.CredentialID)
		}
	}
	result.MeasuredUsers, result.MeasuredDevices, err = r.credentialScopes(ctx, credentialIDs)
	if err != nil {
		return result, err
	}
	if result.ExpectedNodes == result.MeasuredNodes {
		users, devices, connections := result.MeasuredUsers, result.MeasuredDevices, result.MeasuredConnections
		result.OnlineUsers = &users
		result.OnlineDevices = &devices
		result.ActiveConnections = &connections
	}
	err = r.Database.QueryRowContext(ctx, "SELECT coalesce(sum(upload_bytes),0),coalesce(sum(download_bytes),0),count(*) FROM account_traffic_samples WHERE observed_at>=? AND observed_at<?", from.UTC().Format("2006-01-02T15:04:05.000000000Z"), to.UTC().Format("2006-01-02T15:04:05.000000000Z")).Scan(&result.TodayUpload, &result.TodayDownload, &result.TrafficObservations)
	if err != nil {
		return result, err
	}
	err = r.Database.QueryRowContext(ctx, "SELECT coalesce(sum(connections),0) FROM access_rollups WHERE allowed=0 AND bucket>=? AND bucket<?", from.UTC().Format("2006-01-02T15:04:05Z"), to.UTC().Format("2006-01-02T15:04:05Z")).Scan(&result.TodayBlocked)
	return result, err
}

// credentialScopes resolves the distinct users and devices behind a set of
// credential IDs. It reads the whole credentials table instead of an IN list,
// so it is not bounded by the SQLite variable limit.
func (r *Repository) credentialScopes(ctx context.Context, credentialIDs []string) (int, int, error) {
	if len(credentialIDs) == 0 {
		return 0, 0, nil
	}
	wanted := map[string]bool{}
	for _, id := range credentialIDs {
		wanted[id] = true
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,user_id,device_id FROM credentials")
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	users := map[string]bool{}
	devices := map[string]bool{}
	for rows.Next() {
		var id, userID, deviceID string
		if err := rows.Scan(&id, &userID, &deviceID); err != nil {
			return 0, 0, err
		}
		if wanted[id] {
			users[userID] = true
			devices[deviceID] = true
		}
	}
	return len(users), len(devices), rows.Err()
}

func (r *Repository) Recent(ctx context.Context, blocked bool) ([]Recent, error) {
	where := ""
	if blocked {
		where = " WHERE a.allowed=0"
	}
	rows, err := r.Database.QueryContext(ctx, `SELECT a.bucket,coalesce(n.name,''),coalesce(u.username,''),coalesce(d.name,''),a.domain,a.target_ip,json_extract(a.dimensions_json,'$.action'),a.allowed,a.connections FROM access_rollups a LEFT JOIN nodes n ON n.id=a.node_id LEFT JOIN users u ON u.id=a.user_id LEFT JOIN devices d ON d.id=a.device_id`+where+" ORDER BY a.bucket DESC,a.id LIMIT 5")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Recent{}
	for rows.Next() {
		var item Recent
		if err := rows.Scan(&item.Time, &item.NodeName, &item.UserName, &item.DeviceName, &item.Domain, &item.TargetIP, &item.Action, &item.Allowed, &item.Connections); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Trend(ctx context.Context, from, to time.Time) ([]Hour, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT substr(observed_at,1,13)||':00:00Z',sum(upload_bytes),sum(download_bytes) FROM account_traffic_samples WHERE observed_at>=? AND observed_at<? GROUP BY substr(observed_at,1,13) ORDER BY substr(observed_at,1,13)", from.UTC().Format("2006-01-02T15:04:05.000000000Z"), to.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Hour{}
	for rows.Next() {
		var item Hour
		if err := rows.Scan(&item.Time, &item.Upload, &item.Download); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
