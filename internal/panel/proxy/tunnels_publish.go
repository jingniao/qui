package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
)

type PublishSelection struct {
	TunnelIDs []string `json:"tunnelIds"`
	RuleIDs   []string `json:"ruleIds"`
}
type PublishListener struct {
	Port    int    `json:"port"`
	Network string `json:"network"`
	Purpose string `json:"purpose"`
}
type PublishChange struct {
	Action string `json:"action"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
}
type PublishServer struct {
	NodeID    string            `json:"nodeId"`
	NodeName  string            `json:"nodeName"`
	Order     int               `json:"order"`
	Listeners []PublishListener `json:"listeners"`
	Routes    []string          `json:"routes"`
	Changes   []PublishChange   `json:"changes"`
}
type PublishConflict struct {
	NodeID string `json:"nodeId"`
	Port   int    `json:"port"`
	UsedBy string `json:"usedBy"`
}
type PublishPlan struct {
	OK        bool              `json:"ok"`
	Message   string            `json:"message,omitempty"`
	Servers   []PublishServer   `json:"servers"`
	Conflicts []PublishConflict `json:"conflicts"`
	Warnings  []string          `json:"warnings"`
}
type PublishServerResult struct {
	NodeID   string `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Revision *int64 `json:"revision"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}
type PublishResult struct {
	BatchID string                `json:"batchId,omitempty"`
	Servers []PublishServerResult `json:"servers"`
}

// TunnelDeployer uses the same deployment batches as canvas, but waits for their
// terminal results before advancing the captured business-object versions.
type TunnelDeployer interface {
	PublishTunnels(context.Context, string, string, []NodeSnapshot) (CanvasPublishResult, error)
}
type managedInbound struct {
	legacy bool
	owner  PortOwner
	input  InboundInput
	id     string
}
type tunnelPublication struct {
	relays   map[string]published.Relay
	view     PublishPlan
	tunnels  []Tunnel
	rules    []TunnelRule
	desired  []managedInbound
	previous []managedInbound
}

func (r *Repository) managedBy(ctx context.Context, inboundID string) (*PortOwner, error) {
	if inboundID == "" {
		return nil, nil
	}
	var present int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tunnel_objects'").Scan(&present); err != nil {
		return nil, err
	}
	if present == 0 {
		// Pre-0129 backup inspection has no managed tunnel objects.
		return nil, nil
	}
	var o PortOwner
	err := r.Database.QueryRowContext(ctx, `SELECT o.owner_kind,o.owner_id,coalesce(t.name,r.name,'') FROM tunnel_objects o LEFT JOIN tunnels t ON o.owner_kind='tunnel' AND t.id=o.owner_id LEFT JOIN tunnel_rules r ON o.owner_kind='rule' AND r.id=o.owner_id WHERE o.object_kind='inbound' AND o.object_id=?`, inboundID).Scan(&o.Kind, &o.ID, &o.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}
func (r *Repository) refuseManagedInbound(ctx context.Context, inboundID string) error {
	o, err := r.managedBy(ctx, inboundID)
	if err != nil {
		return err
	}
	if o != nil {
		return tunnelFail("managed_by_tunnel", 409, "该入站由隧道管理，请在隧道或规则中修改")
	}
	return nil
}
func (r *Repository) managedInbounds(ctx context.Context, owner PortOwner) ([]managedInbound, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT object_id,EXISTS(SELECT 1 FROM canvas_forward_reconcile,json_each(migrated_ids) m WHERE m.value=owner_id) FROM tunnel_objects WHERE owner_kind=? AND owner_id=? ORDER BY object_id", owner.Kind, owner.ID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	legacy := map[string]bool{}
	for rows.Next() {
		var objectID string
		var migrated bool
		if err = rows.Scan(&objectID, &migrated); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, objectID)
		legacy[objectID] = migrated
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []managedInbound{}
	for _, objectID := range ids {
		in, e := r.Inbound(ctx, objectID, true)
		if e != nil {
			return nil, e
		}
		result = append(result, managedInbound{legacy: legacy[objectID], owner: owner, id: objectID, input: inboundInputOf(in, in.DefaultOutbound)})
	}
	return result, nil
}

func (s *Service) PlanTunnels(ctx context.Context, selection PublishSelection) (PublishPlan, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	p, err := s.buildTunnelPublication(ctx, selection)
	return p.view, err
}

func (s *Service) buildTunnelPublication(ctx context.Context, selection PublishSelection) (tunnelPublication, error) {
	p := tunnelPublication{view: PublishPlan{OK: true, Servers: []PublishServer{}, Conflicts: []PublishConflict{}, Warnings: []string{}}}
	r := s.Repository
	selectedT := map[string]bool{}
	selectedR := map[string]bool{}
	for _, tid := range selection.TunnelIDs {
		selectedT[tid] = true
	}
	for _, rid := range selection.RuleIDs {
		selectedR[rid] = true
	}
	if len(selectedT)+len(selectedR) == 0 {
		return p, tunnelFail("invalid", 400, "请选择隧道或规则")
	}
	// Include all rules of a selected tunnel: edits may change its entry and every
	// rule's next-hop credentials. Publishing only its listeners would break them.
	allRules, err := r.storedTunnelRules(ctx, "", "")
	if err != nil {
		return p, err
	}
	for _, rule := range allRules {
		if rule.TunnelID != nil && selectedT[*rule.TunnelID] {
			selectedR[rule.ID] = true
		}
	}
	for _, rid := range selection.RuleIDs {
		rule, e := r.TunnelRule(ctx, rid)
		if e != nil {
			return p, e
		}
		if rule.TunnelID != nil && !selectedT[*rule.TunnelID] {
			selectedT[*rule.TunnelID] = true
			p.view.Warnings = append(p.view.Warnings, "已自动包含规则依赖的隧道及其规则："+*rule.TunnelName)
		}
	}
	// Deterministic database order rather than map iteration determines plan order.
	tunnels, err := r.Tunnels(ctx)
	if err != nil {
		return p, err
	}
	for _, t := range tunnels {
		if selectedT[t.ID] {
			p.tunnels = append(p.tunnels, t)
			delete(selectedT, t.ID)
		}
	}
	if len(selectedT) > 0 {
		return p, ErrNotFound
	}
	included := map[string]Tunnel{}
	for _, t := range p.tunnels {
		included[t.ID] = t
		// A dirty dependency changes all its rule dialers (especially rotation).
		// Include those rules in the reviewable plan before applying anything.
		if t.DraftVersion != t.PublishedVersion {
			for _, rule := range allRules {
				if rule.TunnelID != nil && *rule.TunnelID == t.ID {
					selectedR[rule.ID] = true
				}
			}
		}
	}
	for _, rule := range allRules {
		if selectedR[rule.ID] {
			if rule.Status != "pending_delete" {
				rule.Target, err = deriveTunnelTarget(ctx, r.Database, rule.Target, rule.TunnelID)
				if err != nil {
					return p, err
				}
			}
			p.rules = append(p.rules, rule)
			delete(selectedR, rule.ID)
		}
	}
	if len(selectedR) > 0 {
		return p, ErrNotFound
	}
	p.relays = map[string]published.Relay{}
	for _, rule := range p.rules {
		if rule.Status == "pending_delete" {
			continue
		}
		relay, e := r.relaySnapshot(ctx, r.Database, rule)
		if e != nil {
			return p, e
		}
		p.relays[rule.ID] = relay
	}
	servers := map[string]*PublishServer{}
	nodes := []string{}
	before := map[string][]string{}
	addNode := func(nodeID string) error {
		if servers[nodeID] != nil {
			return nil
		}
		n, e := s.Nodes.Get(ctx, nodeID)
		if e != nil {
			return e
		}
		servers[nodeID] = &PublishServer{NodeID: nodeID, NodeName: n.Name, Listeners: []PublishListener{}, Routes: []string{}, Changes: []PublishChange{}}
		nodes = append(nodes, nodeID)
		return nil
	}
	loadOwner := func(o PortOwner) error {
		old, e := r.managedInbounds(ctx, o)
		if e != nil {
			return e
		}
		p.previous = append(p.previous, old...)
		for _, in := range old {
			if e = addNode(in.input.NodeID); e != nil {
				return e
			}
		}
		return nil
	}
	firstHops := map[string]desiredstate.GostHop{}
	for _, t := range p.tunnels {
		owner := PortOwner{Kind: "tunnel", ID: t.ID, Name: t.Name}
		if err = loadOwner(owner); err != nil {
			return p, err
		}
		for i := len(t.Hops) - 1; i >= 0; i-- {
			if err = addNode(t.Hops[i].NodeID); err != nil {
				return p, err
			}
			if i > 0 {
				before[t.Hops[i-1].NodeID] = append(before[t.Hops[i-1].NodeID], t.Hops[i].NodeID)
			}
		}
		route := []string{}
		for _, h := range t.Hops {
			label := h.NodeName
			if h.ListenPort != nil {
				label += fmt.Sprintf(" :%d", *h.ListenPort)
			}
			route = append(route, label)
		}
		for _, h := range t.Hops {
			servers[h.NodeID].Routes = append(servers[h.NodeID].Routes, strings.Join(route, " → "))
		}
		if t.Status == "pending_delete" {
			continue
		}
		material, e := r.openGostMaterial(t.ID, t.sealedMaterial)
		if e != nil {
			return p, tunnelFail("invalid", 409, "隧道凭据不可用，请轮换凭据")
		}
		required := uint32(desiredstate.GostRelayContractVersion)
		if len(t.Hops) == 3 {
			required = desiredstate.GostMiddleContractVersion
			if material.Next == nil {
				return p, tunnelFail("invalid", 409, "三节点隧道需要独立分段凭据，请轮换凭据")
			}
		}
		for _, h := range t.Hops {
			n, e := s.Nodes.Get(ctx, h.NodeID)
			if e != nil {
				return p, e
			}
			core := node.SelectedCore(n.Capabilities, "gost")
			if !n.Enabled || n.DeletedAt != "" || core == nil || !core.ForwardInboundSupported || core.GostContractVersion < required || node.CoreSelectionIsBlocked(n, "gost") {
				return p, tunnelFail("capability", 409, fmt.Sprintf("服务器 %s 需要启用 GOST 且 Agent 支持契约 %d，请升级 Agent", n.Name, required))
			}
		}
		var next *desiredstate.GostHop
		for i := len(t.Hops) - 1; i > 0; i-- {
			h := t.Hops[i]
			host := h.ConnectHost
			if host == "" {
				n, e := s.Nodes.Get(ctx, h.NodeID)
				if e != nil {
					return p, e
				}
				host = n.PublicIP
			}
			if host == "" {
				return p, tunnelFail("invalid", 409, "服务器 "+h.NodeName+" 没有公网 IP 或连接地址")
			}
			leg := material
			if i == 2 {
				leg = *material.Next
			}
			relay := canvasRelay{exitNode: h.NodeID, name: "[隧道] " + t.Name + " " + h.Role, port: *h.ListenPort, material: leg}
			in := relay.relayInput(host)
			if next != nil {
				in.Settings.Forward.Hops = []desiredstate.GostHop{*next}
			}
			if err = in.Settings.Forward.ValidateGostContract(required); err != nil {
				return p, err
			}
			p.desired = append(p.desired, managedInbound{owner: owner, input: in})
			hop := relay.hopFor(host)
			next = &hop
		}
		firstHops[t.ID] = *next
	}
	for _, rule := range p.rules {
		owner := PortOwner{Kind: "rule", ID: rule.ID, Name: rule.Name}
		if err = loadOwner(owner); err != nil {
			return p, err
		}
		if err = addNode(rule.EntryNodeID); err != nil {
			return p, err
		}
		if rule.Status == "pending_delete" {
			continue
		}
		n, e := s.Nodes.Get(ctx, rule.EntryNodeID)
		if e != nil {
			return p, e
		}
		core := n.Capabilities.GetCoreName()
		if node.SelectedCore(n.Capabilities, "gost") != nil {
			core = "gost"
		}
		if !n.Enabled || n.DeletedAt != "" || node.CoreSelectionIsBlocked(n, core) {
			return p, tunnelFail("capability", 409, "入口服务器不可用或核心正在更新")
		}
		if e = ValidateInboundCore(n.Capabilities, core, "forward"); e != nil {
			return p, tunnelFail("capability", 409, "入口服务器不支持端口转发："+e.Error())
		}
		network := rule.Network
		if network == "both" {
			network = "tcp,udp"
		}
		f := &desiredstate.ForwardSettings{Host: rule.Target.Host, Port: rule.Target.Port, Network: network}
		route := fmt.Sprintf("%s :%d", rule.EntryNodeName, *rule.ListenPort)
		if rule.TunnelID != nil {
			t := included[*rule.TunnelID]
			if t.Status == "pending_delete" {
				return p, tunnelFail("invalid", 409, "规则依赖待删除隧道")
			}
			f.Hops = []desiredstate.GostHop{firstHops[t.ID]}
			core = "gost"
			for _, h := range t.Hops[1:] {
				route += " → " + h.NodeName
			}
		}
		route += " → " + net.JoinHostPort(rule.Target.Host, strconv.Itoa(rule.Target.Port))
		servers[rule.EntryNodeID].Routes = append(servers[rule.EntryNodeID].Routes, route)
		in := InboundInput{Core: core, Name: "[隧道] " + rule.Name, NodeID: rule.EntryNodeID, Protocol: "forward", Listen: "0.0.0.0", Port: *rule.ListenPort, Enabled: rule.Enabled, DefaultOutbound: "direct", AllowedUserIDs: []string{}, Settings: desiredstate.ProtocolSettings{Forward: f}}
		p.desired = append(p.desired, managedInbound{owner: owner, input: in})
	}
	used := map[string]bool{}
	for i := range p.desired {
		in := &p.desired[i]
		for _, old := range p.previous {
			if old.owner.Kind == in.owner.Kind && old.owner.ID == in.owner.ID && old.input.NodeID == in.input.NodeID {
				in.id = old.id
				if old.legacy && migrationOwnerCurrent(p, in.owner) {
					in.input = old.input
				}
				used[old.id] = true
				break
			}
		}
		if err = validateGeneratedInbound(in.input); err != nil {
			return p, err
		}
		action := "create"
		if in.id != "" {
			action = "update"
		}
		server := servers[in.input.NodeID]
		changed := true
		for _, old := range p.previous {
			if old.id == in.id && reflect.DeepEqual(old.input, in.input) {
				changed = false
			}
		}
		if changed {
			server.Changes = append(server.Changes, PublishChange{Action: action, Kind: in.owner.Kind, Name: in.input.Name})
		}
		network := "both"
		if in.input.Settings.Forward.Network != "" {
			network = strings.ReplaceAll(in.input.Settings.Forward.Network, "tcp,udp", "both")
		}
		if in.input.Enabled {
			server.Listeners = append(server.Listeners, PublishListener{Port: in.input.Port, Network: network, Purpose: in.input.Name})
		}
		occupied, e := r.portOccupants(ctx, r.Database, in.input.NodeID, []PortOwner{in.owner}, "enabled")
		if e != nil {
			return p, e
		}
		if c := checkPort(in.input.NodeID, in.input.Port, occupied); !c.Free {
			p.view.Conflicts = append(p.view.Conflicts, PublishConflict{NodeID: c.NodeID, Port: c.Port, UsedBy: c.UsedBy.Name})
		}
	}
	for _, old := range p.previous {
		if !used[old.id] {
			servers[old.input.NodeID].Changes = append(servers[old.input.NodeID].Changes, PublishChange{Action: "remove", Kind: old.owner.Kind, Name: old.input.Name})
		}
	}
	ordered, err := canvasPublishOrder(nodes, before)
	if err != nil {
		return p, err
	}
	for i, nodeID := range ordered {
		server := servers[nodeID]
		server.Order = i + 1
		p.view.Servers = append(p.view.Servers, *server)
	}
	p.view.OK = len(p.view.Conflicts) == 0
	if !p.view.OK {
		p.view.Message = "监听端口冲突，请修改后重试"
	}
	return p, nil
}

// replaceManagedInbounds is one atomic reconciliation, including ID ownership.
// It never touches ordinary or canvas objects, even if their names match.
func (r *Repository) replaceManagedInbounds(ctx context.Context, remove, desired []managedInbound) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	keep := map[string]bool{}
	for _, in := range desired {
		keep[in.id] = true
	}
	for _, in := range remove {
		if !keep[in.id] {
			if _, err = tx.ExecContext(ctx, "DELETE FROM inbounds WHERE id=?", in.id); err != nil {
				return err
			}
		}
	}
	for _, in := range desired {
		encoded, e := r.encode(in.input.Settings)
		if e != nil {
			return e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,relay_credential_id,created_at,core) VALUES(?,?,?,'forward',?,?,?,0,0,'direct','[]',?,'',?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,listen=excluded.listen,port=excluded.port,enabled=excluded.enabled,encrypted_settings=excluded.encrypted_settings,core=excluded.core`, in.id, in.input.Name, in.input.NodeID, in.input.Listen, in.input.Port, in.input.Enabled, encoded, timestamp(), in.input.Core)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tunnel_objects(object_kind,object_id,owner_kind,owner_id) VALUES('inbound',?,?,?) ON CONFLICT(object_id) DO NOTHING`, in.id, in.owner.Kind, in.owner.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) PublishTunnels(ctx context.Context, selection PublishSelection, author string, users AccessReader) (PublishResult, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	result := PublishResult{Servers: []PublishServerResult{}}
	deploy, ok := s.Deploy.(TunnelDeployer)
	if !ok {
		return result, errors.New("tunnel deployment unavailable")
	}
	p, err := s.buildTunnelPublication(ctx, selection)
	if err != nil {
		return result, err
	}
	if !p.view.OK {
		return result, tunnelFail("port_in_use", 409, p.view.Message)
	}
	for i := range p.desired {
		if p.desired[i].id == "" {
			p.desired[i].id, err = id.New()
			if err != nil {
				return result, err
			}
		}
	}
	if err = s.Repository.replaceManagedInbounds(ctx, p.previous, p.desired); err != nil {
		return result, err
	}
	restore := func() error {
		return s.Repository.replaceManagedInbounds(context.WithoutCancel(ctx), p.desired, p.previous)
	}
	snapshots := []NodeSnapshot{}
	for _, server := range p.view.Servers {
		state, intent, e := s.SnapshotIntent(ctx, server.NodeID, users)
		if e != nil {
			return result, errors.Join(e, restore())
		}
		snapshots = append(snapshots, NodeSnapshot{State: state, InboundIntent: intent, ExpandedOnly: true})
	}
	deployed, err := deploy.PublishTunnels(ctx, "隧道与规则发布", author, snapshots)
	result.BatchID = deployed.BatchID
	success := err == nil
	for _, server := range p.view.Servers {
		item := PublishServerResult{NodeID: server.NodeID, NodeName: server.NodeName, Status: "unchanged"}
		for _, n := range deployed.Nodes {
			if n.NodeID == server.NodeID {
				revision := n.Revision
				item.Revision = &revision
				item.Status = n.Status
			}
		}
		if err != nil && item.Status == "unchanged" {
			item.Status = "skipped"
			item.Message = err.Error()
		}
		if item.Status != "applied" && item.Status != "unchanged" {
			success = false
			switch item.Status {
			case "failed":
				item.Message = "服务器配置发布失败"
			case "rolled_back":
				item.Message = "后续服务器失败，已回滚"
			case "skipped":
				item.Message = "发布中止，未下发"
			default:
				item.Status = "failed"
				item.Message = "未确认发布结果，保留草稿与端口声明"
			}
		}
		result.Servers = append(result.Servers, item)
	}
	if !success {
		if e := restore(); e != nil {
			return result, errors.Join(err, e)
		}
		return result, nil
	}
	if err = s.Repository.finishTunnelPublication(ctx, p); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Repository) finishTunnelPublication(ctx context.Context, p tunnelPublication) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	finish := func(table, kind, objectID, status string, version int64) error {
		if status == "pending_delete" {
			if _, e := tx.ExecContext(ctx, "DELETE FROM port_claims WHERE owner_kind=? AND owner_id=?", kind, objectID); e != nil {
				return e
			}
			_, e := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id=? AND draft_version=?", objectID, version)
			return e
		}
		res, e := tx.ExecContext(ctx, "UPDATE "+table+" SET status='published',published_version=draft_version,published_at=? WHERE id=? AND draft_version=?", timestamp(), objectID, version)
		if e != nil {
			return e
		}
		count, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if count != 1 {
			return tunnelFail("stale", 409, "发布期间草稿已改变，请重新发布")
		}
		_, e = tx.ExecContext(ctx, `DELETE FROM port_claims WHERE owner_kind=? AND owner_id=? AND NOT EXISTS(SELECT 1 FROM tunnel_objects o JOIN inbounds i ON i.id=o.object_id WHERE o.owner_id=? AND o.owner_kind=? AND i.node_id=port_claims.node_id AND i.port=port_claims.port)`, kind, objectID, objectID, strings.ReplaceAll(kind, "tunnel_hop", "tunnel"))
		return e
	}
	for _, rule := range p.rules {
		if err = finish("tunnel_rules", "rule", rule.ID, rule.Status, rule.DraftVersion); err != nil {
			return err
		}
		if rule.Status != "pending_delete" {
			if err = r.saveRelaySnapshot(ctx, tx, p.relays[rule.ID]); err != nil {
				return err
			}
		}
	}
	for _, t := range p.tunnels {
		if err = finish("tunnels", "tunnel_hop", t.ID, t.Status, t.DraftVersion); err != nil {
			return err
		}
	}
	return tx.Commit()
}
