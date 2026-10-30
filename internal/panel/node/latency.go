package node

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
	"log/slog"
	"net/http"
	"proxycontrol/internal/platform/httpapi"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strconv"
	"sync/atomic"
	"time"
)

const LatencyInterval = 5 * time.Minute

type LatencyScheduler struct {
	Repository *Repository
	Interval   time.Duration
	Logger     *slog.Logger
	running    atomic.Bool
}

// Run belongs to the serving instance's background context and wait group.
func (s *LatencyScheduler) Run(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	interval := s.Interval
	if interval <= 0 {
		interval = LatencyInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	seen := map[string]bool{}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			err := s.Repository.scheduleLatency(ctx, now.UTC())
			if err != nil && ctx.Err() == nil && !seen[err.Error()] {
				seen[err.Error()] = true
				if s.Logger != nil {
					s.Logger.Warn("scheduled TCP probe failed", "error", err)
				}
			}
		}
	}
}

func (r *Repository) scheduleLatency(ctx context.Context, now time.Time) error {
	if _, err := r.Database.ExecContext(ctx, "DELETE FROM node_latency_samples WHERE at<?", timestamp(now.Add(-7*24*time.Hour))); err != nil {
		return err
	}
	if _, err := r.Database.ExecContext(ctx, "DELETE FROM node_tcp_probes WHERE source='scheduled' AND (status<>'queued' OR deadline<=?)", timestamp(now)); err != nil {
		return err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,label,host,port,allow_non_public FROM probe_targets WHERE scheduled=1 ORDER BY id")
	if err != nil {
		return err
	}
	targets := []ProbeTarget{}
	for rows.Next() {
		var t ProbeTarget
		if err = rows.Scan(&t.ID, &t.Label, &t.Host, &t.Port, &t.AllowNonPublic); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var firstErr error
	for _, t := range targets {
		addresses, err := resolveProbe(ctx, t)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		rows, err := r.Database.QueryContext(ctx, "SELECT id FROM nodes WHERE enabled=1 AND deleted_at IS NULL AND tcp_probe_supported=1 AND last_heartbeat>=?", timestamp(now.Add(-45*time.Second)))
		if err != nil {
			return err
		}
		nodes := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			nodes = append(nodes, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, node := range nodes {
			id, err := RandomID()
			if err != nil {
				return err
			}
			deadline := now.Add(2 * time.Minute)
			command := &pb.TCPProbeCommand{Id: id, Addresses: addresses, Port: uint32(t.Port), Attempts: 3, TimeoutMs: 3000, AllowNonPublic: t.AllowNonPublic, Deadline: timestamppb.New(deadline)}
			raw, err := json.Marshal(command)
			if err != nil {
				return err
			}
			// One outstanding scheduled job per node/target; dispatch serializes nodes.
			_, err = r.Database.ExecContext(ctx, `INSERT INTO node_tcp_probes(id,node_id,target_id,command_json,created_at,deadline,status,source)
    SELECT ?,?,?,?,?,?,'queued','scheduled'
    WHERE EXISTS(SELECT 1 FROM probe_targets WHERE id=? AND scheduled=1)
    AND NOT EXISTS(SELECT 1 FROM node_tcp_probes WHERE node_id=? AND target_id=? AND source='scheduled' AND status='queued')`, id, node, t.ID, string(raw), timestamp(now), timestamp(deadline), t.ID, node, t.ID)
			if err != nil {
				return err
			}
		}
	}
	return firstErr
}

type latencyTarget struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
}
type latencyPoint struct {
	At       string   `json:"at"`
	AvgMs    *float64 `json:"avgMs"`
	MinMs    *float64 `json:"minMs"`
	MaxMs    *float64 `json:"maxMs"`
	MedianMs *float64 `json:"medianMs"`
	Sent     int      `json:"sent"`
	Failed   int      `json:"failed"`
}
type latencySeries struct {
	TargetID string         `json:"targetId"`
	Points   []latencyPoint `json:"points"`
}

func (h *Handler) latency(w http.ResponseWriter, r *http.Request) {
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
	since := timestamp(time.Now().UTC().Add(-time.Duration(hours) * time.Hour))
	rows, err := h.Repository.Database.QueryContext(r.Context(), `SELECT id,label,host,port FROM probe_targets t WHERE scheduled=1
  OR EXISTS(SELECT 1 FROM node_latency_samples s WHERE s.target_id=t.id AND s.node_id=? AND s.at>=?) ORDER BY label,id`, r.PathValue("id"), since)
	if err != nil {
		h.fail(w, err)
		return
	}
	targets := []latencyTarget{}
	for rows.Next() {
		var t latencyTarget
		if err = rows.Scan(&t.ID, &t.Label, &t.Host, &t.Port); err != nil {
			rows.Close()
			h.fail(w, err)
			return
		}
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		h.fail(w, err)
		return
	}
	series := []latencySeries{}
	for _, t := range targets {
		item := latencySeries{TargetID: t.ID, Points: []latencyPoint{}}
		rows, err = h.Repository.Database.QueryContext(r.Context(), "SELECT at,avg_ms,min_ms,max_ms,median_ms,sent,failed FROM node_latency_samples WHERE node_id=? AND target_id=? AND at>=? ORDER BY at", r.PathValue("id"), t.ID, since)
		if err != nil {
			h.fail(w, err)
			return
		}
		for rows.Next() {
			var p latencyPoint
			if err = rows.Scan(&p.At, &p.AvgMs, &p.MinMs, &p.MaxMs, &p.MedianMs, &p.Sent, &p.Failed); err != nil {
				rows.Close()
				h.fail(w, err)
				return
			}
			item.Points = append(item.Points, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			h.fail(w, err)
			return
		}
		series = append(series, item)
	}
	httpapi.JSON(w, 200, map[string]any{"intervalSeconds": int(LatencyInterval / time.Second), "targets": targets, "series": series})
}
