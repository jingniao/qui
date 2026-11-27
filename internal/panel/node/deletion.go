package node

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNodeDeleted refuses every edit of a logically deleted node.
var ErrNodeDeleted = errors.New("node is deleted")

// Blocker is one piece of configuration or in-flight work that still references
// the node. The Owner removes it before the node can be deleted.
type Blocker struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Code lets node operations reuse the same blocker payload while selecting the
// operation-specific HTTP error code.
type BlockedError struct {
	Items []Blocker
	Code  string
}

func (e *BlockedError) Error() string { return ErrNodeInUse.Error() }
func (e *BlockedError) Unwrap() error { return ErrNodeInUse }

// Tasks closes the node's own single-node coordination rows inside the deletion
// transaction. The deployment module owns those tables.
type Tasks interface {
	CloseNodeTasks(context.Context, *sql.Tx, string) error
}

// DeletionAudit records the deletion in the same transaction, so the operation
// log cannot disagree with the deleted node.
type DeletionAudit interface {
	RecordTx(context.Context, *sql.Tx, string, string, string, string, string) error
}

type DeletionItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DeletionInbound struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NodeName string `json:"nodeName"`
}

type DeletionImpact struct {
	Schemes  []DeletionItem    `json:"schemes"`
	Tunnels  []DeletionItem    `json:"tunnels"`
	Rules    []DeletionItem    `json:"rules"`
	Inbounds []DeletionInbound `json:"inbounds"`
	Blockers []Blocker         `json:"blockers"`
}

type CleanupPendingNode struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type DeletionResult struct {
	PendingNodes []CleanupPendingNode `json:"pendingNodes"`
}

// Apply checks the prepared ownership snapshot and removes relay resources in
// the node transaction. Removed keys are kind+":"+id for preview filtering.
type DeletionCascade struct {
	Preview     DeletionImpact
	Removed     map[string]bool
	Apply       func(context.Context, *sql.Tx) error
	AfterCommit func(context.Context) ([]CleanupPendingNode, error)
}

type RelayDeletion interface {
	PrepareNodeDeletion(context.Context, string) (DeletionCascade, error)
}

// DeletionPreview describes what a delete would do and what stops it.
type DeletionPreview struct {
	DeletionImpact
	Mode          string   `json:"mode"`
	Groups        []string `json:"groups"`
	LastHeartbeat string   `json:"lastHeartbeat"`
	Online        bool     `json:"online"`
}

func emptyDeletionImpact() DeletionImpact {
	return DeletionImpact{Schemes: []DeletionItem{}, Tunnels: []DeletionItem{}, Rules: []DeletionItem{}, Inbounds: []DeletionInbound{}, Blockers: []Blocker{}}
}

type rowQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// nodeBlockers lists the configuration that still references the node plus the
// multi-node batches that are in flight. Batches are never closed by a deletion,
// so they must be finished or rolled back first.
func nodeBlockers(ctx context.Context, q rowQuery, id string) ([]Blocker, error) {
	items := []Blocker{}
	for _, item := range []struct {
		Kind  string
		Query string
		Args  []any
	}{
		{"inbound", "SELECT id,name FROM inbounds WHERE node_id=? ORDER BY created_at,id", []any{id}},
		{"outbound", "SELECT id,name FROM outbounds WHERE node_id=? ORDER BY created_at,id", []any{id}},
		{"outbound_target", "SELECT id,name FROM outbounds WHERE target_node_id=? ORDER BY created_at,id", []any{id}},
		{"outbound_group", "SELECT id,name FROM outbound_groups WHERE node_id=? ORDER BY created_at,id", []any{id}},
		{"proxy_chain", "SELECT id,name FROM proxy_chains WHERE EXISTS(SELECT 1 FROM json_each(proxy_chains.node_ids_json) WHERE json_each.value=?) OR EXISTS(SELECT 1 FROM proxy_chain_hops h WHERE h.chain_id=proxy_chains.id AND (h.source_node_id=? OR h.target_node_id=?)) ORDER BY id", []any{id, id, id}},
		{"policy", "SELECT id,name FROM policies WHERE EXISTS(SELECT 1 FROM json_each(policies.scope_json,'$.node_ids') WHERE json_each.value=?) ORDER BY priority,id", []any{id}},
		{"server_egress_policy", `SELECT id,name FROM policies WHERE json_extract(action_json,'$.type')='ServerEgress'
			AND json_extract(action_json,'$.target_id')=? ORDER BY id`, []any{id}},
		{"server_egress_group", `SELECT id,name FROM server_egress_groups WHERE EXISTS
			(SELECT 1 FROM json_each(server_ids_json) WHERE value=?) ORDER BY id`, []any{id}},
		{"deployment_batch", "SELECT DISTINCT b.id,b.name FROM deployment_batches b JOIN deployment_batch_items i ON i.batch_id=b.id WHERE i.node_id=? AND b.status IN ('deploying','rolling_back','rollback_failed') ORDER BY b.id", []any{id}},
	} {
		rows, err := q.QueryContext(ctx, item.Query, item.Args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			blocker := Blocker{Kind: item.Kind}
			if err := rows.Scan(&blocker.ID, &blocker.Name); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, blocker)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

// Delete logically deletes an enrolled node: the certificate is revoked, the node
// leaves its groups and subscriptions and cannot be enabled again, while every
// history row keeps its reference. Deleting an already deleted node changes
// nothing.
func (r *Repository) Delete(ctx context.Context, id string, tasks Tasks, audit DeletionAudit, actor, ip string) error {
	_, err := r.DeleteWithResult(ctx, id, tasks, audit, actor, ip)
	return err
}

func (r *Repository) DeleteWithResult(ctx context.Context, id string, tasks Tasks, audit DeletionAudit, actor, ip string) (DeletionResult, error) {
	result := DeletionResult{PendingNodes: []CleanupPendingNode{}}
	n, err := r.Get(ctx, id)
	if err != nil {
		return result, err
	}
	if n.DeletedAt != "" {
		return result, nil
	}
	var cascade DeletionCascade
	if r.RelayDeletion != nil {
		cascade, err = r.RelayDeletion.PrepareNodeDeletion(ctx, id)
		if err != nil {
			return result, err
		}
	}

	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var name, deleted string
	err = tx.QueryRowContext(ctx, "SELECT name,coalesce(deleted_at,'') FROM nodes WHERE id=?", id).Scan(&name, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if deleted != "" {
		return result, tx.Commit()
	}
	// Refuse external consumers before deleting their inbound providers, so FK
	// restrictions remain a structured blocker response rather than a SQL error.
	before, err := nodeBlockers(ctx, tx, id)
	if err != nil {
		return result, err
	}
	remaining := []Blocker{}
	for _, blocker := range before {
		if !cascade.Removed[blocker.Kind+":"+blocker.ID] {
			remaining = append(remaining, blocker)
		}
	}
	remaining = mergeDeletionBlockers(remaining, cascade.Preview.Blockers)
	if len(remaining) != 0 {
		return result, &BlockedError{Items: remaining}
	}
	if cascade.Apply != nil {
		if err := cascade.Apply(ctx, tx); err != nil {
			return result, err
		}
	}
	blockers, err := nodeBlockers(ctx, tx, id)
	if err != nil {
		return result, err
	}
	blockers = mergeDeletionBlockers(blockers, cascade.Preview.Blockers)
	if len(blockers) != 0 {
		return result, &BlockedError{Items: blockers}
	}
	now := timestamp(time.Now())
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET deleted_at=?,enabled=0,allow_subscription=0,status='disabled' WHERE id=?", now, id); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE node_identities SET revoked_at=coalesce(revoked_at,?) WHERE node_id=?", now, id); err != nil {
		return result, err
	}
	// An enrollment code of a deleted node never works again, and a deleted node
	// is not a member of any node group.
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_group_inbounds WHERE inbound_id IN (SELECT id FROM inbounds WHERE node_id=?)", id); err != nil {
		return result, err
	}
	for _, table := range []string{"node_enrollment_tokens", "node_certificate_renewals", "node_group_servers", "node_group_members", "relay_cleanup_nodes"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE node_id=?", id); err != nil {
			return result, err
		}
	}
	if tasks != nil {
		if err := tasks.CloseNodeTasks(ctx, tx, id); err != nil {
			return result, err
		}
	}
	if audit != nil {
		if err := audit.RecordTx(ctx, tx, actor, "删除节点", name+" ("+id+")", "成功", ip); err != nil {
			return result, err
		}
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	r.invalidateIdentity(id)
	if cascade.AfterCommit != nil {
		pending, err := cascade.AfterCommit(ctx)
		if err != nil {
			return result, err
		}
		result.PendingNodes = append(result.PendingNodes, pending...)
	}
	return result, nil
}

// Preview reports how a delete would proceed and what currently stops it. It
// carries no credentials and no enrollment token.
func (r *Repository) Preview(ctx context.Context, id string) (DeletionPreview, error) {
	n, err := r.Get(ctx, id)
	if err != nil {
		return DeletionPreview{}, err
	}
	preview := DeletionPreview{DeletionImpact: emptyDeletionImpact(), Mode: "delete", Groups: []string{}, LastHeartbeat: n.LastHeartbeat, Online: ConnectionAlive(n.Status, n.LastHeartbeat, time.Now(), n.Liveness)}
	if n.DeletedAt != "" {
		preview.Mode = "deleted"
		return preview, nil
	}
	var identities int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_identities WHERE node_id=?", id).Scan(&identities); err != nil {
		return DeletionPreview{}, err
	}
	if n.Status == "pending" && identities == 0 {
		preview.Mode = "cancel"
	}
	var cascade DeletionCascade
	if r.RelayDeletion != nil {
		cascade, err = r.RelayDeletion.PrepareNodeDeletion(ctx, id)
		if err != nil {
			return DeletionPreview{}, err
		}
		preview.DeletionImpact = cascade.Preview
	}
	blockers, err := nodeBlockers(ctx, r.Database, id)
	if err != nil {
		return DeletionPreview{}, err
	}
	for _, blocker := range blockers {
		if cascade.Removed[blocker.Kind+":"+blocker.ID] {
			continue
		}
		preview.Blockers = mergeDeletionBlockers(preview.Blockers, []Blocker{blocker})
	}
	// Once a pending server owns configuration, use the same atomic deletion
	// as an enrolled server; bare enrollment cancellation cannot cascade it.
	if preview.Mode == "cancel" && (len(preview.Inbounds)+len(preview.Schemes)+len(preview.Tunnels)+len(preview.Rules)+len(preview.Blockers) > 0) {
		preview.Mode = "delete"
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT g.name FROM node_groups g JOIN node_group_members m ON m.group_id=g.id WHERE m.node_id=? ORDER BY g.name", id)
	if err != nil {
		return DeletionPreview{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return DeletionPreview{}, err
		}
		preview.Groups = append(preview.Groups, name)
	}
	return preview, rows.Err()
}

func mergeDeletionBlockers(items, additional []Blocker) []Blocker {
	for _, item := range additional {
		found := false
		for _, current := range items {
			if current.Kind == item.Kind && current.ID == item.ID {
				found = true
				break
			}
		}
		if !found {
			items = append(items, item)
		}
	}
	return items
}
