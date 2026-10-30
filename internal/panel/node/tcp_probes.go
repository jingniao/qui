package node

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net"
	"net/http"
	"net/netip"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/tcpprobe"
	"strconv"
	"strings"
	"time"
)

type ProbeTarget struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	AllowNonPublic bool   `json:"allowNonPublic"`
	Scheduled      bool   `json:"scheduled"`
}

func (t ProbeTarget) Validate() error {
	if strings.TrimSpace(t.Label) == "" || len(t.Label) > 120 || t.Port < 1 || t.Port > 65535 || len(t.Host) == 0 || len(t.Host) > 253 || strings.ContainsAny(t.Host, "/\\@?#% \t\r\n") {
		return errors.New("invalid probe target label or host:port")
	}
	if a, err := netip.ParseAddr(t.Host); err == nil {
		if !t.AllowNonPublic && !tcpprobe.Public(a) {
			return errors.New("non-public target requires explicit Owner permission")
		}
		return nil
	}
	for _, label := range strings.Split(t.Host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("invalid target hostname")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return errors.New("invalid target hostname")
			}
		}
	}
	return nil
}

func resolveProbe(ctx context.Context, t ProbeTarget) ([]string, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", t.Host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 || len(ips) > 16 {
		return nil, errors.New("target must resolve to 1–16 addresses")
	}
	addresses := []string{}
	for _, a := range ips {
		if !t.AllowNonPublic && !tcpprobe.Public(a) {
			return nil, errors.New("target resolves to a non-public address; explicit Owner permission required")
		}
		addresses = append(addresses, a.Unmap().String())
	}
	return addresses, nil
}

func (h *Handler) probeTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" || r.Method == "PUT" {
		var t ProbeTarget
		if err := httpapi.Decode(w, r, &t); err != nil {
			httpapi.Error(w, 400, "invalid_request", err.Error())
			return
		}
		if _, err := resolveProbe(r.Context(), t); err != nil {
			httpapi.Error(w, 400, "invalid_target", err.Error())
			return
		}
		if r.Method == "PUT" {
			t.ID = r.PathValue("target")
		} else {
			var err error
			t.ID, err = RandomID()
			if err != nil {
				h.fail(w, err)
				return
			}
		}
		query := "INSERT INTO probe_targets(id,label,host,port,allow_non_public,scheduled) VALUES(?,?,?,?,?,?)"
		args := []any{t.ID, t.Label, t.Host, t.Port, t.AllowNonPublic, t.Scheduled}
		if r.Method == "PUT" {
			query = "UPDATE probe_targets SET label=?,host=?,port=?,allow_non_public=?,scheduled=? WHERE id=?"
			args = []any{t.Label, t.Host, t.Port, t.AllowNonPublic, t.Scheduled, t.ID}
		}
		result, err := h.Repository.Database.ExecContext(r.Context(), query, args...)
		if err != nil {
			if strings.Contains(err.Error(), "at most 5 scheduled probe targets") {
				httpapi.Error(w, 409, "scheduled_target_limit", "at most 5 scheduled probe targets are allowed system-wide")
			} else {
				h.fail(w, err)
			}
			return
		}
		if n, _ := result.RowsAffected(); n == 0 {
			httpapi.Error(w, 404, "not_found", "probe target not found")
			return
		}
		status := 201
		if r.Method == "PUT" {
			status = 200
		}
		httpapi.JSON(w, status, t)
		return
	}
	rows, err := h.Repository.Database.QueryContext(r.Context(), "SELECT id,label,host,port,allow_non_public,scheduled FROM probe_targets ORDER BY label,id LIMIT 1000")
	if err != nil {
		h.fail(w, err)
		return
	}
	defer rows.Close()
	items := []ProbeTarget{}
	for rows.Next() {
		var t ProbeTarget
		if err := rows.Scan(&t.ID, &t.Label, &t.Host, &t.Port, &t.AllowNonPublic, &t.Scheduled); err != nil {
			h.fail(w, err)
			return
		}
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) deleteProbeTarget(w http.ResponseWriter, r *http.Request) {
	if _, err := h.Repository.Database.ExecContext(r.Context(), "DELETE FROM probe_targets WHERE id=?", r.PathValue("target")); err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, map[string]bool{"deleted": true})
}

