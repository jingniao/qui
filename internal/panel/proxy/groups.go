package proxy

import (
	"context"
	"database/sql"
	"errors"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/scheduler"
	"strings"
)

type Group struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	NodeID           string                     `json:"nodeId"`
	Strategy         string                     `json:"strategy"`
	Enabled          bool                       `json:"enabled"`
	Members          []desiredstate.GroupMember `json:"members"`
	ProbeURL         string                     `json:"probeUrl"`
	ProbeInterval    int                        `json:"probeInterval"`
	FailurePolicy    string                     `json:"failurePolicy"`
	BackupOutboundID string                     `json:"backupOutboundId"`
	CreatedAt        string                     `json:"createdAt"`
}
type GroupInput struct {
	Name             string                     `json:"name"`
	NodeID           string                     `json:"nodeId"`
	Strategy         string                     `json:"strategy"`
	Enabled          bool                       `json:"enabled"`
	Members          []desiredstate.GroupMember `json:"members"`
	ProbeURL         string                     `json:"probeUrl"`
	ProbeInterval    int                        `json:"probeInterval"`
	FailurePolicy    string                     `json:"failurePolicy"`
	BackupOutboundID string                     `json:"backupOutboundId"`
}

func (g Group) Desired() desiredstate.OutboundGroup {
	return desiredstate.OutboundGroup{ID: g.ID, Strategy: g.Strategy, Members: g.Members, ProbeURL: g.ProbeURL, ProbeInterval: g.ProbeInterval, Failover: g.FailurePolicy, BackupTargetID: g.BackupOutboundID}
}
func (r *Repository) Group(ctx context.Context, groupID string) (Group, error) {
	group := Group{Members: []desiredstate.GroupMember{}}
	err := r.Database.QueryRowContext(ctx, "SELECT id,name,node_id,strategy,enabled,probe_url,probe_interval,CASE WHEN failure_policy_v2='' THEN failure_policy ELSE failure_policy_v2 END,backup_outbound_id,created_at FROM outbound_groups WHERE id=?", groupID).Scan(&group.ID, &group.Name, &group.NodeID, &group.Strategy, &group.Enabled, &group.ProbeURL, &group.ProbeInterval, &group.FailurePolicy, &group.BackupOutboundID, &group.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return group, ErrNotFound
	}
	if err != nil {
		return group, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT outbound_id,priority,weight FROM outbound_group_members WHERE group_id=? ORDER BY priority,outbound_id", groupID)
	if err != nil {
		return group, err
	}
	defer rows.Close()
	for rows.Next() {
		var member desiredstate.GroupMember
		if err := rows.Scan(&member.OutboundID, &member.Priority, &member.Weight); err != nil {
			return group, err
		}
		group.Members = append(group.Members, member)
	}
	return group, rows.Err()
}
func (r *Repository) Groups(ctx context.Context, nodeID string, page, size int) ([]Group, int, error) {
	where := ""
	args := []any{}
	if nodeID != "" {
		where = " WHERE node_id=?"
		args = append(args, nodeID)
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM outbound_groups"+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM outbound_groups"+where+" ORDER BY created_at,id LIMIT ? OFFSET ?", args...)
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
func (s *Service) SaveGroup(ctx context.Context, groupID string, input GroupInput) (Group, error) {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 120 {
		return Group{}, errors.New("group name required")
	}
	if n, err := s.Nodes.Get(ctx, input.NodeID); err != nil || !n.Enabled {
		return Group{}, errors.New("group node unavailable")
	}
	if input.ProbeURL == "" {
		input.ProbeURL = "https://www.gstatic.com/generate_204"
	}
	if input.ProbeInterval == 0 {
		input.ProbeInterval = 30
	}
	if input.FailurePolicy == "" {
		input.FailurePolicy = "Block"
	}
	if input.FailurePolicy == "BackupChain" {
		input.FailurePolicy = "BackupProxyChain"
	}
	create := groupID == ""
	if create {
		var err error
		groupID, err = id.New()
		if err != nil {
			return Group{}, err
		}
	} else {
		existing, err := s.Repository.Group(ctx, groupID)
		if err != nil {
			return Group{}, err
		}
		if existing.NodeID != input.NodeID {
			return Group{}, errors.New("group node is immutable")
		}
	}
	available := map[string]bool{}
	udp := true
	for _, member := range input.Members {
		outbound, err := s.Repository.Outbound(ctx, member.OutboundID, false)
		if err != nil || outbound.NodeID != input.NodeID || !outbound.Enabled || !outbound.SupportTCP || outbound.Protocol == "block" {
			return Group{}, errors.New("group member must be an enabled TCP-capable outbound on the same node")
		}
		available[member.OutboundID] = true
		udp = udp && outbound.SupportUDP
	}
	if err := s.validateBackup(ctx, input.NodeID, input.FailurePolicy, input.BackupOutboundID, true, udp); err != nil {
		return Group{}, err
	}
	if desiredstate.BackupKind(input.FailurePolicy) != "" {
		available[input.BackupOutboundID] = true
	}
	group := Group{ID: groupID, Name: input.Name, NodeID: input.NodeID, Strategy: input.Strategy, Enabled: input.Enabled, Members: input.Members, ProbeURL: input.ProbeURL, ProbeInterval: input.ProbeInterval, FailurePolicy: input.FailurePolicy, BackupOutboundID: input.BackupOutboundID}
	if err := scheduler.ValidateGroup(group.Desired(), available); err != nil {
		return Group{}, err
	}
	if err := s.checkSchedulingChange(ctx, input.NodeID, groupID, group); err != nil {
		return Group{}, err
	}
	return s.Repository.saveGroup(ctx, groupID, input, create)
}

func (r *Repository) saveGroup(ctx context.Context, groupID string, input GroupInput, create bool) (Group, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback()
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO outbound_groups(id,name,node_id,strategy,enabled,probe_url,probe_interval,failure_policy,backup_outbound_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", groupID, input.Name, input.NodeID, input.Strategy, input.Enabled, input.ProbeURL, input.ProbeInterval, legacyFailure(input.FailurePolicy, false), input.BackupOutboundID, timestamp())
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE outbound_groups SET name=?,strategy=?,enabled=?,probe_url=?,probe_interval=?,failure_policy=?,backup_outbound_id=? WHERE id=?", input.Name, input.Strategy, input.Enabled, input.ProbeURL, input.ProbeInterval, legacyFailure(input.FailurePolicy, false), input.BackupOutboundID, groupID)
	}
	if err != nil {
		return Group{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE outbound_groups SET failure_policy_v2=? WHERE id=?", input.FailurePolicy, groupID); err != nil {
		return Group{}, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM outbound_group_members WHERE group_id=?", groupID); err != nil {
		return Group{}, err
	}
	for _, member := range input.Members {
		if _, err := tx.ExecContext(ctx, "INSERT INTO outbound_group_members(group_id,outbound_id,priority,weight) VALUES(?,?,?,?)", groupID, member.OutboundID, member.Priority, member.Weight); err != nil {
			return Group{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Group{}, err
	}
	return r.Group(ctx, groupID)
}
func (r *Repository) DeleteGroup(ctx context.Context, groupID string) error {
	if _, err := r.Group(ctx, groupID); err != nil {
		return err
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM inbounds WHERE default_outbound=? OR default_outbound=?) +
 (SELECT count(*) FROM outbounds WHERE backup_outbound_id=?) +
 (SELECT count(*) FROM outbound_groups WHERE backup_outbound_id=?) +
 (SELECT count(*) FROM proxy_chains WHERE backup_target_id=?)`, groupID, "group:"+groupID, groupID, groupID, groupID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("group is used as an inbound default")
	}
	_, err := r.Database.ExecContext(ctx, "DELETE FROM outbound_groups WHERE id=?", groupID)
	return err
}

func (s *Service) validateSavedGroup(ctx context.Context, group Group) error {
	available := map[string]bool{}
	udp := true
	for _, member := range group.Members {
		outbound, err := s.Repository.Outbound(ctx, member.OutboundID, false)
		if err != nil || !outbound.Enabled || outbound.NodeID != group.NodeID || !outbound.SupportTCP || outbound.Protocol == "block" {
			return errors.New("group member unavailable or capability changed; update the group before publication")
		}
		available[outbound.ID] = true
		udp = udp && outbound.SupportUDP
	}
	if err := s.validateBackup(ctx, group.NodeID, group.FailurePolicy, group.BackupOutboundID, true, udp); err != nil {
		return err
	}
	if desiredstate.BackupKind(group.FailurePolicy) != "" {
		available[group.BackupOutboundID] = true
	}
	return scheduler.ValidateGroup(group.Desired(), available)
}
