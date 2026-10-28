package deployment

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"

	"google.golang.org/protobuf/encoding/protojson"
	"proxycontrol/internal/platform/id"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

// CoreUpdateTask is one direct-URL core installation attempt. The Owner supplies
// the component, the stable version, the official release asset URL and the
// archive SHA-256; the Agent verifies everything again on the node.
type CoreUpdateTask struct {
	ID        string                `json:"id"`
	NodeID    string                `json:"nodeId"`
	Component string                `json:"component"`
	Version   string                `json:"version"`
	URL       string                `json:"url"`
	SHA256    string                `json:"sha256"`
	Sequence  int64                 `json:"sequence"`
	Revision  int64                 `json:"revision"`
	Status    string                `json:"status"`
	CommandID string                `json:"commandId"`
	CreatedBy string                `json:"createdBy"`
	CreatedAt string                `json:"createdAt"`
	UpdatedAt string                `json:"updatedAt"`
	Result    *release.UpdateResult `json:"result,omitempty"`
}

// CoreUpdateInput is the Owner-supplied target of a core installation.
type CoreUpdateInput struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
}

const coreTaskColumns = "id,node_id,baseline_revision,component,version,url,sha256,sequence,status,command_id,created_by,created_at,updated_at,result_json"

// ReleaseAccess reads signed catalogue entries and records update operations in
// the shared admin operation log. Core updates themselves no longer use the
// signed catalogue; it stays for Agent self-update.
type ReleaseAccess interface {
	ReleaseTx(context.Context, *sql.Tx, string) (release.CatalogueEntry, error)
	RecordTx(context.Context, *sql.Tx, string, string, string, string, string) error
}

func scanCoreTask(row interface{ Scan(...any) error }) (CoreUpdateTask, error) {
	var t CoreUpdateTask
	var data []byte
	err := row.Scan(&t.ID, &t.NodeID, &t.Revision, &t.Component, &t.Version, &t.URL, &t.SHA256, &t.Sequence, &t.Status, &t.CommandID, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &data)
	if err != nil {
		return t, err
	}
	if len(data) > 0 {
		result, err := release.DecodeCoreUpdateResult(data)
		if err != nil {
			return t, err
		}
		t.Result = &result
	}
	return t, nil
}

// ValidateCoreUpdateTarget refuses everything that could make a node download
// something other than an official, stable, correctly hashed release asset for
// its own platform.
func ValidateCoreUpdateTarget(component, version, assetURL, digest, platform string) error {
	if !release.CoreComponent(component) || !release.StableCoreVersion(version) {
		return release.ErrUpdate
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != digest {
		return release.ErrUpdate
	}
	u, _, err := release.CoreAssetURL(component, assetURL)
	if err != nil {
		return err
	}
	osName, architecture, ok := splitPlatform(platform)
	if !ok || !release.CoreAssetMatches(component, osName, architecture, path.Base(u.Path)) {
		return release.ErrPolicy
	}
	return nil
}

func splitPlatform(platform string) (string, string, bool) {
	for index := 0; index < len(platform); index++ {
		if platform[index] != '/' {
			continue
		}
		osName, architecture := platform[:index], platform[index+1:]
		if (osName == "linux" || osName == "windows") && (architecture == "amd64" || architecture == "arm64") {
			return osName, architecture, true
		}
		return "", "", false
	}
	return "", "", false
}

// storedJSON reads capabilities_json written by this or a newer panel: fields a
// later version added must not make the stored record unreadable.
var storedJSON = protojson.UnmarshalOptions{DiscardUnknown: true}

// supportsQuiCore requires explicit support; old Agents keep official sources.
// A core version or tag alone cannot establish an Agent's URL support.
func supportsQuiCore(capabilities *pb.Capabilities) bool {
	return capabilities != nil && capabilities.SingboxUserStats
}

func coreUpdateAvailable(ctx context.Context, tx *sql.Tx, nodeID string) error {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM core_update_tasks WHERE node_id=? AND status IN ('queued','running')", nodeID).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrNodeBusy
	}
	return nil
}