func (h *Handler) tcpProbes(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		rows, err := h.Repository.Database.QueryContext(r.Context(), "SELECT id,target_id,status,result_json FROM node_tcp_probes WHERE source='manual' AND node_id=? AND target_id=? ORDER BY created_at DESC,id DESC LIMIT 100", r.PathValue("id"), r.URL.Query().Get("targetId"))
		if err != nil {
			h.fail(w, err)
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, target, status string
			var raw *string
			if err := rows.Scan(&id, &target, &status, &raw); err != nil {
				h.fail(w, err)
				return
			}
			var result *pb.TCPProbeResult
			if raw != nil {
				result = &pb.TCPProbeResult{}
				if json.Unmarshal([]byte(*raw), result) != nil {
					h.fail(w, errors.New("invalid probe result"))
					return
				}
			}
			items = append(items, map[string]any{"id": id, "targetId": target, "status": status, "result": result})
		}
		if err := rows.Err(); err != nil {
			h.fail(w, err)
			return
		}
		httpapi.JSON(w, 200, map[string]any{"items": items})
		return
	}
	var input struct {
		TargetID  string `json:"targetId"`
		Attempts  uint32 `json:"attempts"`
		TimeoutMs uint32 `json:"timeoutMs"`
	}
	if err := httpapi.Decode(w, r, &input); err != nil {
		httpapi.Error(w, 400, "invalid_request", err.Error())
		return
	}
	if input.Attempts < 1 || input.Attempts > 10 || input.TimeoutMs < 1 || input.TimeoutMs > 3000 {
		httpapi.Error(w, 400, "invalid_request", "attempts must be 1–10; timeout 1–3000 ms")
		return
	}
	if _, err := h.Repository.Get(r.Context(), r.PathValue("id")); err != nil {
		h.fail(w, err)
		return
	}
	var supported int
	if err := h.Repository.Database.QueryRowContext(r.Context(), "SELECT count(*) FROM nodes WHERE id=? AND tcp_probe_supported=1 AND enabled=1 AND deleted_at IS NULL AND last_heartbeat>=?", r.PathValue("id"), timestamp(time.Now().UTC().Add(-45*time.Second))).Scan(&supported); err != nil {
		h.fail(w, err)
		return
	}
	if supported == 0 {
		httpapi.Error(w, 409, "probe_unsupported", "node must be online with TCP probe support")
		return
	}
	var t ProbeTarget
	if err := h.Repository.Database.QueryRowContext(r.Context(), "SELECT id,label,host,port,allow_non_public FROM probe_targets WHERE id=?", input.TargetID).Scan(&t.ID, &t.Label, &t.Host, &t.Port, &t.AllowNonPublic); err != nil {
		h.fail(w, err)
		return
	}
	addresses, err := resolveProbe(r.Context(), t)
	if err != nil {
		httpapi.Error(w, 400, "invalid_target", err.Error())
		return
	}
	id, err := RandomID()
	if err != nil {
		h.fail(w, err)
		return
	}
	now := time.Now().UTC()
	deadline := now.Add(2 * time.Minute)
	c := &pb.TCPProbeCommand{Id: id, Addresses: addresses, Port: uint32(t.Port), Attempts: input.Attempts, TimeoutMs: input.TimeoutMs, AllowNonPublic: t.AllowNonPublic, Deadline: timestamppb.New(deadline)}
	raw, _ := json.Marshal(c)
	tx, err := h.Repository.Database.BeginTx(r.Context(), nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "UPDATE node_tcp_probes SET status='expired' WHERE node_id=? AND status='queued' AND deadline<=?", r.PathValue("id"), timestamp(now)); err != nil {
		h.fail(w, err)
		return
	}
	var pending int
	if err = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM node_tcp_probes WHERE node_id=? AND status='queued' AND source='manual'", r.PathValue("id")).Scan(&pending); err != nil {
		h.fail(w, err)
		return
	}
	if pending > 0 {
		httpapi.Error(w, 409, "probe_pending", "a probe is already pending for this node")
		return
	}
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,created_at,deadline,status) VALUES(?,?,?,?,?,?,'queued')", id, r.PathValue("id"), t.ID, string(raw), timestamp(now), timestamp(deadline)); err == nil {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM node_tcp_probes WHERE source='manual' AND node_id=? AND target_id=? AND id NOT IN (SELECT id FROM node_tcp_probes WHERE source='manual' AND node_id=? AND target_id=? ORDER BY created_at DESC,id DESC LIMIT 100)", r.PathValue("id"), t.ID, r.PathValue("id"), t.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 202, map[string]string{"id": id})
}

