package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

type Totals struct {
	NodeGroupInbounds     int64 `json:"nodeGroupInbounds"`
	NodeGroupImports      int64 `json:"nodeGroupImports"`
	NodeLatencyTargets    int64 `json:"nodeLatencyTargets"`
	LatencySamples        int64 `json:"latencySamples"`
	ProbeTargets          int64 `json:"probeTargets"`
	TCPProbes             int64 `json:"tcpProbes"`
	NodeMeta              int64 `json:"nodeMeta"`
	PendingTrafficBatches int64 `json:"pendingTrafficBatches,omitempty"`
	Admins                int64 `json:"admins"`
	ActiveOwners          int64 `json:"activeOwners"`
	Sessions              int64 `json:"sessions"`
	PortalSessions        int64 `json:"portalSessions"`
	SubscriptionSources   int64 `json:"subscriptionSources"`
	ImportedProxies       int64 `json:"importedProxies"`
	SourceGrants          int64 `json:"sourceGrants"`
	Users                 int64 `json:"users"`
	Devices               int64 `json:"devices"`
	Nodes                 int64 `json:"nodes"`
	NodeGroups            int64 `json:"nodeGroups"`
	Policies              int64 `json:"policies"`
	Revisions             int64 `json:"revisions"`
	CanvasSchemes         int64 `json:"canvasSchemes"`
	PendingOperations     int64 `json:"pendingOperations"`
	BackupFileOperations  int64 `json:"backupFileOperations"`
	RunningBackupJobs     int64 `json:"runningBackupJobs"`
}
type NodeRecord struct {
	ID              string
	Sequence        int64
	RuntimeRevision int64
}
type IdentityRecord struct {
	NodeID, Fingerprint string
	Revoked             bool
}
type UserRecord struct {
	TrafficResetCycle, TrafficResetAnchor, TrafficResetAt string
	DeviceLimit                                           int
	ID, Status                                            string
	Used, Limit                                           int64
}

