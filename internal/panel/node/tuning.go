package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"maps"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/tuning"
	"regexp"
	"time"
)

func ValidateTuning(settings map[string]string, clear bool) error {
	if clear && len(settings) > 0 {
		return errors.New("clear requires empty settings")
	}
	for key, value := range settings {
		if err := tuning.ValidateValue(key, value); err != nil {
			return err
		}
	}
	return nil
}

type TuningView struct {
	Supported             bool              `json:"supported"`
	Desired               map[string]string `json:"desired"`
	RequestID             *string           `json:"requestId"`
	Status                string            `json:"status"`
	Result                json.RawMessage   `json:"result"`
	RequestedAt           *string           `json:"requestedAt"`
	UpdatedAt             *string           `json:"updatedAt"`
	InstallMissingModules bool              `json:"installMissingModules"`
	Current               map[string]string `json:"current"`
	Initial               map[string]string `json:"initial"`
}

func (h *Handler) tuning(w http.ResponseWriter, r *http.Request) {
	n, err := h.Repository.GetForCoreSelection(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		h.fail(w, ErrNotFound)
		return
	}
	if r.Method == "GET" {
		v := TuningView{Supported: n.Capabilities.GetTuningSupported(), Desired: map[string]string{}, Status: "unconfirmed", Current: n.Heartbeat.GetTuningCurrent(), Initial: map[string]string{}, Result: json.RawMessage("null")}
		if v.Current == nil {
			v.Current = map[string]string{}
		}
		var desired string
		var result *string
		err = h.Repository.Database.QueryRowContext(r.Context(), "SELECT desired_json,request_id,status,result_json,requested_at,updated_at,install_missing_modules FROM node_tuning WHERE node_id=?", n.ID).Scan(&desired, &v.RequestID, &v.Status, &result, &v.RequestedAt, &v.UpdatedAt, &v.InstallMissingModules)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			h.fail(w, err)
			return
		}
		if err == nil {
			if json.Unmarshal([]byte(desired), &v.Desired) != nil {
				h.fail(w, errors.New("invalid tuning profile"))
				return
			}
			if result != nil {
				v.Result = json.RawMessage(*result)
				var parsed pb.TuningResult
				if protojson.Unmarshal([]byte(*result), &parsed) == nil && len(parsed.Initial) > 0 {
					v.Initial = maps.Clone(parsed.Initial)
				}
			}
		}
		httpapi.JSON(w, 200, v)
		return
	}
	var input struct {
		Settings              map[string]string `json:"settings"`
		Clear                 bool              `json:"clear"`
		ConfirmNodeID         string            `json:"confirmNodeId"`
		InstallMissingModules bool              `json:"installMissingModules"`
	}
	if err = httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if input.ConfirmNodeID != n.ID {
		httpapi.Error(w, 400, "invalid_request", "confirmNodeId must match node")
		return
	}
	if err = ValidateTuning(input.Settings, input.Clear); err != nil {
		httpapi.Error(w, 400, "invalid_tuning", err.Error())
		return
	}
	if !n.Capabilities.GetTuningSupported() {
		httpapi.Error(w, 409, "tuning_unsupported", "node does not support tuning")
		return
	}
	host := n.Capabilities.GetHost()
	if input.InstallMissingModules {
		if !host.GetTuningModuleInstallSupported() || host.GetTuningProbeStatus() != "ready" {
			httpapi.Error(w, 409, "tuning_module_install_unsupported", "此节点未报告 Ubuntu 当前内核模块补装能力")
			return
		}
		if input.Clear || !tuning.HasModuleTarget(input.Settings) {
			httpapi.Error(w, 400, "invalid_tuning", "补装只适用于 BBR 或 fq")
			return
		}
	}
	if err := tuning.CheckProbe(input.Settings, host.GetTuningProbeStatus(), host.GetTuningKeys()); err != nil {
		httpapi.Error(w, 409, "tuning_probe_unavailable", err.Error())
		return
	}
	if !n.Enabled || (n.Status != "online" && n.Status != "degraded") {
		httpapi.Error(w, 409, "node_offline", "node must be online")
		return
	}
	requestID, err := RandomID()
	if err != nil {
		h.fail(w, err)
		return
	}
	if input.Settings == nil {
		input.Settings = map[string]string{}
	}
	raw, _ := json.Marshal(input.Settings)
	now := timestamp(time.Now())
	tx, err := h.Repository.Database.BeginTx(r.Context(), nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	// Recheck under the write transaction: disconnect/deletion may have raced
	// the initial view read. Do not leave a new pending command on an offline node.
	var available bool
	if err = tx.QueryRowContext(r.Context(), "SELECT enabled=1 AND deleted_at IS NULL AND status IN ('online','degraded') AND last_heartbeat>? FROM nodes WHERE id=?", FreshSince(time.Now()), n.ID).Scan(&available); err != nil {
		h.fail(w, err)
		return
	}
	if !available {
		httpapi.Error(w, 409, "node_offline", "node must be online")
		return
	}
	res, err := tx.ExecContext(r.Context(), `INSERT INTO node_tuning(node_id,desired_json,request_id,status,requested_at,updated_at,install_missing_modules) VALUES(?,?,?,'pending',?,?,?)
 ON CONFLICT(node_id) DO UPDATE SET desired_json=excluded.desired_json,request_id=excluded.request_id,status='pending',result_json=NULL,requested_at=excluded.requested_at,updated_at=excluded.updated_at,install_missing_modules=excluded.install_missing_modules WHERE node_tuning.status<>'pending'`, n.ID, string(raw), requestID, now, now, input.InstallMissingModules)
	if err != nil {
		h.fail(w, err)
		return
	}
	count, err := res.RowsAffected()
	if err != nil {
		h.fail(w, err)
		return
	}
	if count == 0 {
		httpapi.Error(w, 409, "tuning_busy", "a tuning operation is in flight")
		return
	}
	if h.Audit != nil {
		err = h.Audit.RecordTx(r.Context(), tx, auth.Actor(r.Context()).Username, "修改服务器调优", n.Name+" ("+n.ID+")", "成功", r.RemoteAddr)
		if err != nil {
			h.fail(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]string{"requestId": requestID, "status": "pending"})
}

func (r *Repository) pendingTuning(ctx context.Context, node string) (*pb.TuningCommand, error) {
	var raw, id string
	var install bool
	err := r.Database.QueryRowContext(ctx, "SELECT desired_json,request_id,install_missing_modules FROM node_tuning WHERE node_id=? AND status='pending'", node).Scan(&raw, &id, &install)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := &pb.TuningCommand{RequestId: id, InstallMissingModules: install}
	if err = json.Unmarshal([]byte(raw), &c.Settings); err != nil {
		return nil, err
	}
	c.Clear = len(c.Settings) == 0
	return c, nil
}
func (r *Repository) unconfirmTuning(ctx context.Context, node string) error {
	_, err := r.Database.ExecContext(ctx, "UPDATE node_tuning SET status='unconfirmed',updated_at=? WHERE node_id=? AND status='pending'", timestamp(time.Now()), node)
	return err
}
func recordTuningTx(ctx context.Context, tx *sql.Tx, node string, result *pb.TuningResult) error {
	if err := validateTuningResult(result); err != nil {
		return err
	}
	raw, err := protojson.Marshal(result)
	if err != nil {
		return err
	}
	var id, status string
	var previous *string
	if err = tx.QueryRowContext(ctx, "SELECT request_id,status,result_json FROM node_tuning WHERE node_id=?", node).Scan(&id, &status, &previous); err != nil {
		return err
	}
	if id != result.RequestId {
		return errors.New("tuning request mismatch")
	}
	if previous != nil {
		var prior pb.TuningResult
		if protojson.Unmarshal([]byte(*previous), &prior) != nil {
			return errors.New("invalid stored tuning result")
		}
		old, _ := protojson.Marshal(&prior)
		if string(old) != string(raw) {
			return errors.New("conflicting tuning result")
		}
		return nil
	}
	if status != "pending" && status != "unconfirmed" {
		return errors.New("tuning request is closed")
	}
	_, err = tx.ExecContext(ctx, "UPDATE node_tuning SET status=?,result_json=?,updated_at=? WHERE node_id=? AND request_id=?", result.Status, string(raw), timestamp(time.Now()), node, result.RequestId)
	return err
}
func validateTuningResult(r *pb.TuningResult) error {
	return ValidateTuningResult(r)
}

// ValidateTuningResult is shared with portable archive validation so restored
// receipts obey the same constraints as authenticated stream receipts.
func ValidateTuningResult(r *pb.TuningResult) error {
	if r == nil || r.RequestId == "" || len(r.RequestId) > 128 || (r.Status != "applied" && r.Status != "cleared" && r.Status != "failed") || len(r.Keys) > len(tuning.Keys())+1 || len(r.Initial) > len(tuning.Keys()) {
		return errors.New("invalid tuning result")
	}
	seen := map[string]bool{}
	if len(r.InstalledModulePackages) > 2 {
		return errors.New("invalid module package count")
	}
	for _, name := range r.InstalledModulePackages {
		if len(name) > 160 || !modulePackageName.MatchString(name) || seen[name] {
			return errors.New("invalid module package receipt")
		}
		seen[name] = true
	}
	for _, k := range r.Keys {
		if k == nil || seen[k.Key] || len(k.Value) > 256 || len(k.Error) > 1024 {
			return errors.New("invalid tuning key result")
		}
		seen[k.Key] = true
		if !tuning.Known(k.Key) && k.Key != "" {
			return errors.New("unknown tuning result key")
		}
		if r.Status == "failed" && k.Applied {
			return errors.New("failed tuning result claims applied")
		}
	}
	for k, v := range r.Initial {
		if !tuning.Known(k) || len(v) > 256 {
			return errors.New("invalid tuning initial value")
		}
	}
	return nil
}

var modulePackageName = regexp.MustCompile(`^linux-modules-(extra-)?[0-9]+\.[0-9]+\.[0-9]+-[0-9]+-[a-z0-9][a-z0-9-]*$`)
