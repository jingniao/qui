package proxy

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
)

type TunnelHop struct {
	Role        string `json:"role"`
	NodeID      string `json:"nodeId"`
	NodeName    string `json:"nodeName"`
	ListenPort  *int   `json:"listenPort"`
	PortMode    string `json:"portMode"`
	ConnectHost string `json:"connectHost"`
}
type TunnelInput struct {
	Name      string      `json:"name"`
	Transport string      `json:"transport"`
	Hops      []TunnelHop `json:"hops"`
	UpdatedAt string      `json:"updatedAt,omitempty"`
}
type Tunnel struct {
	TunnelInput
	ID               string                   `json:"id"`
	RuleCount        int                      `json:"ruleCount"`
	Status           string                   `json:"status"`
	Credentials      *CanvasTunnelCredentials `json:"credentials"`
	CreatedAt        string                   `json:"createdAt"`
	PublishedAt      *string                  `json:"publishedAt"`
	DraftVersion     int64                    `json:"-"`
	PublishedVersion int64                    `json:"-"`
	sealedMaterial   []byte
}
type TunnelTarget struct {
	Mode        string `json:"mode"`
	NodeID      string `json:"nodeId,omitempty"`
	NodeName    string `json:"nodeName,omitempty"`
	InboundID   string `json:"inboundId,omitempty"`
	InboundName string `json:"inboundName,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
}
type TunnelRuleInput struct {
	Name                string       `json:"name"`
	EntryNodeID         string       `json:"entryNodeId"`
	ListenPort          *int         `json:"listenPort,omitempty"`
	PortMode            string       `json:"portMode"`
	Network             string       `json:"network"`
	TunnelID            *string      `json:"tunnelId"`
	Target              TunnelTarget `json:"target"`
	SubscriptionVisible bool         `json:"subscriptionVisible"`
	Enabled             bool         `json:"enabled"`
	UpdatedAt           string       `json:"updatedAt,omitempty"`
}
type TunnelRule struct {
	TunnelRuleInput
	ID               string  `json:"id"`
	EntryNodeName    string  `json:"entryNodeName"`
	TunnelName       *string `json:"tunnelName"`
	OwnerNodeID      *string `json:"ownerNodeId"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"createdAt"`
	PublishedAt      *string `json:"publishedAt"`
	DraftVersion     int64   `json:"-"`
	PublishedVersion int64   `json:"-"`
}

const managedTunnelColumns = "id,name,transport,status,draft_version,published_version,published_at,gost_material,created_at,updated_at"

func scanManagedTunnel(row interface{ Scan(...any) error }) (Tunnel, error) {
	var t Tunnel
	err := row.Scan(&t.ID, &t.Name, &t.Transport, &t.Status, &t.DraftVersion, &t.PublishedVersion, &t.PublishedAt, &t.sealedMaterial, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return t, err
}
func (r *Repository) attachTunnel(ctx context.Context, t *Tunnel) error {
	rows, err := r.Database.QueryContext(ctx, `SELECT h.role,h.node_id,n.name,h.listen_port,h.port_mode,h.connect_host FROM tunnel_hops h JOIN nodes n ON n.id=h.node_id WHERE tunnel_id=? ORDER BY position`, t.ID)
	if err != nil {
		return err
	}
	t.Hops = []TunnelHop{}
	for rows.Next() {
		var h TunnelHop
		if err = rows.Scan(&h.Role, &h.NodeID, &h.NodeName, &h.ListenPort, &h.PortMode, &h.ConnectHost); err != nil {
			rows.Close()
			return err
		}
		t.Hops = append(t.Hops, h)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = r.Database.QueryRowContext(ctx, "SELECT count(*) FROM tunnel_rules WHERE tunnel_id=?", t.ID).Scan(&t.RuleCount); err != nil {
		return err
	}
	material, e := r.openGostMaterial(t.ID, t.sealedMaterial)
	if e == nil {
		t.Credentials = &CanvasTunnelCredentials{Generation: material.Generation, ServerName: material.ServerName, NotAfter: material.NotAfter, Fingerprint: material.Fingerprint}
	}
	return nil
}
func (r *Repository) Tunnel(ctx context.Context, tunnelID string) (Tunnel, error) {
	t, err := scanManagedTunnel(r.Database.QueryRowContext(ctx, "SELECT "+managedTunnelColumns+" FROM tunnels WHERE id=?", tunnelID))
	if err == nil {
		err = r.attachTunnel(ctx, &t)
	}
	return t, err
}
func (r *Repository) Tunnels(ctx context.Context) ([]Tunnel, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT "+managedTunnelColumns+" FROM tunnels ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	items := []Tunnel{}
	for rows.Next() {
		t, e := scanManagedTunnel(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range items {
		if err = r.attachTunnel(ctx, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

const managedRuleColumns = `r.id,r.name,r.entry_node_id,n.name,r.listen_port,r.port_mode,r.network,r.tunnel_id,t.name,r.target_mode,r.target_inbound_id,coalesce(r.target_host,''),coalesce(r.target_port,0),r.owner_node_id,r.subscription_visible,r.enabled,r.status,r.draft_version,r.published_version,r.published_at,r.created_at,r.updated_at`
const managedRuleFrom = ` FROM tunnel_rules r JOIN nodes n ON n.id=r.entry_node_id LEFT JOIN tunnels t ON t.id=r.tunnel_id`

func scanManagedRule(row interface{ Scan(...any) error }) (TunnelRule, error) {
	var r TunnelRule
	var inbound *string
	err := row.Scan(&r.ID, &r.Name, &r.EntryNodeID, &r.EntryNodeName, &r.ListenPort, &r.PortMode, &r.Network, &r.TunnelID, &r.TunnelName, &r.Target.Mode, &inbound, &r.Target.Host, &r.Target.Port, &r.OwnerNodeID, &r.SubscriptionVisible, &r.Enabled, &r.Status, &r.DraftVersion, &r.PublishedVersion, &r.PublishedAt, &r.CreatedAt, &r.UpdatedAt)
	if inbound != nil {
		r.Target.InboundID = *inbound
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}
func deriveTunnelTarget(ctx context.Context, q portDatabase, target TunnelTarget, tunnelID *string) (TunnelTarget, error) {
	if target.Mode != "linked" {
		return TunnelTarget{Mode: target.Mode, Host: target.Host, Port: target.Port}, nil
	}
	t := TunnelTarget{Mode: "linked", InboundID: target.InboundID}
	var internal bool
	var deleted string
	err := q.QueryRowContext(ctx, `SELECT i.node_id,n.name,i.name,i.protocol,n.public_ip,i.port,i.internal_relay,coalesce(n.deleted_at,'') FROM inbounds i JOIN nodes n ON n.id=i.node_id WHERE i.id=?`, target.InboundID).Scan(&t.NodeID, &t.NodeName, &t.InboundName, &t.Protocol, &t.Host, &t.Port, &internal, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	if internal || t.Protocol == "forward" || deleted != "" {
		return t, tunnelFail("invalid", 400, "目标必须是未删除服务器的用户入站")
	}
	if tunnelID != nil {
		var exit string
		if err = q.QueryRowContext(ctx, "SELECT node_id FROM tunnel_hops WHERE tunnel_id=? AND role='exit'", *tunnelID).Scan(&exit); err != nil {
			return t, err
		}
		if exit == t.NodeID {
			t.Host = "127.0.0.1"
		}
	}
	if t.Host == "" {
		return t, tunnelFail("invalid", 400, "目标服务器没有公网 IP")
	}
	return t, nil
}
func (r *Repository) TunnelRule(ctx context.Context, ruleID string) (TunnelRule, error) {
	item, err := scanManagedRule(r.Database.QueryRowContext(ctx, "SELECT "+managedRuleColumns+managedRuleFrom+" WHERE r.id=?", ruleID))
	if err == nil && item.Status != "pending_delete" {
		item.Target, err = deriveTunnelTarget(ctx, r.Database, item.Target, item.TunnelID)
	}
	return item, err
}
func (r *Repository) TunnelRules(ctx context.Context, tunnelID, nodeID string) ([]TunnelRule, error) {
	items, err := r.storedTunnelRules(ctx, tunnelID, nodeID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Status == "pending_delete" {
			continue
		}
		items[i].Target, err = deriveTunnelTarget(ctx, r.Database, items[i].Target, items[i].TunnelID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}
func (r *Repository) storedTunnelRules(ctx context.Context, tunnelID, nodeID string) ([]TunnelRule, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT "+managedRuleColumns+managedRuleFrom+" WHERE (?='' OR r.tunnel_id=?) AND (?='' OR r.entry_node_id=?) ORDER BY r.created_at,r.id", tunnelID, tunnelID, nodeID, nodeID)
	if err != nil {
		return nil, err
	}
	items := []TunnelRule{}
	for rows.Next() {
		item, e := scanManagedRule(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return items, nil
}

func editableTunnel(status, oldStamp, inputStamp string) error {
	if status == "pending_delete" {
		return tunnelFail("pending_delete", 409, "待删除对象只读")
	}
	if oldStamp != inputStamp {
		return tunnelFail("stale", 409, "数据已更新，请刷新后重试")
	}
	return nil
}
func managedStatus(publishedAt *string) string {
	if publishedAt != nil {
		return "modified"
	}
	return "draft"
}
func validPortMode(mode string) bool { return mode == "auto" || mode == "manual" }
func managedName(name string) bool   { return strings.TrimSpace(name) != "" && len(name) <= 80 }
func validForwardHost(host string) bool {
	return (&desiredstate.ForwardSettings{Host: host, Port: 1, Network: "tcp"}).Validate() == nil
}

// claimPort runs inside the save transaction. Published owners retain their old
// claims until X1b has successfully published removal of the old listeners.
func (r *Repository) claimPort(ctx context.Context, tx *sql.Tx, nodeID string, port *int, mode string, owner PortOwner, oldPort *int) (int, error) {
	if !validPortMode(mode) {
		return 0, tunnelFail("invalid", 400, "端口模式无效")
	}
	if err := portNode(ctx, tx, nodeID); err != nil {
		return 0, err
	}
	modeCheck := "enabled"
	if mode == "auto" {
		modeCheck = "all"
	}
	occupied, err := r.portOccupants(ctx, tx, nodeID, []PortOwner{owner}, modeCheck)
	if err != nil {
		return 0, err
	}
	if port == nil && mode == "auto" {
		if oldPort != nil {
			port = oldPort
		} else {
			ports, e := allocatePorts(ctx, tx, nodeID, 1, occupied, nil)
			if e != nil {
				return 0, e
			}
			port = &ports[0]
		}
	}
	if port == nil || *port < 1 || *port > 65535 {
		return 0, tunnelFail("port_out_of_range", 400, "端口必须在 1–65535 范围内")
	}
	check := checkPort(nodeID, *port, occupied)
	if !check.Free {
		return 0, &tunnelError{Code: "port_in_use", Status: 409, Message: "端口已被占用", Details: map[string]any{"usedBy": check.UsedBy}}
	}
	kind := owner.Kind
	if kind == "tunnel" {
		kind = "tunnel_hop"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO port_claims(node_id,port,owner_kind,owner_id) VALUES(?,?,?,?) ON CONFLICT(node_id,port) DO NOTHING`, nodeID, *port, kind, owner.ID)
	return *port, err
}
func releaseDraftClaims(ctx context.Context, tx *sql.Tx, kind, ownerID string, publishedAt *string) error {
	if publishedAt != nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM port_claims WHERE owner_kind=? AND owner_id=?", kind, ownerID)
	return err
}

