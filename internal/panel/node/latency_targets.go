package node

import (
	"database/sql"
	"errors"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
)

// n is the eligible node; the parameter is the candidate target ID.
const effectiveLatencyTarget = `EXISTS(SELECT 1 FROM probe_targets et WHERE et.id=? AND
 ((n.latency_targets_custom=0 AND et.scheduled=1) OR
 (n.latency_targets_custom=1 AND EXISTS(SELECT 1 FROM node_latency_targets nt WHERE nt.node_id=n.id AND nt.target_id=et.id))))`

type latencyTargetsSettings struct {
	Custom           bool     `json:"custom"`
	TargetIDs        []string `json:"targetIds"`
	DefaultTargetIDs []string `json:"defaultTargetIds"`
}

func (h *Handler) latencyTargets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	tx, err := h.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		h.fail(w, err)
		return
	}
	defer tx.Rollback()
	settings := latencyTargetsSettings{TargetIDs: []string{}, DefaultTargetIDs: []string{}}
	err = tx.QueryRowContext(ctx, "SELECT latency_targets_custom FROM nodes WHERE id=? AND deleted_at IS NULL", id).Scan(&settings.Custom)
	if errors.Is(err, sql.ErrNoRows) {
		h.fail(w, ErrNotFound)
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	if r.Method == "PUT" {
		var input struct {
			Custom    bool     `json:"custom"`
			TargetIDs []string `json:"targetIds"`
		}
		if err = httpapi.Decode(w, r, &input); err != nil {
			httpapi.Error(w, 400, "invalid_request", err.Error())
			return
		}
		seen := map[string]bool{}
		for _, target := range input.TargetIDs {
			if seen[target] {
				httpapi.Error(w, 400, "invalid_request", "targetIds must be unique")
				return
			}
			seen[target] = true
		}
		if len(seen) > 5 {
			httpapi.Error(w, 409, "node_target_limit", "at most 5 node latency targets are allowed")
			return
		}
		for target := range seen {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM probe_targets WHERE id=?", target).Scan(&count); err != nil {
				h.fail(w, err)
				return
			}
			if count == 0 {
				httpapi.Error(w, 400, "unknown_target", "probe target not found")
				return
			}
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM node_latency_targets WHERE node_id=?", id); err != nil {
			h.fail(w, err)
			return
		}
		if input.Custom {
			for _, target := range input.TargetIDs {
				if _, err = tx.ExecContext(ctx, "INSERT INTO node_latency_targets(node_id,target_id) VALUES(?,?)", id, target); err != nil {
					h.fail(w, err)
					return
				}
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE nodes SET latency_targets_custom=? WHERE id=?", input.Custom, id); err == nil {
			err = h.Audit.RecordTx(ctx, tx, auth.Actor(ctx).Username, "修改节点延迟目标", id, "成功", r.RemoteAddr)
		}
		if err != nil {
			h.fail(w, err)
			return
		}
		settings.Custom = input.Custom
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,scheduled,EXISTS(SELECT 1 FROM node_latency_targets nt WHERE nt.node_id=? AND nt.target_id=t.id) FROM probe_targets t ORDER BY id`, id)
	if err != nil {
		h.fail(w, err)
		return
	}
	for rows.Next() {
		var target string
		var scheduled, custom bool
		if err = rows.Scan(&target, &scheduled, &custom); err != nil {
			rows.Close()
			h.fail(w, err)
			return
		}
		if scheduled {
			settings.DefaultTargetIDs = append(settings.DefaultTargetIDs, target)
		}
		if settings.Custom && custom {
			settings.TargetIDs = append(settings.TargetIDs, target)
		}
	}
	err = rows.Err()
	rows.Close()
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.JSON(w, 200, settings)
}
