package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

type CanvasPlanEntryGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CanvasPlanEntry contains only public planning data, never credentials.
type CanvasPlanEntry struct {
	InstanceID    string                 `json:"instanceId"`
	Mode          string                 `json:"mode"`
	NodeID        string                 `json:"nodeId"`
	NodeName      string                 `json:"nodeName"`
	InboundID     string                 `json:"inboundId,omitempty"`
	Protocol      string                 `json:"protocol"`
	Port          int                    `json:"port"`
	Name          string                 `json:"name"`
	Route         string                 `json:"route"`
	Groups        []CanvasPlanEntryGroup `json:"groups"`
	PreviousRoute string                 `json:"previousRoute,omitempty"`
}

func (s *Service) canvasPlanEntries(ctx context.Context, layout CanvasLayout, plan *canvasPlan) error {
	for _, instance := range layout.Instances {
		ref, _ := parseCanvasRef(instance.Ref)
		if instance.Role != "entry" || (instance.EntryBinding == nil && ref.kind != "inbound") {
			continue
		}
		entry := canvasEntry{schemeID: plan.schemeID, binding: instance.EntryBinding}
		if entry.binding == nil {
			// Publication needs the original settings to round-trip legacy entries.
			inbound, err := s.Repository.Inbound(ctx, ref.id, true)
			if err != nil {
				return err
			}
			entry.existing = inbound
			plan.node(inbound.NodeID)
		} else {
			b := entry.binding
			n, err := s.Nodes.Get(ctx, ref.id)
			if err != nil {
				return err
			}
			if !n.Enabled || n.DeletedAt != "" {
				return errors.New("入口服务器不存在、已删除或已停用")
			}
			entry.preview = &CanvasPlanEntry{InstanceID: instance.InstanceID, Mode: b.Mode, NodeID: n.ID, NodeName: n.Name, Groups: []CanvasPlanEntryGroup{}}
			if b.Mode == "reuse" {
				inbound, err := s.Repository.Inbound(ctx, b.InboundID, true)
				if errors.Is(err, ErrNotFound) {
					return errors.New("复用入站不存在或已删除")
				}
				if err != nil {
					return err
				}
				if inbound.NodeID != n.ID || inbound.InternalRelay || inbound.Protocol == "forward" {
					return errors.New("复用入口必须是入口服务器上的普通用户入站，不能使用内部中转入站")
				}
				if err := validateCanvasEntryCore(n, inbound.Core, inbound.Protocol); err != nil {
					return err
				}
				entry.existing = inbound
				entry.preview.InboundID, entry.preview.Protocol, entry.preview.Port = inbound.ID, inbound.Protocol, inbound.Port
				entry.preview.Name, entry.preview.PreviousRoute = inbound.Name, inbound.DefaultOutbound
				entry.preview.Groups, err = s.canvasEntryGroups(ctx, inbound.ID, false)
				if err != nil {
					return err
				}
			} else {
				if err := validateCanvasEntryCore(n, b.Core, b.Protocol); err != nil {
					return err
				}
				entry.preview.Protocol = b.Protocol
				if b.PortMode == "manual" {
					entry.preview.Port = *b.Port
				}
				if b.GroupMode == "explicit" {
					seen := map[string]bool{}
					for _, groupID := range b.GroupIDs {
						if seen[groupID] {
							continue
						}
						var group CanvasPlanEntryGroup
						err := s.Repository.Database.QueryRowContext(ctx, "SELECT id,name FROM node_groups WHERE id=?", groupID).Scan(&group.ID, &group.Name)
						if errors.Is(err, sql.ErrNoRows) {
							return fmt.Errorf("入口分组 %s 不存在", groupID)
						}
						if err != nil {
							return err
						}
						entry.preview.Groups = append(entry.preview.Groups, group)
						seen[groupID] = true
					}
				}
			}
			record, err := s.Repository.CanvasEntryBinding(ctx, plan.schemeID, instance.InstanceID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			if err == nil {
				if record.Mode != b.Mode || b.Mode == "reuse" && record.InboundID != b.InboundID {
					return errors.New("入口绑定已发布，不能切换绑定模式或入站")
				}
				inbound, err := s.Repository.Inbound(ctx, record.InboundID, true)
				if err != nil {
					return err
				}
				if inbound.NodeID != n.ID {
					return errors.New("入口绑定已发布，不能切换服务器")
				}
				if b.Mode == "create" && (b.Protocol != inbound.Protocol || b.Core != inbound.Core || b.PortMode == "manual" && *b.Port != inbound.Port) {
					return errors.New("入口绑定已发布，不能在方案中修改协议、核心或监听端口")
				}
				entry.existing = inbound
				entry.preview.InboundID, entry.preview.Port = inbound.ID, inbound.Port
				entry.preview.Protocol = inbound.Protocol
				entry.preview.Name, entry.preview.PreviousRoute = inbound.Name, inbound.DefaultOutbound
				entry.preview.Groups, err = s.canvasEntryGroups(ctx, inbound.ID, false)
				if err != nil {
					return err
				}
			}
			plan.node(n.ID)
		}
		plan.entries = append(plan.entries, entry)
	}
	return nil
}

func validateCanvasEntryCore(n node.Node, core, protocol string) error {
	if !desiredstate.SupportsInbound(protocol) || protocol == "forward" {
		return fmt.Errorf("用户入口不支持协议 %s", protocol)
	}
	if node.SelectedCore(n.Capabilities, core) == nil {
		return errors.New("入口服务器未上报所选核心已启用，无法确认入站协议支持")
	}
	if node.CoreSelectionIsBlocked(n, core) {
		return errors.New("入口核心正在变更，请等待完成并刷新能力后再试")
	}
	if err := ValidateInboundCore(n.Capabilities, core, protocol); err != nil {
		return fmt.Errorf("入口服务器 %s 的核心不支持 %s 入站：%w", n.Name, protocol, err)
	}
	return nil
}

func (s *Service) canvasEntryGroups(ctx context.Context, inboundID string, subscriptionOnly bool) ([]CanvasPlanEntryGroup, error) {
	query := "SELECT g.id,g.name FROM node_groups g JOIN node_group_inbounds i ON i.group_id=g.id WHERE i.inbound_id=?"
	if subscriptionOnly {
		query += " AND g.purpose IN ('subscription','mixed')"
	}
	rows, err := s.Repository.Database.QueryContext(ctx, query+" ORDER BY g.id", inboundID)
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

// Pending objects use display names until publication assigns their IDs.
func (s *Service) canvasEntryRoute(ctx context.Context, entry canvasEntry) (string, error) {
	switch {
	case entry.finalOutbound != "":
		return entry.finalOutbound, nil
	case entry.chain != nil:
		existing, err := s.Repository.canvasChainNamed(ctx, entry.chain.name)
		if errors.Is(err, ErrNotFound) {
			return entry.chain.name, nil
		}
		if err != nil {
			return "", err
		}
		return "chain:" + existing.ID, nil
	case entry.edge != nil:
		existing, err := s.Repository.canvasOutboundNamed(ctx, entry.edge.sourceNode, entry.edge.outboundName)
		if errors.Is(err, ErrNotFound) {
			return entry.edge.outboundName, nil
		}
		if err != nil {
			return "", err
		}
		return existing.ID, nil
	}
	return entry.existing.DefaultOutbound, nil
}

func (s *Service) canvasPlanEntryRoute(ctx context.Context, layout CanvasLayout, entry *canvasEntry, names map[string]string) error {
	if entry.edge == nil && entry.chain == nil && entry.finalOutbound == "" {
		if entry.existing.ID != "" {
			_, err := s.Repository.CanvasEntryBindingByInbound(ctx, entry.existing.ID)
			if err == nil {
				entry.preview.Route = entry.existing.DefaultOutbound
				return nil
			}
			if !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		return errors.New("绑定的用户入口没有目标路由，请连接本方案的出口")
	}
	route, err := s.canvasEntryRoute(ctx, *entry)
	if err != nil {
		return err
	}
	entry.preview.Route = route
	if entry.existing.ID != "" && entry.binding.Mode == "create" {
		return nil
	}
	if entry.binding.Mode != "create" {
		return nil
	}
	path := []string{entry.preview.NodeID}
	var last *canvasEdge
	if entry.chain != nil {
		path = entry.chain.nodes
		last = entry.chain.outbounds[len(entry.chain.outbounds)-1]
	} else if entry.edge != nil {
		path = append(path, entry.edge.targetNode)
		last = entry.edge
	}
	if entry.existing.ID == "" {
		entry.preview.Name = strings.Join(nodeNames(names, path), " → ")
	}
	if entry.binding.GroupMode != "exit_groups" || last == nil {
		return nil
	}
	for _, c := range layout.Connections {
		if c.ID != last.connectionID {
			continue
		}
		for _, instance := range layout.Instances {
			if instance.InstanceID == c.Target && instance.Role == "exit" {
				ref, _ := parseCanvasRef(instance.Ref)
				if ref.kind == "inbound" {
					entry.preview.Groups, err = s.canvasEntryGroups(ctx, ref.id, true)
					return err
				}
			}
		}
	}
	return nil
}
