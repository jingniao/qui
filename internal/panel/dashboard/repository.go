package dashboard

import (
	"context"
	"database/sql"
	"fmt"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/statistics"
	"proxycontrol/internal/platform/db"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"proxycontrol/internal/panel/connections"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

// Repository reads dashboard aggregates. Connections is the panel's in-memory
// live connection view; when nil, the measured numbers read as unknown.
type Repository struct {
	Liveness    *node.Liveness
	Database    *sql.DB
	Connections *connections.Store
}

func (r *Repository) publishedTraffic(ctx context.Context, dayStart, monthStart, to time.Time) (statistics.ProxyData, error) {
	tx, err := db.BeginRead(ctx, r.Database)
	if err != nil {
		return statistics.ProxyData{}, err
	}
	defer tx.Close()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM nodes WHERE deleted_at IS NULL ORDER BY created_at,id")
	if err != nil {
		return statistics.ProxyData{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return statistics.ProxyData{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return statistics.ProxyData{}, err
	}
	if err := rows.Close(); err != nil {
		return statistics.ProxyData{}, err
	}
	return statistics.ReadProxyAt(ctx, tx, dayStart, monthStart, to, ids)
}

type BaseCounts struct {
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
	TodayBlocked        int64  `json:"todayBlocked"`
}
type Counts struct {
	BaseCounts
	TodayUpload         int64 `json:"todayUpload"`
	TodayDownload       int64 `json:"todayDownload"`
	TrafficObservations int64 `json:"trafficObservations"`
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

func (r *Repository) Counts(ctx context.Context, from, to, now time.Time) (BaseCounts, error) {
	result := BaseCounts{}
	var offlineAfterSeconds int
	if err := r.Database.QueryRowContext(ctx, "SELECT duration_minutes*60 FROM notification_rules WHERE kind='node_offline'").Scan(&offlineAfterSeconds); err != nil {
		return result, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,status,enabled,maintenance,last_heartbeat,heartbeat_json,capabilities_json FROM nodes WHERE deleted_at IS NULL")
	if err != nil {
		return result, err
	}
	defer rows.Close()
	facts := r.Liveness.Snapshot(now)
	for rows.Next() {
		n := node.Node{OfflineAfterSeconds: offlineAfterSeconds, Heartbeat: &pb.Heartbeat{}, Capabilities: &pb.Capabilities{}}
		var heartbeat, capabilities string
		if err := rows.Scan(&n.ID, &n.Status, &n.Enabled, &n.Maintenance, &n.LastHeartbeat, &heartbeat, &capabilities); err != nil {
			return result, err
		}
		n.Liveness = facts[n.ID]
		decode := protojson.UnmarshalOptions{DiscardUnknown: true}
		if err := decode.Unmarshal([]byte(heartbeat), n.Heartbeat); err != nil {
			return result, fmt.Errorf("dashboard node %s heartbeat: %w", n.ID, err)
		}
		if err := decode.Unmarshal([]byte(capabilities), n.Capabilities); err != nil {
			return result, fmt.Errorf("dashboard node %s capabilities: %w", n.ID, err)
		}
		// Match node reads' single-core report normalization before display.
		if !n.Capabilities.MultiCoreSupported {
			n.Capabilities.Cores = node.EnabledCores(n.Capabilities)
			if n.Capabilities.CoreName != "" && n.Heartbeat.CoreHealthy != nil {
				n.Heartbeat.Cores = []*pb.CoreStatus{{Name: n.Capabilities.CoreName, Healthy: n.Heartbeat.GetCoreHealthy(), Error: n.Heartbeat.CoreError, StartedAt: n.Heartbeat.CoreStartedAt, TrafficSourceStatus: n.Heartbeat.TrafficSourceStatus}}
			}
		}
		result.TotalNodes++
		if n.Enabled {
			result.ExpectedNodes++
		}
		switch node.DisplayStatus(n, now) {
		case "online":
			result.OnlineNodes++
		case "degraded", "core_error":
			result.OnlineNodes++
			result.AbnormalNodes++
		case "offline":
			result.OfflineNodes++
		case "error":
			result.AbnormalNodes++
		}
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if err := rows.Close(); err != nil {
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
