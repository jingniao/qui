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

// heartbeatFreshness is the age at which a stored heartbeat stops counting as a
// current measurement.
const heartbeatFreshness = 45 * time.Second

// freshObservationTime returns the Agent's own message time when it is valid and
// inside the freshness window; a stale or invalid message is never sampled.
func freshObservationTime(m *pb.AgentMessage, now time.Time) (time.Time, bool) {
	if m.Timestamp == nil || m.Timestamp.CheckValid() != nil {
		return time.Time{}, false
	}
	at := m.Timestamp.AsTime()
	if at.Before(now.Add(-heartbeatFreshness)) || at.After(now.Add(heartbeatFreshness)) {
		return time.Time{}, false
	}
	return at, true
}

// freshHeartbeat reports whether a stored heartbeat time still counts as a
// current measurement.
func freshHeartbeat(at, now time.Time) bool { return now.Sub(at) <= heartbeatFreshness }

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
	for _, col := range strings.Split(metricColumns, ",") {
		assignments = append(assignments, col+"=excluded."+col)
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO node_metric_samples(node_id,at,"+metricColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(node_id,at) DO UPDATE SET "+strings.Join(assignments, ","), node, timestamp(at.Truncate(time.Minute)), h.CpuPercent, h.MemoryPercent, h.DiskPercent, h.MemoryUsedBytes, h.SwapUsedBytes, h.DiskUsedBytes, h.Load1, h.ProcessCount, h.TcpConnections, h.UdpSockets, active, rx, ntx)
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
	step := latencyStepSeconds(hours)
	since := timestamp(time.Now().UTC().Add(-time.Duration(hours) * time.Hour))
	query := "SELECT at," + metricColumns + ",1 FROM node_metric_samples WHERE node_id=? AND at>=? ORDER BY at"
	args := []any{n.ID, since}
	if step > 60 {
		avgs := []string{}
		for _, col := range strings.Split(metricColumns, ",") {
			avgs = append(avgs, "AVG("+col+")")
		}
		query = "WITH samples AS (SELECT *,CAST(strftime('%s',substr(at,1,19)||'Z') AS INTEGER)/?*? AS bucket FROM node_metric_samples WHERE node_id=? AND at>=?) SELECT strftime('%Y-%m-%dT%H:%M:%S',bucket,'unixepoch')||'.000000000Z'," + strings.Join(avgs, ",") + ",COUNT(*) FROM samples GROUP BY bucket ORDER BY bucket"
		args = []any{step, step, n.ID, since}
	}
	rows, err := h.Repository.Database.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer rows.Close()
	points := []MetricPoint{}
	for rows.Next() {
		var p MetricPoint
		if err = rows.Scan(&p.At, &p.CPUPercent, &p.MemoryPercent, &p.DiskPercent, &p.MemoryUsedBytes, &p.SwapUsedBytes, &p.DiskUsedBytes, &p.Load1, &p.ProcessCount, &p.TCPConnections, &p.UDPSockets, &p.ActiveConnections, &p.RxBytesPerSecond, &p.TxBytesPerSecond, &p.Samples); err != nil {
			h.fail(w, err)
			return
		}
		points = append(points, p)
	}
	if err = rows.Err(); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"intervalSeconds": step, "sampleSeconds": 60, "points": points})
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
