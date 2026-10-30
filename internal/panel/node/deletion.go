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

// BlockedError carries the blockers to the HTTP layer, which answers with them.
type BlockedError struct{ Items []Blocker }

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

// DeletionPreview describes what a delete would do and what stops it.
type DeletionPreview struct {
	Mode          string    `json:"mode"`
	Blockers      []Blocker `json:"blockers"`
	Groups        []string  `json:"groups"`
	LastHeartbeat string    `json:"lastHeartbeat"`
	Online        bool      `json:"online"`
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
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name, deleted string
	err = tx.QueryRowContext(ctx, "SELECT name,coalesce(deleted_at,'') FROM nodes WHERE id=?", id).Scan(&name, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if deleted != "" {
		return tx.Commit()
	}
	blockers, err := nodeBlockers(ctx, tx, id)
	if err != nil {
		return err
	}
	if len(blockers) != 0 {
		return &BlockedError{Items: blockers}
	}
	now := timestamp(time.Now())
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET deleted_at=?,enabled=0,allow_subscription=0,status='disabled' WHERE id=?", now, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE node_identities SET revoked_at=coalesce(revoked_at,?) WHERE node_id=?", now, id); err != nil {
		return err
	}
	// An enrollment code of a deleted node never works again, and a deleted node
	// is not a member of any node group.
	if _, err := tx.ExecContext(ctx, "DELETE FROM node_group_inbounds WHERE inbound_id IN (SELECT id FROM inbounds WHERE node_id=?)", id); err != nil {
		return err
	}
	for _, table := range []string{"node_enrollment_tokens", "node_certificate_renewals", "node_group_servers", "node_group_members"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE node_id=?", id); err != nil {
			return err
		}
	}
	if tasks != nil {
		if err := tasks.CloseNodeTasks(ctx, tx, id); err != nil {
			return err
		}
	}
	if audit != nil {
		if err := audit.RecordTx(ctx, tx, actor, "删除节点", name+" ("+id+")", "成功", ip); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Preview reports how a delete would proceed and what currently stops it. It
// carries no credentials and no enrollment token.
func (r *Repository) Preview(ctx context.Context, id string) (DeletionPreview, error) {
	n, err := r.Get(ctx, id)
	if err != nil {
		return DeletionPreview{}, err
	}
	preview := DeletionPreview{Mode: "delete", Blockers: []Blocker{}, Groups: []string{}, LastHeartbeat: n.LastHeartbeat, Online: n.Status == "online" || n.Status == "degraded"}
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
	if preview.Blockers, err = nodeBlockers(ctx, r.Database, id); err != nil {
		return DeletionPreview{}, err
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
