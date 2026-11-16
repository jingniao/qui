package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/importtarget"
	"proxycontrol/pkg/contracts/desiredstate"
)

// canvasImportExit is an imported node a server scheme ends at (v1.6.0 plan 1):
// the previous hop dials it through an outbound the publication generates from
// the import's saved endpoint, owned by the connection's canvas tunnel like
// every other generated object.
type canvasImportExit struct {
	tunnelID   string
	sourceNode string
	importID   string
	importName string
	name       string
	input      OutboundInput
	// outboundID is the outbound a previous publication generated; canvasEnsure
	// fills it for a new one.
	outboundID string
	changed    bool
}

// canvasImportOutboundName keys the generated outbound by the import's ID, so
// renaming the import keeps the same object.
func canvasImportOutboundName(prefix, importID string) string {
	return prefix + "导入出口 " + importID + canvasOutboundSuffix
}

// final is how chains and entry routes name this exit before publication: the
// outbound's ID, or its display name while it does not exist yet (the same
// convention as pending edge outbounds).
func (x *canvasImportExit) final() string {
	if x.outboundID != "" {
		return x.outboundID
	}
	return x.name
}

// canvasImportExitFor converts the target import and finds the outbound a
// previous publication generated for it. A conversion that would lose a setting
// is refused with the fixed reason the canvas shows.
func (s *Service) canvasImportExitFor(ctx context.Context, connection CanvasConnection, sourceNode, importID, prefix string) (*canvasImportExit, error) {
	endpoint, err := importtarget.Load(ctx, s.Repository.Database, s.Repository.Vault, importID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("导入节点已不存在，请从方案中移除它")
	}
	if err != nil {
		return nil, err
	}
	input, code := ImportOutbound(endpoint)
	if code != "" {
		return nil, fmt.Errorf("导入节点「%s」不能作为服务器出口：%s", endpoint.Name, ImportOutboundReason(code))
	}
	input.SupportTCP = input.SupportTCP && connection.SupportTCP
	input.SupportUDP = input.SupportUDP && connection.SupportUDP
	if !input.SupportTCP && !input.SupportUDP {
		return nil, fmt.Errorf("导入节点「%s」不支持连接所需的 TCP/UDP", endpoint.Name)
	}
	exit := &canvasImportExit{tunnelID: connection.TunnelID, sourceNode: sourceNode, importID: importID, importName: endpoint.Name, name: canvasImportOutboundName(prefix, importID)}
	input.Name, input.NodeID = exit.name, sourceNode
	n, err := s.Nodes.Get(ctx, sourceNode)
	if err != nil {
		return nil, err
	}
	if !n.Enabled || n.DeletedAt != "" {
		return nil, errors.New("导入出口的上一跳服务器不存在或已停用")
	}
	if err := ValidateOutboundCores(n.Capabilities, input.Protocol); err != nil {
		return nil, err
	}
	if err := node.RequireOutboundClientSupport(desiredstate.State{NodeID: sourceNode, Outbounds: []desiredstate.Outbound{{ID: exit.name, Protocol: input.Protocol, Settings: input.Settings}}}, n.Capabilities, n.Name); err != nil {
		return nil, err
	}
	exit.input, exit.changed = input, true
	existing, err := s.Repository.canvasImportOutbound(ctx, connection.TunnelID, sourceNode)
	switch {
	case err == nil:
		exit.outboundID = existing.ID
		exit.name, exit.input.Name = existing.Name, existing.Name
		if existing.Protocol != input.Protocol {
			return nil, errors.New("导入节点协议已改变，请撤除旧连接后重新创建")
		}
		exit.changed, err = s.canvasImportOutboundChanged(ctx, existing, exit.input)
		if err != nil {
			return nil, err
		}
	case !errors.Is(err, ErrNotFound):
		return nil, err
	}
	return exit, nil
}

// Resolve the connection's owned slot by ID, never by an editable display name.
func (r *Repository) canvasImportOutbound(ctx context.Context, tunnelID, nodeID string) (Outbound, error) {
	var id string
	err := r.Database.QueryRowContext(ctx, "SELECT b.id FROM outbounds b JOIN canvas_tunnel_objects o ON o.object_kind='outbound' AND o.object_id=b.id WHERE o.tunnel_id=? AND b.node_id=? AND COALESCE(b.target_inbound_id,'')=''", tunnelID, nodeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Outbound{}, ErrNotFound
	}
	if err != nil {
		return Outbound{}, err
	}
	return r.Outbound(ctx, id, true)
}