// Inventory is internal read-only evidence. No credentials, token values,
// private material, or arbitrary row contents are included.
type Inventory struct {
	Totals     Totals
	Nodes      map[string]NodeRecord
	Identities map[string]IdentityRecord
	Users      map[string]UserRecord
}
type Reader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ReadInventory(ctx context.Context, r Reader) (Inventory, error) {
	result := Inventory{Nodes: map[string]NodeRecord{}, Identities: map[string]IdentityRecord{}, Users: map[string]UserRecord{}}
	for _, item := range []struct {
		table string
		count *int64
	}{{"node_group_inbounds", &result.Totals.NodeGroupInbounds}, {"node_group_imports", &result.Totals.NodeGroupImports}, {"node_latency_targets", &result.Totals.NodeLatencyTargets}, {"node_latency_samples", &result.Totals.LatencySamples}, {"probe_targets", &result.Totals.ProbeTargets}, {"node_tcp_probes", &result.Totals.TCPProbes}, {"node_meta", &result.Totals.NodeMeta}, {"subscription_sources", &result.Totals.SubscriptionSources}, {"imported_proxies", &result.Totals.ImportedProxies}, {"user_source_grants", &result.Totals.SourceGrants}} {
		var present int
		if err := r.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", item.table).Scan(&present); err != nil {
			return Inventory{}, err
		}
		if present > 0 {
			if err := r.QueryRowContext(ctx, "SELECT count(*) FROM "+item.table).Scan(item.count); err != nil {
				return Inventory{}, err
			}
		}
	}
	var pendingTrafficTable int
	if err := r.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='traffic_pending_batches'").Scan(&pendingTrafficTable); err != nil {
		return Inventory{}, err
	}
	if pendingTrafficTable != 0 {
		if err := r.QueryRowContext(ctx, "SELECT count(*) FROM traffic_pending_batches").Scan(&result.Totals.PendingTrafficBatches); err != nil {
			return Inventory{}, err
		}
	}
	// A backup taken before migration 0112 has no canvas table; the count stays
	// zero instead of failing the inventory of an older archive.
	var canvasTable int
	if err := r.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='canvas_schemes'").Scan(&canvasTable); err != nil {
		return Inventory{}, err
	}
	if canvasTable != 0 {
		if err := r.QueryRowContext(ctx, "SELECT count(*) FROM canvas_schemes").Scan(&result.Totals.CanvasSchemes); err != nil {
			return Inventory{}, err
		}
	}
	// As with canvas schemes, pre-0114 archives have no portal sessions.
	var portalTable int
	if err := r.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='user_sessions'").Scan(&portalTable); err != nil {
		return Inventory{}, err
	}
	if portalTable != 0 {
		if err := r.QueryRowContext(ctx, "SELECT count(*) FROM user_sessions").Scan(&result.Totals.PortalSessions); err != nil {
			return Inventory{}, err
		}
	}
	var updatePending int64
	for _, table := range []string{"core_update_tasks", "agent_update_tasks"} {
		var present, pending int64
		if err := r.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&present); err != nil {
			return Inventory{}, err
		}
		if present != 0 {
			if err := r.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE status IN ('queued','running','failed_closed')").Scan(&pending); err != nil {
				return Inventory{}, err
			}
			updatePending += pending
		}
	}
	for _, item := range []struct {
		query  string
		target *int64
	}{
		{"SELECT count(*) FROM admins", &result.Totals.Admins}, {"SELECT count(*) FROM admins WHERE role='Owner' AND status='active'", &result.Totals.ActiveOwners}, {"SELECT count(*) FROM sessions", &result.Totals.Sessions},
		{"SELECT count(*) FROM users", &result.Totals.Users}, {"SELECT count(*) FROM devices", &result.Totals.Devices}, {"SELECT count(*) FROM nodes", &result.Totals.Nodes}, {"SELECT count(*) FROM node_groups", &result.Totals.NodeGroups}, {"SELECT count(*) FROM policies", &result.Totals.Policies},
		{"SELECT count(*) FROM config_revisions", &result.Totals.Revisions},
		{"SELECT (SELECT count(*) FROM deployment_targets WHERE status='pending')+(SELECT count(*) FROM deployment_commands WHERE status='pending')+(SELECT count(*) FROM deployment_batches WHERE status IN ('deploying','rolling_back','rollback_failed'))", &result.Totals.PendingOperations},
	} {
		if err := r.QueryRowContext(ctx, item.query).Scan(item.target); err != nil || *item.target < 0 {
			return Inventory{}, errors.New("restore inventory query failed")
		}
	}
	for _, item := range []struct {
		table, query string
		target       *int64
	}{{"backup_file_ops", "SELECT count(*) FROM backup_file_ops", &result.Totals.BackupFileOperations}, {"backup_runs", "SELECT count(*) FROM backup_runs WHERE status='running'", &result.Totals.RunningBackupJobs}} {
		var exists int
		if err := r.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", item.table).Scan(&exists); err != nil {
			return Inventory{}, err
		}
		if exists == 1 {
			if err := r.QueryRowContext(ctx, item.query).Scan(item.target); err != nil {
				return Inventory{}, err
			}
		}
	}
	result.Totals.PendingOperations += updatePending
	if result.Totals.Users > 10000 || result.Totals.Nodes > 10000 {
		return Inventory{}, errors.New("restore inventory exceeds reconciliation limits")
	}
	rows, err := r.QueryContext(ctx, "SELECT n.id,n.last_sequence,coalesce(s.revision,0) FROM nodes n LEFT JOIN node_runtime_state s ON s.node_id=n.id")
	if err != nil {
		return Inventory{}, err
	}
	for rows.Next() {
		var node NodeRecord
		if err := rows.Scan(&node.ID, &node.Sequence, &node.RuntimeRevision); err != nil {
			rows.Close()
			return Inventory{}, err
		}
		if node.Sequence < 0 || node.RuntimeRevision < 0 {
			rows.Close()
			return Inventory{}, ErrArchive
		}
		result.Nodes[node.ID] = node
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Inventory{}, err
	}
	rows, err = r.QueryContext(ctx, "SELECT node_id,fingerprint,coalesce(revoked_at,'')<>'' FROM node_identities")
	if err != nil {
		return Inventory{}, err
	}
	for rows.Next() {
		var identity IdentityRecord
		if err := rows.Scan(&identity.NodeID, &identity.Fingerprint, &identity.Revoked); err != nil {
			rows.Close()
			return Inventory{}, err
		}
		result.Identities[identity.NodeID] = identity
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Inventory{}, err
	}
	var deviceLimitApplied int
	if err := r.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0115_device_limit.sql'").Scan(&deviceLimitApplied); err != nil {
		return Inventory{}, err
	}
	deviceColumn := "0"
	if deviceLimitApplied != 0 {
		deviceColumn = "device_limit"
	}
	var resetApplied int
	if err := r.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0118_traffic_reset.sql'").Scan(&resetApplied); err != nil {
		return Inventory{}, err
	}
	resetColumns := "'none','',''"
	if resetApplied > 0 {
		resetColumns = "traffic_reset_cycle,traffic_reset_anchor,traffic_reset_at"
	}
	rows, err = r.QueryContext(ctx, "SELECT id,status,used_traffic,traffic_limit,"+deviceColumn+","+resetColumns+" FROM users")
	if err != nil {
		return Inventory{}, err
	}
	for rows.Next() {
		var user UserRecord
		if err := rows.Scan(&user.ID, &user.Status, &user.Used, &user.Limit, &user.DeviceLimit, &user.TrafficResetCycle, &user.TrafficResetAnchor, &user.TrafficResetAt); err != nil {
			rows.Close()
			return Inventory{}, err
		}
		if user.Used < 0 || user.Limit < 0 || user.DeviceLimit < 0 || user.DeviceLimit > 100 {
			rows.Close()
			return Inventory{}, ErrArchive
		}
		result.Users[user.ID] = user
	}
	err = rows.Err()
	rows.Close()
	return result, err
}

