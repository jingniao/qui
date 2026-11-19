// Package statistics prepares read-only panel display data. Its snapshots do
// not participate in ingestion, quota decisions, or accounting.
package statistics

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
)

type Hour struct {
	Time     string `json:"time"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
}

type NodeTraffic struct {
	NodeID        string `json:"nodeId"`
	TodayUpload   int64  `json:"todayUpload"`
	TodayDownload int64  `json:"todayDownload"`
	TodayMeasured bool   `json:"todayMeasured"`
	MonthUpload   int64  `json:"monthUpload"`
	MonthDownload int64  `json:"monthDownload"`
	Measured      bool   `json:"measured"`
	HistoryError  string `json:"historyError,omitempty"`
}

type Data struct {
	Timezone          string    `json:"timezone"`
	DayStart          time.Time `json:"dayStart"`
	MonthStart        time.Time `json:"monthStart"`
	TrafficIndexReady bool      `json:"trafficIndexReady"`
	ProxyData
	MachineTraffic []node.MachineTrafficSummary `json:"machineTraffic"`
	MachineError   string                       `json:"machineError,omitempty"`
}
type ProxyData struct {
	TodayUpload         int64         `json:"todayUpload"`
	TodayDownload       int64         `json:"todayDownload"`
	TrafficObservations int64         `json:"trafficObservations"`
	Trend               []Hour        `json:"trend"`
	NodeTraffic         []NodeTraffic `json:"nodeTraffic"`
	details             map[string]*nodeDetail
}

type nodeConfig struct {
	id, method, iface, automatic string
	allowance                    int64
	reset                        int
}
type configuration struct {
	zone, localDate string
	nodes           []nodeConfig
}

func (c *configuration) matches(other *configuration) bool {
	return c != nil && other != nil && c.zone == other.zone && c.localDate == other.localDate && slices.Equal(c.nodes, other.nodes)
}

// Only settings that affect this display (including the NIC selected by
// automatic mode) enter the key. Ordinary heartbeat counters stay out of it.
func (s *Service) configuration(ctx context.Context, tx *db.ReadSnapshot, at time.Time) (*configuration, error) {
	zone, err := s.nodes.Timezone(ctx, tx.Transaction())
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, err
	}
	c := &configuration{zone: zone, localDate: at.In(loc).Format(time.DateOnly)}
	rows, err := tx.QueryContext(ctx, `SELECT n.id, coalesce(s.method,'sum'), coalesce(s.interface_name,''),
		CASE WHEN coalesce(s.interface_name,'')='' THEN coalesce(json_extract(n.heartbeat_json,'$.defaultRouteInterface'),json_extract(n.heartbeat_json,'$.default_route_interface'),'') ELSE '' END,
		coalesce(s.allowance_bytes,-1), coalesce(s.reset_day,1)
		FROM nodes n LEFT JOIN node_machine_traffic_settings s ON s.node_id=n.id
		WHERE n.deleted_at IS NULL ORDER BY n.created_at,n.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n nodeConfig
		if err := rows.Scan(&n.id, &n.method, &n.iface, &n.automatic, &n.allowance, &n.reset); err != nil {
			return nil, err
		}
		c.nodes = append(c.nodes, n)
	}
	return c, rows.Err()
}

type nodeDetail struct {
	index   int
	samples int64
	trend   []Hour
}
type snapshot struct {
	View
	config *configuration
}

