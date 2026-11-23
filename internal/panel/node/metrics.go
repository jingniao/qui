package node

import (
	"context"
	"database/sql"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strconv"
	"strings"
	"time"
)

const metricColumns = "cpu_percent,memory_percent,disk_percent,memory_used_bytes,swap_used_bytes,disk_used_bytes,load1,process_count,tcp_connections,udp_sockets,active_connections,rx_bytes_per_second,tx_bytes_per_second"
const sampleInsertColumns = metricColumns + ",tcp_out_segs,tcp_retrans_segs"

// keepMeasured lists the rate-style columns an Agent can only compute some of
// the time (two heartbeats less than a second apart carry none). Within one
// minute a later NULL must not erase an earlier measurement; every other column
// keeps last-write-wins.
var keepMeasured = map[string]bool{"cpu_percent": true, "rx_bytes_per_second": true, "tx_bytes_per_second": true}

// HeartbeatFreshness is the age at which a stored heartbeat stops counting as a
// current measurement.
const HeartbeatFreshness = 45 * time.Second

// FreshSince returns the UTC cutoff for fresh stored heartbeats.
func FreshSince(now time.Time) string { return timestamp(now.Add(-HeartbeatFreshness)) }

// freshObservationTime returns the Agent's own message time when it is valid and
// inside the freshness window; a stale or invalid message is never sampled.
func freshObservationTime(m *pb.AgentMessage, now time.Time) (time.Time, bool) {
	if m.Timestamp == nil || m.Timestamp.CheckValid() != nil {
		return time.Time{}, false
	}
	at := m.Timestamp.AsTime()
	if at.Before(now.Add(-HeartbeatFreshness)) || at.After(now.Add(HeartbeatFreshness)) {
		return time.Time{}, false
	}
	return at, true
}

// freshHeartbeat reports whether a stored heartbeat time still counts as a
// current measurement.
func freshHeartbeat(at, now time.Time) bool { return now.Sub(at) <= HeartbeatFreshness }