// InventoryFromVerifiedDirectory is for Examine's validated callback only.
func InventoryFromVerifiedDirectory(ctx context.Context, work string) (Inventory, error) {
	database, err := readonlyDatabase(filepath.Join(work, "database", "panel.db"))
	if err != nil {
		return Inventory{}, err
	}
	defer database.Close()
	return ReadInventory(ctx, database)
}

type Materials struct {
	KeyDigest, NodeCADigest, ActiveCADigest, RetiredCADigest, SuccessorCADigest, SuccessorRetiredCADigest [sha256.Size]byte
	// Current recovered-root material: the independent protected base, the
	// recovered-round head and its retirement proof. They are zero when the
	// corresponding file is absent, so archives and installations that predate
	// the recovered root stay byte-compatible.
	RecoveredRootBaseDigest, RecoveredAuthorityDigest, RecoveredRetirementDigest [sha256.Size]byte
}

// OriginalMaterials returns the historical-coordinator comparison subset: only
// the original configuration key and the ordinary ca/active/retired/successor
// material. The independent recovered current-root base and the recovered-round
// head are authorized separately against the durable recovered-root publication
// and must not block the original coordinator gate.
func (m Materials) OriginalMaterials() Materials {
	m.RecoveredRootBaseDigest = [sha256.Size]byte{}
	m.RecoveredAuthorityDigest = [sha256.Size]byte{}
	m.RecoveredRetirementDigest = [sha256.Size]byte{}
	return m
}

func ReadMaterials(identityDirectory string) (Materials, error) {
	var result Materials
	for _, item := range []struct {
		name   string
		limit  int64
		target *[sha256.Size]byte
	}{{"configuration.key", 32, &result.KeyDigest}, {"ca.crt", 1 << 20, &result.NodeCADigest}} {
		path := filepath.Join(identityDirectory, item.name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > item.limit {
			return Materials{}, ErrArchive
		}
		data, err := os.ReadFile(path)
		if err != nil || int64(len(data)) != info.Size() {
			return Materials{}, ErrArchive
		}
		*item.target = sha256.Sum256(data)
		clear(data)
	}
	activePath := filepath.Join(identityDirectory, "node-ca-active.enc")
	if info, err := os.Lstat(activePath); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 32768 {
			return Materials{}, ErrArchive
		}
		data, err := os.ReadFile(activePath)
		if err != nil || int64(len(data)) != info.Size() {
			return Materials{}, ErrArchive
		}
		result.ActiveCADigest = sha256.Sum256(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Materials{}, err
	}
	retiredPath := filepath.Join(identityDirectory, "node-ca-retired.enc")
	if info, err := os.Lstat(retiredPath); err == nil {
		if !info.Mode().IsRegular() || info.Size() > 32768 {
			return Materials{}, ErrArchive
		}
		data, err := os.ReadFile(retiredPath)
		if err != nil || int64(len(data)) != info.Size() {
			return Materials{}, ErrArchive
		}
		result.RetiredCADigest = sha256.Sum256(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Materials{}, err
	}
	for _, item := range []struct {
		name   string
		limit  int64
		target *[sha256.Size]byte
	}{{"node-ca-round-active.enc", 1 << 20, &result.SuccessorCADigest}, {"node-ca-round-retired.enc", 32768, &result.SuccessorRetiredCADigest}} {
		info, err := os.Lstat(filepath.Join(identityDirectory, item.name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > item.limit {
			return Materials{}, ErrArchive
		}
		data, err := os.ReadFile(filepath.Join(identityDirectory, item.name))
		if err != nil || int64(len(data)) != info.Size() {
			return Materials{}, ErrArchive
		}
		*item.target = sha256.Sum256(data)
	}
	// The current recovered root and its head are ordinary identity material for
	// a full digest comparison; an absent file leaves a zero digest so an
	// archive or installation without a recovered root stays comparable. The
	// historical coordinator gate uses OriginalMaterials() to exclude these.
	for _, item := range []struct {
		name   string
		limit  int64
		target *[sha256.Size]byte
	}{{"node-ca-recovered-base.enc", 32768, &result.RecoveredRootBaseDigest}, {"node-ca-recovered-round.enc", 1 << 20, &result.RecoveredAuthorityDigest}, {"node-ca-recovered-retired.enc", 32768, &result.RecoveredRetirementDigest}} {
		info, err := os.Lstat(filepath.Join(identityDirectory, item.name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > item.limit {
			return Materials{}, ErrArchive
		}
		data, err := os.ReadFile(filepath.Join(identityDirectory, item.name))
		if err != nil || int64(len(data)) != info.Size() {
			return Materials{}, ErrArchive
		}
		*item.target = sha256.Sum256(data)
	}
	return result, nil
}