func (r *Repository) pendingTCPProbe(ctx context.Context, node string) (*pb.TCPProbeCommand, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := timestamp(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, "UPDATE node_tcp_probes SET status='expired' WHERE node_id=? AND status='queued' AND deadline<=?", node, now); err != nil {
		return nil, err
	}
	var raw, id string
	err = tx.QueryRowContext(ctx, "SELECT id,command_json FROM node_tcp_probes WHERE node_id=? AND status='queued' ORDER BY (dispatched_at IS NOT NULL) DESC,(source='manual') DESC,created_at,id LIMIT 1", node).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE node_tcp_probes SET dispatched_at=COALESCE(dispatched_at,?) WHERE id=?", now, id); err != nil {
		return nil, err
	}
	c := &pb.TCPProbeCommand{}
	if err = json.Unmarshal([]byte(raw), c); err != nil {
		return nil, err
	}
	return c, tx.Commit()
}

func (r *Repository) recordTCPProbe(ctx context.Context, node string, result *pb.TCPProbeResult) error {
	var raw, source string
	if err := r.Database.QueryRowContext(ctx, "SELECT command_json,source FROM node_tcp_probes WHERE id=? AND node_id=?", result.Id, node).Scan(&raw, &source); err != nil {
		return err
	}
	var c pb.TCPProbeCommand
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return err
	}
	if err := tcpprobe.ValidateResult(&c, result); err != nil {
		return err
	}
	if source == "scheduled" {
		tx, err := r.Database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO node_latency_samples(node_id,target_id,at,sent,failed,median_ms) SELECT node_id,target_id,created_at,?,?,? FROM node_tcp_probes WHERE id=? AND status='queued' AND deadline>?", result.Attempted, result.Lost, result.MedianMs, result.Id, timestamp(time.Now().UTC()))
		if err != nil {
			return err
		}
		// Retain an expired receipt until the next tick for duplicate results.
		if _, err = tx.ExecContext(ctx, "UPDATE node_tcp_probes SET status='expired' WHERE id=?", result.Id); err != nil {
			return err
		}
		return tx.Commit()
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = r.Database.ExecContext(ctx, "UPDATE node_tcp_probes SET status='complete',result_json=? WHERE id=? AND node_id=? AND status='queued' AND deadline>?", string(data), result.Id, node, timestamp(time.Now().UTC()))
	return err
}

// PanelReachability runs only on an explicit Owner request, never in /sub.
func PanelReachability(ctx context.Context, host string, port int, serverName string, useTLS bool, trustedPEM ...string) error {
	addresses, err := resolveProbe(ctx, ProbeTarget{Label: "self-hosted", Host: host, Port: port, AllowNonPublic: true})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(addresses[0], strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	if useTLS {
		if serverName == "" {
			serverName = host
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		for _, pem := range trustedPEM {
			if pem != "" && !roots.AppendCertsFromPEM([]byte(pem)) {
				return errors.New("invalid configured TLS trust")
			}
		}
		client := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName, RootCAs: roots})
		return client.HandshakeContext(ctx)
	}
	return nil
}