// recordMetricTx stores the last fresh heartbeat in a UTC minute. Replayed
// sequence numbers never reach this function. Missing measurements stay NULL.
func recordMetricTx(ctx context.Context, tx *sql.Tx, node string, m *pb.AgentMessage, now time.Time) error {
	at, fresh := freshObservationTime(m, now)
	if !fresh {
		return nil
	}
	h := m.GetHeartbeat()
	var active any
	if h.GetActiveConnectionsMeasured() {
		active = h.ActiveConnections
	}
	var rx, ntx *float64
	for _, nic := range h.Interfaces {
		if nic == nil || nic.Name == "lo" {
			continue
		}
		if nic.ReceiveBytesPerSecond != nil {
			if rx == nil {
				rx = new(float64)
			}
			*rx += *nic.ReceiveBytesPerSecond
		}
		if nic.TransmitBytesPerSecond != nil {
			if ntx == nil {
				ntx = new(float64)
			}
			*ntx += *nic.TransmitBytesPerSecond
		}
	}
	assignments := []string{}
	for _, col := range strings.Split(sampleInsertColumns, ",") {
		if keepMeasured[col] {
			assignments = append(assignments, col+"=coalesce(excluded."+col+","+col+")")
		} else {
			assignments = append(assignments, col+"=excluded."+col)
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO node_metric_samples(node_id,at,"+sampleInsertColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(node_id,at) DO UPDATE SET "+strings.Join(assignments, ","), node, timestamp(at.Truncate(time.Minute)), h.CpuPercent, h.MemoryPercent, h.DiskPercent, h.MemoryUsedBytes, h.SwapUsedBytes, h.DiskUsedBytes, h.Load1, h.ProcessCount, h.TcpConnections, h.UdpSockets, active, rx, ntx, h.TcpOutSegs, h.TcpRetransSegs)
	return err
}

type MetricPoint struct {
	At                string   `json:"at"`
	CPUPercent        *float64 `json:"cpuPercent"`
	MemoryPercent     *float64 `json:"memoryPercent"`
	DiskPercent       *float64 `json:"diskPercent"`
	MemoryUsedBytes   *float64 `json:"memoryUsedBytes"`
	SwapUsedBytes     *float64 `json:"swapUsedBytes"`
	DiskUsedBytes     *float64 `json:"diskUsedBytes"`
	Load1             *float64 `json:"load1"`
	ProcessCount      *float64 `json:"processCount"`
	TCPConnections    *float64 `json:"tcpConnections"`
	UDPSockets        *float64 `json:"udpSockets"`
	ActiveConnections *float64 `json:"activeConnections"`
	RxBytesPerSecond  *float64 `json:"rxBytesPerSecond"`
	TxBytesPerSecond  *float64 `json:"txBytesPerSecond"`
	TCPRetransPercent *float64 `json:"tcpRetransPercent"`
	Samples           int      `json:"samples"`
}

func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if raw, ok := r.URL.Query()["hours"]; ok {
		var err error
		hours, err = strconv.Atoi(raw[0])
		if err != nil || hours < 1 || hours > 168 {
			httpapi.Error(w, 400, "invalid_hours", "hours must be between 1 and 168")
			return
		}
	}
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		h.fail(w, ErrNotFound)
		return
	}
	sinceTime := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	tx, err := h.Repository.Database.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	var coarse bool
	if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM node_metric_rollups WHERE node_id=? AND bucket>=?)`, n.ID, timestamp(sinceTime.Truncate(telemetryBucket))).Scan(&coarse); err != nil {
		h.fail(w, err)
		return
	}
	step := telemetryStepSeconds(hours, coarse)
	if coarse && !sinceTime.Equal(sinceTime.Truncate(telemetryBucket)) {
		sinceTime = sinceTime.Truncate(telemetryBucket).Add(telemetryBucket)
	}
	query, args := metricTierQuery(n.ID, sinceTime, step)
	rows, err := tx.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer rows.Close()
	points := []MetricPoint{}
	for rows.Next() {
		var p MetricPoint
		if err = rows.Scan(&p.At, &p.CPUPercent, &p.MemoryPercent, &p.DiskPercent, &p.MemoryUsedBytes, &p.SwapUsedBytes, &p.DiskUsedBytes, &p.Load1, &p.ProcessCount, &p.TCPConnections, &p.UDPSockets, &p.ActiveConnections, &p.RxBytesPerSecond, &p.TxBytesPerSecond, &p.TCPRetransPercent, &p.Samples); err != nil {
			h.fail(w, err)
			return
		}
		points = append(points, p)
	}
	if err = rows.Err(); err != nil {
		h.fail(w, err)
		return
	}
	storageResolution := 60
	if coarse {
		storageResolution = 300
	}
	httpapi.JSON(w, 200, map[string]any{"intervalSeconds": step, "sampleSeconds": 60, "storageResolutionSeconds": storageResolution, "from": timestamp(sinceTime), "points": points})
}

type MetricSample struct {
	At            string
	CPUPercent    *float64
	MemoryPercent *float64
	DiskPercent   *float64
}

func (r *Repository) RecentMetrics(ctx context.Context, since time.Time) (map[string][]MetricSample, error) {
	query := "SELECT node_id, at, cpu_percent, memory_percent, disk_percent FROM node_metric_samples WHERE at>=? ORDER BY at ASC"
	rows, err := r.Database.QueryContext(ctx, query, timestamp(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string][]MetricSample)
	for rows.Next() {
		var nodeID string
		var s MetricSample
		if err := rows.Scan(&nodeID, &s.At, &s.CPUPercent, &s.MemoryPercent, &s.DiskPercent); err != nil {
			return nil, err
		}
		res[nodeID] = append(res[nodeID], s)
	}
	return res, rows.Err()
}