func (s *Service) SaveTunnel(ctx context.Context, tunnelID string, input TunnelInput) (Tunnel, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	input.Name = strings.TrimSpace(input.Name)
	if !managedName(input.Name) || input.Transport != "tls" || len(input.Hops) < 2 || len(input.Hops) > 3 {
		return Tunnel{}, tunnelFail("invalid", 400, "隧道名称、传输或路径无效")
	}
	seen := map[string]bool{}
	for i, h := range input.Hops {
		role := "mid"
		if i == 0 {
			role = "entry"
		} else if i == len(input.Hops)-1 {
			role = "exit"
		}
		if h.Role != role || seen[h.NodeID] || !validPortMode(h.PortMode) {
			return Tunnel{}, tunnelFail("invalid", 400, "隧道路径必须包含不同的入口、中间和出口服务器")
		}
		seen[h.NodeID] = true
		if h.ConnectHost != "" && !validForwardHost(h.ConnectHost) {
			return Tunnel{}, tunnelFail("invalid", 400, "连接地址无效")
		}
	}
	r := s.Repository
	old := Tunnel{}
	var err error
	if tunnelID != "" {
		old, err = r.Tunnel(ctx, tunnelID)
		if err != nil {
			return old, err
		}
		if err = editableTunnel(old.Status, old.UpdatedAt, input.UpdatedAt); err != nil {
			return old, err
		}
	} else {
		tunnelID, err = id.New()
		if err != nil {
			return old, err
		}
	}
	stamp := timestamp()
	if old.ID != "" {
		stamp = nextCanvasStamp(old.UpdatedAt)
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Tunnel{}, err
	}
	defer tx.Rollback()
	for _, h := range input.Hops {
		if err = portNode(ctx, tx, h.NodeID); err != nil {
			return Tunnel{}, err
		}
	}
	if old.ID == "" {
		material, e := newManagedGostMaterial(tunnelID, 1)
		if e != nil {
			return Tunnel{}, e
		}
		sealed, e := r.sealGostMaterial(tunnelID, material)
		if e != nil {
			return Tunnel{}, e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO tunnels(id,name,transport,status,gost_material,created_at,updated_at) VALUES(?,?,?,'draft',?,?,?)`, tunnelID, input.Name, input.Transport, sealed, stamp, stamp)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE tunnels SET name=?,transport=?,status=?,draft_version=draft_version+1,updated_at=? WHERE id=? AND updated_at=?`, input.Name, input.Transport, managedStatus(old.PublishedAt), stamp, tunnelID, input.UpdatedAt)
		if err == nil {
			n, e := result.RowsAffected()
			if e != nil {
				err = e
			} else if n == 0 {
				err = tunnelFail("stale", 409, "数据已更新，请刷新后重试")
			}
		}
	}
	if err != nil {
		return Tunnel{}, managedSQLError(err)
	}
	if err = releaseDraftClaims(ctx, tx, "tunnel_hop", tunnelID, old.PublishedAt); err != nil {
		return Tunnel{}, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM tunnel_hops WHERE tunnel_id=?", tunnelID); err != nil {
		return Tunnel{}, err
	}
	for i, h := range input.Hops {
		var port *int
		if i > 0 {
			var previous *int
			for _, p := range old.Hops {
				if p.NodeID == h.NodeID {
					previous = p.ListenPort
				}
			}
			p, e := r.claimPort(ctx, tx, h.NodeID, h.ListenPort, h.PortMode, PortOwner{Kind: "tunnel", ID: tunnelID}, previous)
			if e != nil {
				return Tunnel{}, e
			}
			port = &p
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tunnel_hops(tunnel_id,role,position,node_id,listen_port,port_mode,connect_host) VALUES(?,?,?,?,?,?,?)`, tunnelID, h.Role, i, h.NodeID, port, h.PortMode, h.ConnectHost); err != nil {
			return Tunnel{}, err
		}
	}
	if old.ID != "" {
		if err = s.retargetTunnelRules(ctx, tx, tunnelID, input.Hops[0].NodeID); err != nil {
			return Tunnel{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Tunnel{}, err
	}
	return r.Tunnel(ctx, tunnelID)
}

func (s *Service) retargetTunnelRules(ctx context.Context, tx *sql.Tx, tunnelID, entry string) error {
	rows, err := tx.QueryContext(ctx, "SELECT id,entry_node_id,listen_port,port_mode,published_at,target_mode,coalesce(target_inbound_id,''),updated_at FROM tunnel_rules WHERE tunnel_id=? AND status<>'pending_delete'", tunnelID)
	if err != nil {
		return err
	}
	type linked struct {
		id, node, mode string
		port           int
		published      *string
		target         TunnelTarget
		stamp          string
	}
	items := []linked{}
	for rows.Next() {
		var x linked
		if err = rows.Scan(&x.id, &x.node, &x.port, &x.mode, &x.published, &x.target.Mode, &x.target.InboundID, &x.stamp); err != nil {
			rows.Close()
			return err
		}
		items = append(items, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range items {
		if _, err = deriveTunnelTarget(ctx, tx, x.target, &tunnelID); err != nil {
			return err
		}
		if x.node != entry {
			if err = releaseDraftClaims(ctx, tx, "rule", x.id, x.published); err != nil {
				return err
			}
			if _, err = s.Repository.claimPort(ctx, tx, entry, &x.port, x.mode, PortOwner{Kind: "rule", ID: x.id}, nil); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tunnel_rules SET entry_node_id=?,status=?,draft_version=draft_version+1,updated_at=? WHERE id=?`, entry, managedStatus(x.published), nextCanvasStamp(x.stamp), x.id); err != nil {
			return err
		}
	}
	return nil
}

