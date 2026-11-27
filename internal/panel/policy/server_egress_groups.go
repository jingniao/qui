package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
)

const serverEgress = "ServerEgress"
const serverEgressGroup = "ServerEgressGroup"

type ServerEgressGroupInput struct {
	Name        string   `json:"name"`
	ServerIDs   []string `json:"serverIds"`
	NamePattern string   `json:"namePattern"`
}

type ServerEgressGroup struct {
	ID string `json:"id"`
	ServerEgressGroupInput
}

type ServerEgressMember struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (r *Repository) ServerEgressGroups(ctx context.Context) ([]ServerEgressGroup, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT id,name,name_pattern,server_ids_json FROM server_egress_groups ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []ServerEgressGroup{}
	for rows.Next() {
		var group ServerEgressGroup
		var ids string
		if err := rows.Scan(&group.ID, &group.Name, &group.NamePattern, &ids); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ids), &group.ServerIDs); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

// ServerEgressMembers is used for preview and for validation before saving.
// Manual members keep their selection order. Matching names follow in order.
func (r *Repository) ServerEgressMembers(ctx context.Context, input ServerEgressGroupInput) ([]ServerEgressMember, error) {
	return r.serverEgressMembers(ctx, input, true)
}

func (r *Repository) serverEgressMembers(ctx context.Context, input ServerEgressGroupInput, requireManual bool) ([]ServerEgressMember, error) {
	if len(input.NamePattern) > 512 || len(input.ServerIDs) > 100 {
		return nil, errors.New("最多选择 100 台服务器，名称正则最多 512 字节")
	}
	var pattern *regexp.Regexp
	if input.NamePattern != "" {
		var err error
		pattern, err = regexp.Compile(input.NamePattern)
		if err != nil {
			return nil, errors.New("名称正则无效：" + err.Error())
		}
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,name FROM nodes WHERE enabled=1 AND status NOT IN ('pending','disabled') AND deleted_at IS NULL ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	choices := map[string]ServerEgressMember{}
	var matched []ServerEgressMember
	for rows.Next() {
		var member ServerEgressMember
		if err := rows.Scan(&member.ID, &member.Name); err != nil {
			return nil, err
		}
		choices[member.ID] = member
		if pattern != nil && pattern.MatchString(member.Name) {
			matched = append(matched, member)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	members := []ServerEgressMember{}
	for _, serverID := range input.ServerIDs {
		member, ok := choices[serverID]
		if !ok {
			if requireManual {
				return nil, errors.New("手选服务器不存在、未接入或已停用：" + serverID)
			}
			continue
		}
		if !seen[serverID] {
			seen[serverID] = true
			members = append(members, member)
		}
	}
	for _, member := range matched {
		if !seen[member.ID] {
			members = append(members, member)
		}
	}
	return members, nil
}

func (r *Repository) SaveServerEgressGroup(ctx context.Context, groupID string, input ServerEgressGroupInput) (ServerEgressGroup, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.NamePattern = strings.TrimSpace(input.NamePattern)
	if input.Name == "" || len(input.Name) > 128 {
		return ServerEgressGroup{}, errors.New("请填写组名，最多 128 字节")
	}
	members, err := r.ServerEgressMembers(ctx, input)
	if err != nil {
		return ServerEgressGroup{}, err
	}
	if len(members) == 0 {
		return ServerEgressGroup{}, errors.New("至少选择或匹配一台已接入的服务器")
	}
	create := groupID == ""
	if create {
		groupID, err = id.New()
		if err != nil {
			return ServerEgressGroup{}, err
		}
	}
	input.ServerIDs = append([]string{}, input.ServerIDs...)
	ids, err := json.Marshal(input.ServerIDs)
	if err != nil {
		return ServerEgressGroup{}, err
	}
	var result sql.Result
	if create {
		result, err = r.Database.ExecContext(ctx, "INSERT INTO server_egress_groups(id,name,name_pattern,server_ids_json) VALUES(?,?,?,?)", groupID, input.Name, input.NamePattern, string(ids))
	} else {
		result, err = r.Database.ExecContext(ctx, "UPDATE server_egress_groups SET name=?,name_pattern=?,server_ids_json=? WHERE id=?", input.Name, input.NamePattern, string(ids), groupID)
	}
	if err != nil {
		return ServerEgressGroup{}, err
	}
	if count, err := result.RowsAffected(); err != nil {
		return ServerEgressGroup{}, err
	} else if count == 0 {
		return ServerEgressGroup{}, ErrNotFound
	}
	return ServerEgressGroup{ID: groupID, ServerEgressGroupInput: input}, nil
}

func (r *Repository) deleteServerEgressGroup(ctx context.Context, groupID string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name string
	err = tx.QueryRowContext(ctx, `SELECT name FROM policies WHERE json_extract(action_json,'$.type')=?
		AND json_extract(action_json,'$.target_id')=? ORDER BY name LIMIT 1`, serverEgressGroup, groupID).Scan(&name)
	if err == nil {
		return errors.New("出站服务器组仍被节点策略引用：" + name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM server_egress_groups WHERE id=?", groupID)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil {
		return err
	} else if count == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func isServerEgress(action string) bool {
	return action == serverEgress || action == serverEgressGroup
}

func validateNodePolicy(rule *desiredstate.Policy, validate func(*desiredstate.Policy) error) error {
	kind := rule.Action.Type
	if isServerEgress(kind) {
		if rule.Action.Failover != "" && rule.Action.Failover != "Block" || rule.Action.BackupID != "" {
			return errors.New("指定服务器出站只支持阻断失败流量")
		}
		rule.Action.Type = "Outbound"
	}
	err := validate(rule)
	rule.Action.Type = kind
	return err
}

func (s *Service) ServerEgressTargets(ctx context.Context, kind, targetID string) ([]node.Node, error) {
	ids := []string{targetID}
	if kind == serverEgressGroup {
		var input ServerEgressGroupInput
		var raw string
		if err := s.Repository.Database.QueryRowContext(ctx, "SELECT name_pattern,server_ids_json FROM server_egress_groups WHERE id=?", targetID).Scan(&input.NamePattern, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &input.ServerIDs); err != nil {
			return nil, err
		}
		members, err := s.Repository.serverEgressMembers(ctx, input, false)
		if err != nil {
			return nil, err
		}
		ids = make([]string, 0, len(members))
		for _, member := range members {
			ids = append(ids, member.ID)
		}
	}
	targets := make([]node.Node, 0, len(ids))
	for _, id := range ids {
		target, err := s.Nodes.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if target.Enabled && target.Status != "pending" && target.Status != "disabled" && target.DeletedAt == "" && !node.CoreOptional(target.Capabilities) {
			targets = append(targets, target)
		} else if kind == serverEgress {
			return nil, errors.New("出站服务器不可用：" + target.Name)
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("出站服务器组没有可发布成员")
	}
	return targets, nil
}
