package node

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// RuntimeSnapshot is the node_runtime SSE payload. Its fields are shared with
// Viewer accounts and limited to the inventory's live display fields.
type RuntimeSnapshot struct {
	Generation        string      `json:"generation"`
	InventoryRevision uint64      `json:"inventoryRevision"`
	Node              RuntimeNode `json:"node"`
}

type RuntimeSnapshotList struct {
	Generation        string        `json:"generation"`
	Revision          uint64        `json:"revision"`
	InventoryRevision uint64        `json:"inventoryRevision"`
	Items             []RuntimeNode `json:"items"`
}

type RuntimeNode struct {
	NodeID              string           `json:"nodeId"`
	Revision            uint64           `json:"revision"`
	Removed             bool             `json:"removed,omitempty"`
	Status              string           `json:"status,omitempty"`
	Enabled             bool             `json:"enabled"`
	Maintenance         bool             `json:"maintenance"`
	DeletedAt           string           `json:"deletedAt"`
	LastHeartbeat       string           `json:"lastHeartbeat"`
	OfflineAfterSeconds int              `json:"offlineAfterSeconds"`
	Heartbeat           RuntimeHeartbeat `json:"heartbeat"`
}

type RuntimeHeartbeat struct {
	CPU                       *float64            `json:"cpu_percent,omitempty"`
	Memory                    *float64            `json:"memory_percent,omitempty"`
	Disk                      *float64            `json:"disk_percent,omitempty"`
	HostUptime                *float64            `json:"host_uptime_seconds,omitempty"`
	ReceiveBytesPerSecond     *float64            `json:"receive_bytes_per_second,omitempty"`
	TransmitBytesPerSecond    *float64            `json:"transmit_bytes_per_second,omitempty"`
	CurrentRevision           int64               `json:"current_revision,omitempty"`
	ActiveConnections         *uint64             `json:"active_connections,omitempty"`
	ActiveConnectionsMeasured *bool               `json:"active_connections_measured,omitempty"`
	CoreHealthy               *bool               `json:"core_healthy,omitempty"`
	Cores                     []RuntimeCoreHealth `json:"cores,omitempty"`
}

type RuntimeCoreHealth struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
}

// RuntimeSnapshots serializes committed database reads with revision assignment
// and SSE publication. A late callback re-reads current state under the lock,
// so it cannot overwrite a newer heartbeat, disable, maintenance or deletion.
type RuntimeSnapshots struct {
	queueMu           sync.Mutex
	queued            map[string]struct{}
	wake              chan struct{}
	mu                sync.Mutex
	database          *sql.DB
	publish           func(RuntimeSnapshot)
	generation        string
	revision          uint64
	inventoryRevision uint64
	items             map[string]runtimeEntry
}

type runtimeSource struct {
	id, status, deletedAt, lastHeartbeat string
	enabled, maintenance                 bool
	heartbeat, primaryCore, inventory    string
	multiCore                            bool
	offlineAfterSeconds                  int
}

type runtimeEntry struct {
	source runtimeSource
	node   RuntimeNode
}

// Only the heartbeat and the fields needed to detect an inventory change are
// read here. The fingerprint material stays internal to the store.
const runtimeSelect = `SELECT n.id,n.status,n.enabled,n.maintenance,coalesce(n.deleted_at,''),n.last_heartbeat,n.heartbeat_json,
 coalesce(json_extract(n.capabilities_json,'$.coreName'),json_extract(n.capabilities_json,'$.core_name'),''),
 coalesce(json_extract(n.capabilities_json,'$.multiCoreSupported'),json_extract(n.capabilities_json,'$.multi_core_supported'),0),
 json_array(n.name,n.hostname,n.operating_system,n.agent_version,n.public_ip,n.connect_host,n.capabilities_json,n.allow_subscription,
 n.outbound_source_ip,n.outbound_interface,n.host_source_ipv4,n.host_source_ipv6,
 m.region,m.remark,m.renew_date,m.renew_price,m.renew_currency,m.renew_cycle,m.renew_notice_days,m.tags_json,m.bandwidth_mbps,
 coalesce(json_extract(n.heartbeat_json,'$.tuningCurrent'),json_extract(n.heartbeat_json,'$.tuning_current')),
 (SELECT json_group_array(json_array(key,value,rules_version)) FROM (
   SELECT key,value,rules_version FROM node_tuning_dismissals WHERE node_id=n.id ORDER BY key)),
 (SELECT last_code FROM relay_cleanup_nodes WHERE node_id=n.id)),
 (SELECT duration_minutes*60 FROM notification_rules WHERE kind='node_offline')
 FROM nodes n LEFT JOIN node_meta m ON m.node_id=n.id`