// QueueCoreUpdate validates the node, its reported core and platform, the
// requested target and the revision the node has confirmed, then reserves the
// single active update slot of that node.
func (r *Repository) QueueCoreUpdate(ctx context.Context, audit ReleaseAccess, nodeID, actor, ip string, expected int64, input CoreUpdateInput) (CoreUpdateTask, error) {
	if expected < 1 || actor == "" || len(actor) > 128 || !release.CoreComponent(input.Component) || !release.StableCoreVersion(input.Version) {
		return CoreUpdateTask{}, release.ErrUpdate
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return CoreUpdateTask{}, err
	}
	defer tx.Rollback()
	if err := nodeAvailable(ctx, tx, nodeID, ""); err != nil {
		return CoreUpdateTask{}, err
	}
	if err := coreUpdateAvailable(ctx, tx, nodeID); err != nil {
		return CoreUpdateTask{}, err
	}
	var enabled, supported bool
	var platform string
	var caps []byte
	var revision int64
	err = tx.QueryRowContext(ctx, "SELECT n.enabled,n.operating_system,n.capabilities_json,coalesce(c.core_supported,0),s.revision FROM nodes n LEFT JOIN node_update_capabilities c ON c.node_id=n.id JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=?", nodeID).Scan(&enabled, &platform, &caps, &supported, &revision)
	if err != nil {
		return CoreUpdateTask{}, err
	}
	if !enabled || !supported || revision != expected {
		return CoreUpdateTask{}, ErrNodeBusy
	}
	capabilities := &pb.Capabilities{}
	if storedJSON.Unmarshal(caps, capabilities) != nil || capabilities.CoreName != input.Component {
		return CoreUpdateTask{}, release.ErrPolicy
	}
	if release.QuiCoreSource(input.URL) && !supportsQuiCore(capabilities) {
		return CoreUpdateTask{}, release.ErrPolicy
	}
	if err := ValidateCoreUpdateTarget(input.Component, input.Version, input.URL, input.SHA256, platform); err != nil {
		return CoreUpdateTask{}, err
	}
	var next int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(sequence),0)+1 FROM core_update_tasks WHERE node_id=?", nodeID).Scan(&next); err != nil {
		return CoreUpdateTask{}, err
	}
	taskID, err := id.New()
	if err != nil {
		return CoreUpdateTask{}, err
	}
	commandID, err := id.New()
	if err != nil {
		return CoreUpdateTask{}, err
	}
	commandID = "upd_" + commandID
	t := CoreUpdateTask{ID: taskID, NodeID: nodeID, Component: input.Component, Version: input.Version, URL: input.URL, SHA256: input.SHA256, Sequence: next, Revision: expected, Status: "queued", CommandID: commandID, CreatedBy: actor, CreatedAt: now(), UpdatedAt: now()}
	if _, err := tx.ExecContext(ctx, "INSERT INTO core_update_tasks("+coreTaskColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL)", t.ID, t.NodeID, t.Revision, t.Component, t.Version, t.URL, t.SHA256, t.Sequence, t.Status, t.CommandID, t.CreatedBy, t.CreatedAt, t.UpdatedAt); err != nil {
		return t, err
	}
	if err := audit.RecordTx(ctx, tx, actor, "queue_core_update", taskID, "成功", ip); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