func managedSQLError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed: tunnels.name") {
		return tunnelFail("invalid", 400, "隧道名称已存在")
	}
	return err
}

func (s *Service) SaveTunnelRule(ctx context.Context, ruleID string, input TunnelRuleInput) (TunnelRule, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	input.Name = strings.TrimSpace(input.Name)
	if !managedName(input.Name) || !validPortMode(input.PortMode) || (input.Network != "tcp" && input.Network != "udp" && input.Network != "both") {
		return TunnelRule{}, tunnelFail("invalid", 400, "规则名称、网络或端口模式无效")
	}
	if input.TunnelID != nil && *input.TunnelID == "" {
		input.TunnelID = nil
	}
	if input.Target.Mode != "linked" && input.Target.Mode != "manual" {
		return TunnelRule{}, tunnelFail("invalid", 400, "目标模式无效")
	}
	if input.Target.Mode == "manual" {
		if !validForwardHost(input.Target.Host) || input.Target.Port < 1 || input.Target.Port > 65535 {
			return TunnelRule{}, tunnelFail("invalid", 400, "目标地址或端口无效")
		}
		input.SubscriptionVisible = false
	}
	r := s.Repository
	old := TunnelRule{}
	var err error
	if ruleID != "" {
		// Read the stored row so an invalidated destination never prevents repair.
		old, err = scanManagedRule(r.Database.QueryRowContext(ctx, "SELECT "+managedRuleColumns+managedRuleFrom+" WHERE r.id=?", ruleID))
		if err != nil {
			return old, err
		}
		if err = editableTunnel(old.Status, old.UpdatedAt, input.UpdatedAt); err != nil {
			return old, err
		}
	} else {
		ruleID, err = id.New()
		if err != nil {
			return old, err
		}
	}
	stamp := timestamp()
	if old.ID != "" {
		stamp = nextCanvasStamp(old.UpdatedAt)
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return TunnelRule{}, err
	}
	defer tx.Rollback()
	if input.TunnelID != nil {
		var entry, status string
		err = tx.QueryRowContext(ctx, `SELECT h.node_id,t.status FROM tunnels t JOIN tunnel_hops h ON h.tunnel_id=t.id AND h.role='entry' WHERE t.id=?`, *input.TunnelID).Scan(&entry, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return TunnelRule{}, ErrNotFound
		}
		if err != nil {
			return TunnelRule{}, err
		}
		if status == "pending_delete" {
			return TunnelRule{}, tunnelFail("pending_delete", 409, "隧道待删除，不能使用")
		}
		if entry != input.EntryNodeID {
			return TunnelRule{}, tunnelFail("invalid", 400, "规则入口必须与隧道入口一致")
		}
	}
	if _, err = deriveTunnelTarget(ctx, tx, input.Target, input.TunnelID); err != nil {
		return TunnelRule{}, err
	}
	if err = releaseDraftClaims(ctx, tx, "rule", ruleID, old.PublishedAt); err != nil {
		return TunnelRule{}, err
	}
	var previous *int
	if old.EntryNodeID == input.EntryNodeID {
		previous = old.ListenPort
	}
	p, err := r.claimPort(ctx, tx, input.EntryNodeID, input.ListenPort, input.PortMode, PortOwner{Kind: "rule", ID: ruleID}, previous)
	if err != nil {
		return TunnelRule{}, err
	}
	var inbound, host, port any
	if input.Target.Mode == "linked" {
		inbound = input.Target.InboundID
	} else {
		host = input.Target.Host
		port = input.Target.Port
	}
	args := []any{input.Name, input.EntryNodeID, p, input.PortMode, input.Network, input.TunnelID, input.Target.Mode, inbound, host, port, input.SubscriptionVisible, input.Enabled}
	if old.ID == "" {
		args = append(args, ruleID, stamp, stamp)
		_, err = tx.ExecContext(ctx, `INSERT INTO tunnel_rules(name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,target_host,target_port,subscription_visible,enabled,id,created_at,updated_at,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'draft')`, args...)
	} else {
		args = append(args, managedStatus(old.PublishedAt), stamp, ruleID, input.UpdatedAt)
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE tunnel_rules SET name=?,entry_node_id=?,listen_port=?,port_mode=?,network=?,tunnel_id=?,target_mode=?,target_inbound_id=?,target_host=?,target_port=?,subscription_visible=?,enabled=?,status=?,draft_version=draft_version+1,updated_at=? WHERE id=? AND updated_at=?`, args...)
		if err == nil {
			n, e := result.RowsAffected()
			if e != nil {
				err = e
			} else if n == 0 {
				err = tunnelFail("stale", 409, "数据已更新，请刷新后重试")
			}
		}
	}
	if err != nil {
		return TunnelRule{}, err
	}
	if err = tx.Commit(); err != nil {
		return TunnelRule{}, err
	}
	return r.TunnelRule(ctx, ruleID)
}

func (s *Service) DeleteTunnel(ctx context.Context, tunnelID string) error {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	return s.deleteManagedTunnel(ctx, "tunnels", "tunnel_hop", tunnelID)
}
func (s *Service) DeleteTunnelRule(ctx context.Context, ruleID string) error {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	return s.deleteManagedTunnel(ctx, "tunnel_rules", "rule", ruleID)
}
func (s *Service) deleteManagedTunnel(ctx context.Context, table, kind, objectID string) error {
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var published *string
	var status, stamp string
	err = tx.QueryRowContext(ctx, "SELECT published_at,status,updated_at FROM "+table+" WHERE id=?", objectID).Scan(&published, &status, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if table == "tunnels" {
		rows, e := tx.QueryContext(ctx, "SELECT id,name FROM tunnel_rules WHERE tunnel_id=? ORDER BY id", objectID)
		if e != nil {
			return e
		}
		rules := []CanvasSchemeRef{}
		for rows.Next() {
			var x CanvasSchemeRef
			if e = rows.Scan(&x.ID, &x.Name); e != nil {
				rows.Close()
				return e
			}
			rules = append(rules, x)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(rules) > 0 {
			return &tunnelError{Code: "tunnel_in_use", Status: 409, Message: "隧道仍被规则使用", Details: map[string]any{"rules": rules}}
		}
	}
	if status == "pending_delete" {
		return nil
	}
	if published == nil {
		if err = releaseDraftClaims(ctx, tx, kind, objectID, nil); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id=?", objectID)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE "+table+" SET status='pending_delete',draft_version=draft_version+1,updated_at=? WHERE id=?", nextCanvasStamp(stamp), objectID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RotateTunnelCredentials(ctx context.Context, tunnelID, updatedAt string) (Tunnel, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	r := s.Repository
	t, err := r.Tunnel(ctx, tunnelID)
	if err != nil {
		return t, err
	}
	if err = editableTunnel(t.Status, t.UpdatedAt, updatedAt); err != nil {
		return t, err
	}
	generation := int64(1)
	if t.Credentials != nil {
		generation = t.Credentials.Generation + 1
	}
	material, err := newManagedGostMaterial(tunnelID, generation)
	if err != nil {
		return t, err
	}
	sealed, err := r.sealGostMaterial(tunnelID, material)
	if err != nil {
		return t, err
	}
	result, err := r.Database.ExecContext(ctx, `UPDATE tunnels SET gost_material=?,status=?,draft_version=draft_version+1,updated_at=? WHERE id=? AND updated_at=?`, sealed, managedStatus(t.PublishedAt), nextCanvasStamp(t.UpdatedAt), tunnelID, updatedAt)
	if err != nil {
		return t, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return t, err
	}
	if n == 0 {
		return t, tunnelFail("stale", 409, "数据已更新，请刷新后重试")
	}
	return r.Tunnel(ctx, tunnelID)
}