func NewRuntimeSnapshots(database *sql.DB, publish func(RuntimeSnapshot)) (*RuntimeSnapshots, error) {
	generation, err := RandomID()
	if err != nil {
		return nil, err
	}
	return &RuntimeSnapshots{
		database: database, publish: publish, generation: generation, items: make(map[string]runtimeEntry),
		queued: make(map[string]struct{}), wake: make(chan struct{}, 1),
	}, nil
}

func scanRuntime(row scanner, now time.Time) (runtimeSource, error) {
	var source runtimeSource
	err := row.Scan(&source.id, &source.status, &source.enabled, &source.maintenance, &source.deletedAt, &source.lastHeartbeat,
		&source.heartbeat, &source.primaryCore, &source.multiCore, &source.inventory, &source.offlineAfterSeconds)
	if err == nil && (source.status == "online" || source.status == "degraded") {
		when, parseErr := time.Parse(time.RFC3339Nano, source.lastHeartbeat)
		if parseErr != nil || !freshHeartbeat(when, now) {
			source.status = "offline"
		}
	}
	return source, err
}

func runtimeMetrics(source runtimeSource) (RuntimeHeartbeat, error) {
	var heartbeat pb.Heartbeat
	if err := storedJSON.Unmarshal([]byte(source.heartbeat), &heartbeat); err != nil {
		return RuntimeHeartbeat{}, fmt.Errorf("decode heartbeat for node %s: %w", source.id, err)
	}
	result := RuntimeHeartbeat{
		CPU: heartbeat.CpuPercent, Memory: heartbeat.MemoryPercent, Disk: heartbeat.DiskPercent, HostUptime: heartbeat.HostUptimeSeconds,
		CurrentRevision: heartbeat.CurrentRevision, ActiveConnectionsMeasured: heartbeat.ActiveConnectionsMeasured, CoreHealthy: heartbeat.CoreHealthy,
	}
	if heartbeat.GetActiveConnectionsMeasured() {
		result.ActiveConnections = &heartbeat.ActiveConnections
	}
	if len(heartbeat.Interfaces) > 0 {
		var receive, transmit float64
		receiveMeasured, transmitMeasured := true, true
		for _, nic := range heartbeat.Interfaces {
			receive += nic.GetReceiveBytesPerSecond()
			transmit += nic.GetTransmitBytesPerSecond()
			receiveMeasured = receiveMeasured && nic.ReceiveBytesPerSecond != nil
			transmitMeasured = transmitMeasured && nic.TransmitBytesPerSecond != nil
		}
		if receiveMeasured {
			result.ReceiveBytesPerSecond = &receive
		}
		if transmitMeasured {
			result.TransmitBytesPerSecond = &transmit
		}
	}
	for _, core := range heartbeat.Cores {
		switch core.GetName() {
		case "xray", "sing-box", "gost":
			result.Cores = append(result.Cores, RuntimeCoreHealth{Name: core.Name, Healthy: core.Healthy})
		}
	}
	if !source.multiCore && result.CoreHealthy != nil {
		switch source.primaryCore {
		case "xray", "sing-box", "gost":
			result.Cores = []RuntimeCoreHealth{{Name: source.primaryCore, Healthy: *result.CoreHealthy}}
		}
	}
	return result, nil
}

func (s *RuntimeSnapshots) prepare(source runtimeSource) (runtimeEntry, error) {
	if previous, ok := s.items[source.id]; ok && previous.source == source {
		return previous, nil
	}
	metrics, err := runtimeMetrics(source)
	if err != nil {
		return runtimeEntry{}, err
	}
	return runtimeEntry{source: source, node: RuntimeNode{
		NodeID: source.id, Status: source.status, Enabled: source.enabled, Maintenance: source.maintenance,
		DeletedAt: source.deletedAt, LastHeartbeat: source.lastHeartbeat, Heartbeat: metrics, OfflineAfterSeconds: source.offlineAfterSeconds,
	}}, nil
}