func (s *Service) build(ctx context.Context, tx *db.ReadSnapshot, c *configuration, asOf time.Time) (*snapshot, error) {
	loc, err := time.LoadLocation(c.zone)
	if err != nil {
		return nil, err
	}
	local := asOf.In(loc)
	data := &Data{Timezone: c.zone, DayStart: node.LocalDayStart(local.Year(), local.Month(), local.Day(), loc).UTC(), MonthStart: node.LocalDayStart(local.Year(), local.Month(), 1, loc).UTC()}
	if err := tx.QueryRowContext(ctx, "SELECT NOT EXISTS(SELECT 1 FROM traffic_sample_hours WHERE ready=0)").Scan(&data.TrafficIndexReady); err != nil {
		return nil, err
	}
	ids := make([]string, len(c.nodes))
	for i, n := range c.nodes {
		ids[i] = n.id
	}
	data.ProxyData, err = ReadProxyAt(ctx, tx, data.DayStart, data.MonthStart, asOf, ids)
	if err != nil {
		return nil, err
	}
	data.MachineTraffic, err = node.MachineTrafficAt(ctx, tx.Transaction(), asOf, c.zone)
	if err != nil {
		data.MachineError = err.Error()
		data.MachineTraffic = []node.MachineTrafficSummary{}
	}
	return &snapshot{View: View{Data: data, AsOf: &asOf}, config: c}, nil
}

// ReadProxyAt keeps the caller's exact half-open bounds. All retained samples
// contribute to the totals and chart. Only the supplied live nodes are ranked.
func ReadProxyAt(ctx context.Context, tx *db.ReadSnapshot, dayStart, monthStart, to time.Time, ids []string) (ProxyData, error) {
	data := ProxyData{NodeTraffic: make([]NodeTraffic, len(ids)), Trend: []Hour{}, details: make(map[string]*nodeDetail, len(ids))}
	for i, id := range ids {
		data.details[id] = &nodeDetail{index: i, trend: []Hour{}}
		data.NodeTraffic[i].NodeID = id
	}
	// A single per-node/hour query produces the chart, today's ranking and
	// totals. The underlying reader checks aggregate overflow before returning.
	today, err := access.ReadNodeTrafficSnapshot(ctx, tx, dayStart, to, "", true)
	if err != nil {
		return ProxyData{}, err
	}
	hours := map[string]Hour{}
	for _, v := range today {
		data.TodayUpload += v.Upload
		data.TodayDownload += v.Download
		data.TrafficObservations += v.Samples
		h := hours[v.Bucket]
		h.Time = v.Bucket
		h.Upload += v.Upload
		h.Download += v.Download
		hours[h.Time] = h
		if d := data.details[v.NodeID]; d != nil {
			item := &data.NodeTraffic[d.index]
			item.TodayUpload += v.Upload
			item.TodayDownload += v.Download
			d.samples += v.Samples
			item.TodayMeasured = d.samples > 0
			d.trend = append(d.trend, Hour{v.Bucket, v.Upload, v.Download})
		}
	}
	for _, h := range hours {
		data.Trend = append(data.Trend, h)
	}
	sort.Slice(data.Trend, func(i, j int) bool { return data.Trend[i].Time < data.Trend[j].Time })
	month, err := access.ReadNodeTrafficSnapshot(ctx, tx, monthStart, to, "", false)
	if errors.Is(err, access.ErrTrafficResolution) {
		for i := range data.NodeTraffic {
			data.NodeTraffic[i].HistoryError = err.Error()
		}
		return data, nil
	}
	if err != nil {
		return ProxyData{}, err
	}
	for _, v := range month {
		if d := data.details[v.NodeID]; d != nil {
			item := &data.NodeTraffic[d.index]
			item.MonthUpload = v.Upload
			item.MonthDownload = v.Download
			item.Measured = v.Samples > 0
		}
	}
	return data, nil
}

func (p *snapshot) forNode(id string) (View, error) {
	view := p.View
	if id == "" {
		return view, nil
	}
	d := p.Data.details[id]
	if d == nil {
		return View{}, node.ErrNotFound
	}
	data := *view.Data
	item := data.NodeTraffic[d.index]
	data.NodeTraffic = data.NodeTraffic[d.index : d.index+1]
	if data.MachineError == "" {
		data.MachineTraffic = data.MachineTraffic[d.index : d.index+1]
	}
	data.TodayUpload, data.TodayDownload = item.TodayUpload, item.TodayDownload
	data.TrafficObservations = d.samples
	data.Trend = d.trend
	view.Data = &data
	return view, nil
}