func (r *Repository) ListCoreUpdates(ctx context.Context, page, size int) ([]CoreUpdateTask, int, error) {
	var total int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM core_update_tasks").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT "+coreTaskColumns+" FROM core_update_tasks ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []CoreUpdateTask{}
	for rows.Next() {
		t, err := scanCoreTask(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}
	return items, total, rows.Err()
}

// coreUpdateRequest rebuilds the immutable command of a reserved task.
func coreUpdateRequest(t CoreUpdateTask) (release.CoreUpdateRequest, error) {
	request := release.CoreUpdateRequest{SchemaVersion: 2, ID: t.CommandID, NodeID: t.NodeID, Revision: t.Revision, Sequence: t.Sequence, Component: t.Component, Version: t.Version, URL: t.URL, SHA256: t.SHA256}
	return request, request.Validate()
}

// legacyCoreUpdateCode is the result code recorded when a stored task cannot be
// compiled into a schema-2 command, which is what migration 0107 leaves behind
// for a historical task: its direct URL is empty. The code reuses the "original
// update history is unusable" meaning the panel already explains to the Owner.
const legacyCoreUpdateCode = "history_invalid"

// closeUndispatchableCoreUpdate closes a reserved task that no longer forms a
// valid command. The node keeps asking for its active task on every reconnect,
// so the task is closed once, with the reason, instead of failing the control
// stream forever.
func (r *Repository) closeUndispatchableCoreUpdate(ctx context.Context, t CoreUpdateTask) error {
	result := release.UpdateResult{ID: t.CommandID, NodeID: t.NodeID, Revision: t.Revision, TargetDigest: t.SHA256, Status: "failed_closed", Code: legacyCoreUpdateCode}
	// A row the old schema never validated (for example an unusable digest) is
	// closed without a result payload, because the panel refuses to read back a
	// result it cannot verify.
	var payload any
	if result.ValidateCore() == nil {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		payload = data
	}
	_, err := r.Database.ExecContext(ctx, "UPDATE core_update_tasks SET status='failed_closed',result_json=?,updated_at=? WHERE command_id=? AND status IN ('queued','running')", payload, now(), t.CommandID)
	return err
}

// PendingCoreUpdate hands the reserved command to the node and marks it running.
func (r *Repository) PendingCoreUpdate(ctx context.Context, nodeID string) (*pb.CoreUpdateCommand, error) {
	t, err := scanCoreTask(r.Database.QueryRowContext(ctx, "SELECT "+coreTaskColumns+" FROM core_update_tasks WHERE node_id=? AND status IN ('queued','running')", nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	request, err := coreUpdateRequest(t)
	if err != nil {
		return nil, r.closeUndispatchableCoreUpdate(ctx, t)
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := r.Database.ExecContext(ctx, "UPDATE core_update_tasks SET status='running',updated_at=? WHERE command_id=? AND status='queued'", now(), t.CommandID); err != nil {
		return nil, err
	}
	return &pb.CoreUpdateCommand{RequestJson: data}, nil
}

// CoreUpdateResult records the node's authenticated outcome for its own task. A
// reconnect that closed the task without an outcome still accepts the
// authenticated result the Agent produced meanwhile, because that outcome is the
// only proof of what happened on the node. That holds for both closures: a task
// closed as failed_closed, and one closed as success because the node already
// reported the target core version (CloseStaleCoreUpdates). A task the Owner
// cleared keeps its status and its result is refused.
func (r *Repository) CoreUpdateResult(ctx context.Context, nodeID string, payload *pb.CoreUpdateResult) error {
	result, err := release.DecodeCoreUpdateResult(payload.ResultJson)
	if err != nil || result.NodeID != nodeID {
		return release.ErrUpdate
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := scanCoreTask(tx.QueryRowContext(ctx, "SELECT "+coreTaskColumns+" FROM core_update_tasks WHERE command_id=? AND node_id=?", result.ID, nodeID))
	if err != nil {
		return err
	}
	if result.Revision != t.Revision || result.TargetDigest != t.SHA256 {
		return release.ErrUpdate
	}
	if result.Status == "success" && (result.InstalledVersion != t.Version || result.InstalledSequence < 1) {
		return release.ErrUpdate
	}
	if t.Result != nil {
		if *t.Result != result {
			return release.ErrUpdate
		}
		return nil
	}
	// The task must still be open, or closed without an outcome. A task the
	// Owner cleared or otherwise finished keeps its status. A task closed as
	// success without a result was closed by CloseStaleCoreUpdates, because the
	// node already reported the requested version; the receipt the node then
	// delivers is still the only proof of what happened on it.
	update, err := tx.ExecContext(ctx, "UPDATE core_update_tasks SET status=?,result_json=?,updated_at=? WHERE id=? AND command_id=? AND (status IN ('queued','running') OR (status IN ('failed_closed','success') AND result_json IS NULL))", result.Status, payload.ResultJson, now(), t.ID, result.ID)
	if err != nil {
		return err
	}
	rows, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		// Never acknowledge a result this panel did not store.
		return release.ErrUpdate
	}
	return tx.Commit()
}

// CloseStaleCoreUpdates closes a dispatched update that was never confirmed. A
// node that already reports the requested core version succeeded; anything
// else is closed as failed so the slot is free for a new attempt.
func (r *Repository) CloseStaleCoreUpdates(ctx context.Context, nodeID string) error {
	var commandID, version string
	err := r.Database.QueryRowContext(ctx, "SELECT command_id,version FROM core_update_tasks WHERE node_id=? AND status='running'", nodeID).Scan(&commandID, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var caps []byte
	if err := r.Database.QueryRowContext(ctx, "SELECT capabilities_json FROM nodes WHERE id=?", nodeID).Scan(&caps); err != nil {
		return err
	}
	capabilities := &pb.Capabilities{}
	if storedJSON.Unmarshal(caps, capabilities) != nil {
		return release.ErrUpdate
	}
	closed := "failed_closed"
	if capabilities.CoreName != "" && capabilities.CoreVersion == version {
		closed = "success"
	}
	_, err = r.Database.ExecContext(ctx, "UPDATE core_update_tasks SET status=?,updated_at=? WHERE command_id=? AND status='running'", closed, now(), commandID)
	return err
}

// ClearCoreUpdate closes a task the Owner no longer wants to track. A task that
// already stored the node's authenticated result is finished: clearing it keeps
// its status and result, because backup inspection requires every stored result
// to keep matching the status of its task. Only a task without a result becomes
// 'cleared', and the UPDATE repeats that condition so a result stored meanwhile
// is never overwritten.
func (r *Repository) ClearCoreUpdate(ctx context.Context, audit ReleaseAccess, taskID, actor, ip string) (CoreUpdateTask, error) {
	if actor == "" || len(actor) > 128 {
		return CoreUpdateTask{}, release.ErrUpdate
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return CoreUpdateTask{}, err
	}
	defer tx.Rollback()
	t, err := scanCoreTask(tx.QueryRowContext(ctx, "SELECT "+coreTaskColumns+" FROM core_update_tasks WHERE id=?", taskID))
	if err != nil {
		return t, err
	}
	if t.Result == nil && (t.Status == "queued" || t.Status == "running" || t.Status == "failed_closed") {
		updatedAt := now()
		update, err := tx.ExecContext(ctx, "UPDATE core_update_tasks SET status='cleared',updated_at=? WHERE id=? AND result_json IS NULL", updatedAt, t.ID)
		if err != nil {
			return t, err
		}
		rows, err := update.RowsAffected()
		if err != nil {
			return t, err
		}
		if rows == 1 {
			t.Status, t.UpdatedAt = "cleared", updatedAt
		}
	}
	if err := audit.RecordTx(ctx, tx, actor, "clear_core_update", t.ID, "成功", ip); err != nil {
		return t, err
	}
	return t, tx.Commit()
}
