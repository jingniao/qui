package node

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"time"
)

type HealthCheck struct {
	Core      string                `json:"core"`
	ID        string                `json:"id"`
	NodeID    string                `json:"nodeId"`
	Revision  int64                 `json:"revision"`
	Status    string                `json:"status"`
	CreatedAt string                `json:"createdAt"`
	Deadline  string                `json:"deadline"`
	Result    *pb.HealthCheckResult `json:"result,omitempty"`
}

func (r *Repository) healthCapability(ctx context.Context, nodeID string, supported bool) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE nodes SET health_check_supported=? WHERE id=?", supported, nodeID)
	return err
}
func (r *Repository) pendingHealthCheck(ctx context.Context, nodeID string) (*pb.HealthCheckCommand, error) {
	now := time.Now().UTC()
	if _, err := r.Database.ExecContext(ctx, "UPDATE node_health_checks SET status='expired' WHERE node_id=? AND status='queued' AND deadline<=?", nodeID, timestamp(now)); err != nil {
		return nil, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,revision,deadline,core FROM node_health_checks WHERE node_id=? AND status='queued' LIMIT 1", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var id, deadline, core string
	var revision int64
	if err := rows.Scan(&id, &revision, &deadline, &core); err != nil {
		return nil, err
	}
	limit, err := time.Parse(time.RFC3339Nano, deadline)
	if err != nil {
		return nil, err
	}
	return &pb.HealthCheckCommand{Core: core, Id: id, Revision: revision, Deadline: timestamppb.New(limit)}, nil
}
func (r *Repository) recordHealthCheck(ctx context.Context, nodeID string, result *pb.HealthCheckResult) error {
	if result == nil || result.Id == "" || len(result.Id) > 128 || result.Revision < 1 || result.ObservedAt == nil || result.ObservedAt.CheckValid() != nil {
		return errors.New("invalid health result")
	}
	switch result.Code {
	case "healthy", "core_unhealthy", "revision_changed", "expired", "unsupported":
	default:
		return errors.New("invalid health code")
	}
	if result.Healthy != (result.Code == "healthy") {
		return errors.New("inconsistent health result")
	}
	now := time.Now().UTC()
	observed := result.ObservedAt.AsTime()
	if observed.After(now.Add(5*time.Second)) || observed.Before(now.Add(-5*time.Minute)) {
		return errors.New("stale health result")
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revision int64
	var phase, deadline, core string
	if err := tx.QueryRowContext(ctx, "SELECT revision,status,deadline,core FROM node_health_checks WHERE id=? AND node_id=?", result.Id, nodeID).Scan(&revision, &phase, &deadline, &core); err != nil {
		return err
	}
	if revision != result.Revision || core != result.Core {
		return errors.New("health result revision mismatch")
	}
	if phase != "queued" {
		return nil
	}
	phase = "complete"
	if deadline <= timestamp(now) {
		phase = "expired"
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE node_health_checks SET status=?,result_json=? WHERE id=?", phase, string(encoded), result.Id); err != nil {
		return err
	}
	return tx.Commit()
}
func (h *Handler) requestHealthCheck(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		Core             string `json:"core"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil || input.ExpectedRevision < 1 {
		httpapi.Error(w, 400, "invalid_request", "A positive expected revision is required")
		return
	}
	id, err := RandomID()
	if err != nil {
		h.fail(w, err)
		return
	}
	now := time.Now().UTC()
	if input.Core != "" {
		n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
		if err != nil || !n.Capabilities.GetMultiCoreSupported() || SelectedCore(n.Capabilities, input.Core) == nil {
			httpapi.Error(w, 409, "core_unavailable", "Core not reported enabled")
			return
		}
	}
	item := HealthCheck{Core: input.Core, ID: id, NodeID: r.PathValue("id"), Revision: input.ExpectedRevision, Status: "queued", CreatedAt: timestamp(now), Deadline: timestamp(now.Add(5 * time.Minute))}
	tx, err := h.Repository.Database.BeginTx(r.Context(), nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	var eligible int
	if err := tx.QueryRowContext(r.Context(), `SELECT count(*) FROM nodes n JOIN node_runtime_state s ON s.node_id=n.id WHERE n.id=? AND n.enabled=1 AND n.status IN ('online','degraded') AND n.last_heartbeat>=? AND n.health_check_supported=1 AND s.revision=?`, item.NodeID, timestamp(now.Add(-45*time.Second)), item.Revision).Scan(&eligible); err != nil {
		h.fail(w, err)
		return
	}
	if eligible != 1 {
		httpapi.Error(w, 409, "health_unavailable", "Node is offline, unsupported or its revision changed")
		return
	}
	if _, err := tx.ExecContext(r.Context(), "UPDATE node_health_checks SET status='expired' WHERE node_id=? AND status='queued' AND deadline<=?", item.NodeID, item.CreatedAt); err != nil {
		h.fail(w, err)
		return
	}
	var pending int
	if err := tx.QueryRowContext(r.Context(), "SELECT count(*) FROM node_health_checks WHERE node_id=? AND status='queued'", item.NodeID).Scan(&pending); err != nil {
		h.fail(w, err)
		return
	}
	if pending > 0 {
		httpapi.Error(w, 409, "health_pending", "A health check is already pending")
		return
	}
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO node_health_checks(id,node_id,revision,status,created_at,deadline,created_by,core) VALUES(?,?,?,'queued',?,?,?,?)", item.ID, item.NodeID, item.Revision, item.CreatedAt, item.Deadline, auth.Actor(r.Context()).Username, item.Core); err != nil {
		h.fail(w, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'申请核心健康检查',?,'已受理',?)", item.CreatedAt, auth.Actor(r.Context()).Username, item.NodeID, r.RemoteAddr); err != nil {
		h.fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, item)
}
func (h *Handler) listHealthChecks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Repository.Database.QueryContext(r.Context(), "SELECT id,node_id,revision,status,created_at,deadline,coalesce(result_json,''),core FROM node_health_checks WHERE node_id=? ORDER BY created_at DESC LIMIT 10", r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	defer rows.Close()
	items := []HealthCheck{}
	for rows.Next() {
		var item HealthCheck
		var encoded string
		if err := rows.Scan(&item.ID, &item.NodeID, &item.Revision, &item.Status, &item.CreatedAt, &item.Deadline, &encoded, &item.Core); err != nil {
			h.fail(w, err)
			return
		}
		if item.Status == "queued" && item.Deadline < timestamp(time.Now()) {
			item.Status = "expired"
		}
		if encoded != "" {
			if json.Unmarshal([]byte(encoded), &item.Result) != nil {
				h.fail(w, errors.New("invalid persisted health observation"))
				return
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
