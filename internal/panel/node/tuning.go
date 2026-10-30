package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strconv"
	"strings"
	"time"
)

type tuningRule struct {
	min, max int64
	choices  string
}

// This is the panel's single whitelist. The privileged helper independently
// validates again, including actual congestion-control availability on the host.
var tuningRules = map[string]tuningRule{
	"net.ipv4.tcp_congestion_control": {choices: "bbr cubic reno htcp hybla bbr2 bbr3"},
	"net.core.default_qdisc":          {choices: "fq fq_codel cake pfifo_fast"},
	"net.ipv4.tcp_ecn":                {min: 0, max: 2}, "net.ipv4.tcp_tw_reuse": {min: 0, max: 2}, "net.ipv4.tcp_mtu_probing": {min: 0, max: 2},
	"fs.file-max":       {min: 65536, max: 16777216},
	"net.ipv4.tcp_rmem": {min: 4194304, max: 268435456}, "net.ipv4.tcp_wmem": {min: 4194304, max: 268435456},
	"net.core.somaxconn": {min: 128, max: 65535}, "net.ipv4.tcp_max_syn_backlog": {min: 128, max: 262144},
	"net.core.rmem_max": {min: 212992, max: 268435456}, "net.core.wmem_max": {min: 212992, max: 268435456},
	"net.ipv4.tcp_fastopen": {min: 0, max: 3},
}

func ValidateTuning(settings map[string]string, clear bool) error {
	if clear && len(settings) > 0 {
		return errors.New("clear requires empty settings")
	}
	for key, value := range settings {
		rule, ok := tuningRules[key]
		if !ok {
			return fmt.Errorf("unsupported tuning key: %s", key)
		}
		if rule.choices != "" {
			if !strings.Contains(" "+rule.choices+" ", " "+value+" ") || value == "" || strings.ContainsAny(value, " \t\r\n") {
				return fmt.Errorf("invalid value for %s", key)
			}
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || strconv.FormatInt(n, 10) != value || n < rule.min || n > rule.max {
			return fmt.Errorf("invalid value for %s", key)
		}
	}
	return nil
}

type TuningView struct {
	Supported   bool              `json:"supported"`
	Desired     map[string]string `json:"desired"`
	RequestID   *string           `json:"requestId"`
	Status      string            `json:"status"`
	Result      json.RawMessage   `json:"result"`
	RequestedAt *string           `json:"requestedAt"`
	UpdatedAt   *string           `json:"updatedAt"`
	Current     map[string]string `json:"current"`
}

func (h *Handler) tuning(w http.ResponseWriter, r *http.Request) {
	n, err := h.Repository.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, err)
		return
	}
	if n.DeletedAt != "" {
		h.fail(w, ErrNotFound)
		return
	}
	if r.Method == "GET" {
		v := TuningView{Supported: n.Capabilities.GetTuningSupported(), Desired: map[string]string{}, Status: "unconfirmed", Current: n.Heartbeat.GetTuningCurrent(), Result: json.RawMessage("null")}
		if v.Current == nil {
			v.Current = map[string]string{}
		}
		var desired string
		var result *string
		err = h.Repository.Database.QueryRowContext(r.Context(), "SELECT desired_json,request_id,status,result_json,requested_at,updated_at FROM node_tuning WHERE node_id=?", n.ID).Scan(&desired, &v.RequestID, &v.Status, &result, &v.RequestedAt, &v.UpdatedAt)
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
			}
		}
		httpapi.JSON(w, 200, v)
		return
	}
	var input struct {
		Settings      map[string]string `json:"settings"`
		Clear         bool              `json:"clear"`
		ConfirmNodeID string            `json:"confirmNodeId"`
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
	if err = tx.QueryRowContext(r.Context(), "SELECT enabled=1 AND deleted_at IS NULL AND status IN ('online','degraded') AND last_heartbeat>? FROM nodes WHERE id=?", timestamp(time.Now().Add(-45*time.Second)), n.ID).Scan(&available); err != nil {
		h.fail(w, err)
		return
	}
	if !available {
		httpapi.Error(w, 409, "node_offline", "node must be online")
		return
	}
	res, err := tx.ExecContext(r.Context(), `INSERT INTO node_tuning(node_id,desired_json,request_id,status,requested_at,updated_at) VALUES(?,?,?,'pending',?,?)
 ON CONFLICT(node_id) DO UPDATE SET desired_json=excluded.desired_json,request_id=excluded.request_id,status='pending',result_json=NULL,requested_at=excluded.requested_at,updated_at=excluded.updated_at WHERE node_tuning.status<>'pending'`, n.ID, string(raw), requestID, now, now)
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
	err := r.Database.QueryRowContext(ctx, "SELECT desired_json,request_id FROM node_tuning WHERE node_id=? AND status='pending'", node).Scan(&raw, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := &pb.TuningCommand{RequestId: id}
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
	if r == nil || r.RequestId == "" || len(r.RequestId) > 128 || (r.Status != "applied" && r.Status != "cleared" && r.Status != "failed") || len(r.Keys) > 14 {
		return errors.New("invalid tuning result")
	}
	seen := map[string]bool{}
	for _, k := range r.Keys {
		if k == nil || seen[k.Key] || len(k.Value) > 256 || len(k.Error) > 1024 {
			return errors.New("invalid tuning key result")
		}
		seen[k.Key] = true
		if _, ok := tuningRules[k.Key]; !ok && k.Key != "" {
			return errors.New("unknown tuning result key")
		}
		if r.Status == "failed" && k.Applied {
			return errors.New("failed tuning result claims applied")
		}
	}
	return nil
}
