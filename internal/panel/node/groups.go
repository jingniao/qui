package node

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type GroupImport struct {
	SourceID string `json:"sourceId"`
	ProxyKey string `json:"proxyKey"`
}

type Group struct {
	InboundIDs          []string      `json:"inboundIds"`
	Imports             []GroupImport `json:"imports"`
	GrantedUserIDs      []string      `json:"grantedUserIds"`
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Purpose             string        `json:"purpose"`
	Enabled             bool          `json:"enabled"`
	SubscriptionVisible bool          `json:"subscriptionVisible"`
	NodeIDs             []string      `json:"nodeIds"`
	Remark              string        `json:"remark"`
	CreatedAt           string        `json:"createdAt"`
}
type GroupInput struct {
	InboundIDs          []string      `json:"inboundIds"`
	Imports             []GroupImport `json:"imports"`
	Name                string        `json:"name"`
	Purpose             string        `json:"purpose"`
	Enabled             bool          `json:"enabled"`
	SubscriptionVisible bool          `json:"subscriptionVisible"`
	NodeIDs             []string      `json:"nodeIds"`
	Remark              string        `json:"remark"`
}

func (r *Repository) Group(ctx context.Context, id string) (Group, error) {
	group := Group{NodeIDs: []string{}, InboundIDs: []string{}, Imports: []GroupImport{}, GrantedUserIDs: []string{}}
	err := r.Database.QueryRowContext(ctx, "SELECT id,name,purpose,enabled,subscription_visible,remark,created_at FROM node_groups WHERE id=?", id).Scan(&group.ID, &group.Name, &group.Purpose, &group.Enabled, &group.SubscriptionVisible, &group.Remark, &group.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return group, ErrNotFound
	}
	if err != nil {
		return group, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT node_id FROM node_group_members WHERE group_id=? ORDER BY node_id", id)
	if err != nil {
		return group, err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return group, err
		}
		group.NodeIDs = append(group.NodeIDs, nodeID)
	}
	if err := rows.Err(); err != nil {
		return group, err
	}
	rows.Close()
	for _, q := range []struct {
		sql    string
		target *[]string
	}{
		{"SELECT inbound_id FROM node_group_inbounds WHERE group_id=? ORDER BY inbound_id", &group.InboundIDs},
		{"SELECT user_id FROM user_node_group_grants WHERE group_id=? ORDER BY user_id", &group.GrantedUserIDs},
	} {
		rows, err := r.Database.QueryContext(ctx, q.sql, id)
		if err != nil {
			return group, err
		}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				rows.Close()
				return group, err
			}
			*q.target = append(*q.target, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return group, err
		}
	}
	rows, err = r.Database.QueryContext(ctx, "SELECT source_id,proxy_key FROM node_group_imports WHERE group_id=? ORDER BY source_id,proxy_key", id)
	if err != nil {
		return group, err
	}
	defer rows.Close()
	for rows.Next() {
		var v GroupImport
		if err = rows.Scan(&v.SourceID, &v.ProxyKey); err != nil {
			return group, err
		}
		group.Imports = append(group.Imports, v)
	}
	return group, rows.Err()
}
func (r *Repository) Groups(ctx context.Context, page, size int) ([]Group, int, error) {
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM node_groups").Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM node_groups ORDER BY created_at,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	groups := []Group{}
	for _, id := range ids {
		group, err := r.Group(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		groups = append(groups, group)
	}
	return groups, count, nil
}
func (r *Repository) SaveGroup(ctx context.Context, groupID string, input GroupInput) (Group, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 120 || len(input.Remark) > 1024 {
		return Group{}, errors.New("invalid node group name or remark")
	}
	if !strings.Contains("|subscription|relay|egress|mixed|", "|"+input.Purpose+"|") {
		return Group{}, errors.New("invalid node group purpose")
	}
	if len(input.NodeIDs) > 100 || len(input.InboundIDs) > 500 || len(input.Imports) > 200 {
		return Group{}, errors.New("node group member limit exceeded")
	}
	if input.Purpose == "relay" || input.Purpose == "egress" {
		input.SubscriptionVisible = false
	}
	create := groupID == ""
	if create {
		var err error
		groupID, err = RandomID()
		if err != nil {
			return Group{}, err
		}
	} else {
		if _, err := r.Group(ctx, groupID); err != nil {
			return Group{}, err
		}
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback()
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,remark,created_at) VALUES(?,?,?,?,?,?,?)", groupID, input.Name, input.Purpose, input.Enabled, input.SubscriptionVisible, input.Remark, timestamp(time.Now()))
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE node_groups SET name=?,purpose=?,enabled=?,subscription_visible=?,remark=? WHERE id=?", input.Name, input.Purpose, input.Enabled, input.SubscriptionVisible, input.Remark, groupID)
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return Group{}, errors.New("node group name already exists")
		}
		return Group{}, err
	}
	// Legacy server selection is expanded once, never used as an access grant.
	if input.InboundIDs == nil && input.NodeIDs != nil {
		input.InboundIDs = []string{}
		seen := map[string]bool{}
		for _, nodeID := range input.NodeIDs {
			if seen[nodeID] {
				return Group{}, errors.New("duplicate node group member")
			}
			seen[nodeID] = true
			var exists int
			if err := tx.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE id=? AND deleted_at IS NULL", nodeID).Scan(&exists); err != nil {
				return Group{}, errors.New("node group member does not exist")
			}
			rows, err := tx.QueryContext(ctx, "SELECT id FROM inbounds WHERE node_id=? AND subscription_visible=1 AND internal_relay=0 ORDER BY id", nodeID)
			if err != nil {
				return Group{}, err
			}
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return Group{}, err
				}
				input.InboundIDs = append(input.InboundIDs, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return Group{}, err
			}
		}
	}
	if len(input.InboundIDs) > 500 {
		return Group{}, errors.New("node group is limited to 500 inbounds")
	}
	for _, table := range []string{"node_group_inbounds", "node_group_imports", "node_group_members"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE group_id=?", groupID); err != nil {
			return Group{}, err
		}
	}
	seen := map[string]bool{}
	for _, inboundID := range input.InboundIDs {
		if seen[inboundID] {
			return Group{}, errors.New("duplicate inbound member")
		}
		seen[inboundID] = true
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM inbounds i JOIN nodes n ON n.id=i.node_id WHERE i.id=? AND n.deleted_at IS NULL", inboundID).Scan(&exists); err != nil {
			return Group{}, errors.New("inbound member does not exist")
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO node_group_inbounds VALUES(?,?)", groupID, inboundID); err != nil {
			return Group{}, err
		}
	}
	seen = map[string]bool{}
	for _, item := range input.Imports {
		key := item.SourceID + "\x00" + item.ProxyKey
		if seen[key] || len(item.ProxyKey) > 1024 {
			return Group{}, errors.New("invalid or duplicate import member")
		}
		seen[key] = true
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM subscription_sources WHERE id=?", item.SourceID).Scan(&exists); err != nil {
			return Group{}, errors.New("import source does not exist")
		}
		if item.ProxyKey != "" {
			if err := tx.QueryRowContext(ctx, "SELECT 1 FROM imported_proxies WHERE source_id=? AND name||'|'||protocol=? LIMIT 1", item.SourceID, item.ProxyKey).Scan(&exists); err != nil {
				return Group{}, errors.New("import proxy does not exist")
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO node_group_imports VALUES(?,?,?)", groupID, item.SourceID, item.ProxyKey); err != nil {
			return Group{}, err
		}
	}
	// Triggers also maintain this index on inbound moves/deletes in Proxy's transaction.
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO node_group_members SELECT DISTINCT e.group_id,i.node_id FROM node_group_inbounds e JOIN inbounds i ON i.id=e.inbound_id WHERE e.group_id=?", groupID); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(); err != nil {
		return Group{}, err
	}
	return r.Group(ctx, groupID)
}
func (r *Repository) DeleteGroup(ctx context.Context, id string) error {
	if _, err := r.Group(ctx, id); err != nil {
		return err
	}
	_, err := r.Database.ExecContext(ctx, "DELETE FROM node_groups WHERE id=?", id)
	return err
}
func (r *Repository) ExpandedNodes(ctx context.Context, groupIDs []string) ([]Node, error) {
	seen := map[string]bool{}
	nodes := []Node{}
	for _, groupID := range groupIDs {
		group, err := r.Group(ctx, groupID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !group.Enabled || !group.SubscriptionVisible || group.Purpose != "subscription" && group.Purpose != "mixed" {
			continue
		}
		for _, nodeID := range group.NodeIDs {
			if seen[nodeID] {
				continue
			}
			seen[nodeID] = true
			node, err := r.Get(ctx, nodeID)
			if err != nil {
				return nil, err
			}
			if !node.Enabled || node.Status == "disabled" || !node.AllowSubscription || node.Maintenance {
				continue
			}
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}
func (r *Repository) SubscriptionFlags(ctx context.Context, id string, allow, maintenance bool) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM nodes WHERE id=? AND deleted_at IS NULL", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE nodes SET allow_subscription=?,maintenance=? WHERE id=?", allow, maintenance, id); err != nil {
		return err
	}
	return tx.Commit()
}