func (s *Service) canvasImportOutboundChanged(ctx context.Context, old Outbound, input OutboundInput) (bool, error) {
	if old.Address != input.Address || old.Port != input.Port || old.Protocol != input.Protocol || old.Enabled != input.Enabled || old.SupportTCP != input.SupportTCP || old.SupportUDP != input.SupportUDP || old.FailurePolicy != input.FailurePolicy || !reflect.DeepEqual(old.Settings, input.Settings) {
		return true, nil
	}
	if old.CredentialID == "" {
		return input.Secret != "" || input.Username != "", nil
	}
	credential, err := s.Repository.Credential(ctx, old.CredentialID)
	if err != nil {
		return false, err
	}
	return credential.Secret != input.Secret || credential.Username != input.Username, nil
}

// Compare cached import content with the owned outbound without publishing or
// requiring a live Agent. Names are presentation only; credentials stay private.
func (s *Service) canvasImportDraftReasons(ctx context.Context, scheme CanvasScheme) ([]string, error) {
	if scheme.Execution != canvasExecutionServer || scheme.PublishedAt == "" {
		return nil, nil
	}
	layout, err := s.Repository.resolveCanvasLayout(ctx, scheme.Execution, scheme.Layout)
	if err != nil {
		return nil, err
	}
	if err := s.Repository.attachInlineTunnelIDs(ctx, scheme.ID, &layout); err != nil {
		return nil, err
	}
	instances := map[string]CanvasInstance{}
	for _, instance := range layout.Instances {
		instances[instance.InstanceID] = instance
	}
	for _, c := range layout.Connections {
		ref, _ := parseCanvasRef(instances[c.Target].Ref)
		if ref.kind != "import" {
			continue
		}
		endpoint, err := importtarget.Load(ctx, s.Repository.Database, s.Repository.Vault, ref.id)
		if errors.Is(err, sql.ErrNoRows) {
			return []string{"导入节点已不存在，请从方案中移除它"}, nil
		}
		if err != nil {
			return nil, err
		}
		input, code := ImportOutbound(endpoint)
		if code != "" {
			return []string{"导入节点已更新：" + ImportOutboundReason(code)}, nil
		}
		nodeID, err := s.canvasManagedNode(ctx, instances[c.Source])
		if err != nil {
			return nil, err
		}
		old, err := s.Repository.canvasImportOutbound(ctx, c.TunnelID, nodeID)
		if errors.Is(err, ErrNotFound) {
			return []string{"导入节点出口尚未生成，重新发布后生效"}, nil
		}
		if err != nil {
			return nil, err
		}
		input.SupportTCP = input.SupportTCP && c.SupportTCP
		input.SupportUDP = input.SupportUDP && c.SupportUDP
		changed, err := s.canvasImportOutboundChanged(ctx, old, input)
		if err != nil {
			return nil, err
		}
		if changed {
			return []string{"导入节点已更新，重新发布后生效"}, nil
		}
	}
	return nil, nil
}

// Only the public editor is guarded; canvas publication and compensation use
// the internal service path to update their own objects.
func (r *Repository) refuseManagedImportOutbound(ctx context.Context, outboundID string) error {
	var count int
	err := r.Database.QueryRowContext(ctx, `SELECT count(*) FROM canvas_tunnel_objects o JOIN canvas_tunnels t ON t.id=o.tunnel_id WHERE o.object_kind='outbound' AND o.object_id=? AND t.target_ref LIKE 'import:%'`, outboundID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return tunnelFail("managed_by_tunnel", 409, "该出站由中转方案管理，请在方案中修改或撤除")
	}
	return nil
}

// canvasEnsureImportExits creates or updates each generated outbound, then
// points every chain and entry that named a pending one at its ID. It runs
// before chains and entry routes are written.
func (s *Service) canvasEnsureImportExits(ctx context.Context, plan *canvasPlan, undo *canvasUndo) error {
	for _, exit := range plan.importExits {
		pending := exit.final()
		existing, err := s.Repository.canvasImportOutbound(ctx, exit.tunnelID, exit.sourceNode)
		switch {
		case errors.Is(err, ErrNotFound):
			saved, err := s.SaveOutbound(ctx, "", exit.input)
			if err != nil {
				return err
			}
			undo.add(func(ctx context.Context) error { return s.Repository.DeleteOutbound(ctx, saved.ID) })
			if err := s.ownCanvasObject(ctx, plan, "outbound", saved.ID, exit.name, undo); err != nil {
				return err
			}
			exit.outboundID = saved.ID
		case err != nil:
			return err
		default:
			if !exit.changed {
				continue
			}
			previous := existing
			if _, err := s.saveOutbound(ctx, existing.ID, exit.input, true); err != nil {
				return err
			}
			undo.add(func(ctx context.Context) error {
				if _, err := s.saveOutbound(ctx, previous.ID, outboundInputOf(previous), true); err != nil {
					return err
				}
				_, err := s.Repository.Database.ExecContext(ctx, "UPDATE outbounds SET credential_id=? WHERE id=?", previous.CredentialID, previous.ID)
				return err
			})
			if exit.input.Secret == "" && existing.CredentialID != "" {
				if _, err := s.Repository.Database.ExecContext(ctx, "UPDATE outbounds SET credential_id='' WHERE id=?", existing.ID); err != nil {
					return err
				}
			}
			exit.outboundID = existing.ID
		}
		for i := range plan.chains {
			if plan.chains[i].finalOutbound == pending {
				plan.chains[i].finalOutbound = exit.outboundID
			}
		}
		for i := range plan.entries {
			if plan.entries[i].finalOutbound == pending {
				plan.entries[i].finalOutbound = exit.outboundID
			}
		}
	}
	return nil
}