func (s *RuntimeSnapshots) store(entry runtimeEntry, publish bool) RuntimeNode {
	if entry.node.Revision != 0 {
		return entry.node
	}
	previous, exists := s.items[entry.node.NodeID]
	s.revision++
	entry.node.Revision = s.revision
	if !exists || entry.node.Removed || previous.source.deletedAt != entry.source.deletedAt || previous.source.inventory != entry.source.inventory {
		s.inventoryRevision = s.revision
	}
	if entry.node.Removed {
		delete(s.items, entry.node.NodeID)
	} else {
		s.items[entry.node.NodeID] = entry
	}
	if publish {
		s.publish(RuntimeSnapshot{Generation: s.generation, InventoryRevision: s.inventoryRevision, Node: entry.node})
	}
	return entry.node
}

func (s *RuntimeSnapshots) Refresh(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, err := scanRuntime(s.database.QueryRowContext(ctx, runtimeSelect+" WHERE n.id=?", id), time.Now())
	if errors.Is(err, sql.ErrNoRows) {
		if _, exists := s.items[id]; exists {
			s.store(runtimeEntry{node: RuntimeNode{NodeID: id, Removed: true}}, true)
		}
		return nil
	}
	if err != nil {
		return err
	}
	entry, err := s.prepare(source)
	if err != nil {
		return err
	}
	s.store(entry, true)
	return nil
}

// List also reconciles persisted state. This covers an expired heartbeat,
// out-of-band writes and events lost when an SSE subscriber's buffer fills.
func (s *RuntimeSnapshots) List(ctx context.Context) (RuntimeSnapshotList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.database.QueryContext(ctx, runtimeSelect+" ORDER BY n.id")
	if err != nil {
		return RuntimeSnapshotList{}, err
	}
	defer rows.Close()
	now := time.Now()
	var entries []runtimeEntry
	seen := make(map[string]bool)
	for rows.Next() {
		source, err := scanRuntime(rows, now)
		if err != nil {
			return RuntimeSnapshotList{}, err
		}
		entry, err := s.prepare(source)
		if err != nil {
			return RuntimeSnapshotList{}, err
		}
		entries = append(entries, entry)
		seen[source.id] = true
	}
	if err := rows.Err(); err != nil {
		return RuntimeSnapshotList{}, err
	}
	// A cold list seeds the cache. Subsequent reconciliation publishes changes
	// so one reader cannot swallow a committed change ahead of its callback.
	publish := s.revision != 0
	items := make([]RuntimeNode, 0, len(entries))
	for _, entry := range entries {
		items = append(items, s.store(entry, publish))
	}
	for id := range s.items {
		if !seen[id] {
			s.store(runtimeEntry{node: RuntimeNode{NodeID: id, Removed: true}}, publish)
		}
	}
	return RuntimeSnapshotList{Generation: s.generation, Revision: s.revision, InventoryRevision: s.inventoryRevision, Items: items}, nil
}

// afterCommit only records the changed node. A slow display read must not
// extend a writer's request or delay an Agent's acknowledgment.
func (s *RuntimeSnapshots) afterCommit(id string) {
	if s == nil {
		return
	}
	s.queueMu.Lock()
	s.queued[id] = struct{}{}
	s.queueMu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
		// A worker wakeup is already queued. The ID stays in the pending set.
	}
}

func (s *RuntimeSnapshots) Run(ctx context.Context, logger *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		}
		s.queueMu.Lock()
		pending := s.queued
		s.queued = make(map[string]struct{})
		s.queueMu.Unlock()
		for id := range pending {
			if ctx.Err() != nil {
				return
			}
			if err := s.Refresh(ctx, id); err != nil && ctx.Err() == nil {
				logger.ErrorContext(ctx, "refresh node runtime snapshot", "node", id, "error", err)
			}
		}
	}
}

func (c *Control) notifyHeartbeat(id string) {
	if c.HeartbeatNotify != nil {
		c.HeartbeatNotify(id)
	} else if c.Notify != nil {
		c.Notify(id)
	}
}

func (h *Handler) runtimeSnapshots(w http.ResponseWriter, r *http.Request) {
	listing, err := h.Runtime.List(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, listing)
}