// canvasImportEntryGroups are the enabled subscription groups that contain the
// import, the counterpart of an exit inbound's groups for 加入出口所在分组.
func (s *Service) canvasImportEntryGroups(ctx context.Context, importID string) ([]CanvasPlanEntryGroup, error) {
	rows, err := s.Repository.Database.QueryContext(ctx, `SELECT DISTINCT g.id,g.name FROM node_groups g
		JOIN node_group_imports e ON e.group_id=g.id JOIN imported_proxies p ON p.source_id=e.source_id
		WHERE p.id=? AND (e.proxy_key='' OR e.proxy_key='id:'||p.id OR e.proxy_key=p.name||'|'||p.protocol)
		AND g.purpose IN ('subscription','mixed') ORDER BY g.id`, importID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := []CanvasPlanEntryGroup{}
	for rows.Next() {
		var group CanvasPlanEntryGroup
		if err := rows.Scan(&group.ID, &group.Name); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

// canvasConnectionFrom reports whether c starts at the server instance of nodeID.
func canvasConnectionFrom(layout CanvasLayout, c CanvasConnection, nodeID string) bool {
	for _, instance := range layout.Instances {
		if instance.InstanceID == c.Source {
			ref, _ := parseCanvasRef(instance.Ref)
			return ref.kind == "server" && ref.id == nodeID
		}
	}
	return false
}

// CanvasImportExit says whether an imported node can end a server scheme: Reason
// is empty when it converts, otherwise the fixed Chinese text.
type CanvasImportExit struct {
	ID      string   `json:"id"`
	Reason  string   `json:"reason"`
	NodeIDs []string `json:"nodeIds"`
}

// CanvasImportExits converts every cached imported node once.
func (s *Service) CanvasImportExits(ctx context.Context) ([]CanvasImportExit, error) {
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT id FROM imported_proxies ORDER BY id")
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	nodeRows, err := s.Repository.Database.QueryContext(ctx, "SELECT id FROM nodes WHERE enabled=1 AND deleted_at IS NULL ORDER BY id")
	if err != nil {
		return nil, err
	}
	var nodeIDs []string
	for nodeRows.Next() {
		var id string
		if err := nodeRows.Scan(&id); err != nil {
			nodeRows.Close()
			return nil, err
		}
		nodeIDs = append(nodeIDs, id)
	}
	err = nodeRows.Err()
	nodeRows.Close()
	if err != nil {
		return nil, err
	}
	nodes := make([]node.Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		n, err := s.Nodes.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	items := make([]CanvasImportExit, 0, len(ids))
	for _, id := range ids {
		endpoint, err := importtarget.Load(ctx, s.Repository.Database, s.Repository.Vault, id)
		if err != nil {
			return nil, err
		}
		input, code := ImportOutbound(endpoint)
		item := CanvasImportExit{ID: id, Reason: ImportOutboundReason(code), NodeIDs: []string{}}
		if code == "" {
			for _, n := range nodes {
				if ValidateOutboundCores(n.Capabilities, input.Protocol) != nil {
					continue
				}
				if node.RequireOutboundClientSupport(desiredstate.State{NodeID: n.ID, Outbounds: []desiredstate.Outbound{{ID: id, Protocol: input.Protocol, Settings: input.Settings}}}, n.Capabilities, n.Name) != nil {
					continue
				}
				item.NodeIDs = append(item.NodeIDs, n.ID)
			}
			if len(item.NodeIDs) == 0 {
				item.Reason = "没有支持该导入出口的服务器，请检查核心并升级 Agent"
			}
		}
		items = append(items, item)
	}
	return items, nil
}
