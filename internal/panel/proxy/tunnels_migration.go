package proxy

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"proxycontrol/pkg/contracts/desiredstate"
)

type TunnelMigrationItem struct {
	TunnelID string `json:"tunnelId"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
}

func (r *Repository) TunnelMigrationReport(ctx context.Context) ([]TunnelMigrationItem, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT tunnel_id,name,reason FROM tunnel_migration_report ORDER BY tunnel_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TunnelMigrationItem{}
	for rows.Next() {
		var item TunnelMigrationItem
		if err = rows.Scan(&item.TunnelID, &item.Name, &item.Reason); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type forwardMigration struct {
	legacy         CanvasTunnel
	source, target string
	entry, exit    Inbound
	destination    TunnelTarget
	network        string
}

// ReconcileCanvasForwards changes panel ownership only. Inbounds and deployed
// revisions are never written. The marker and unresolved inventory commit with
// all conversions, so startup and restore use the same one-shot operation.
func (r *Repository) ReconcileCanvasForwards(ctx context.Context) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done bool
	if err = tx.QueryRowContext(ctx, "SELECT done FROM canvas_forward_reconcile WHERE id=1").Scan(&done); err != nil {
		return err
	}
	if done {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+tunnelColumns+" FROM canvas_tunnels WHERE json_extract(connection_json,'$.type')='port_forward' ORDER BY id")
	if err != nil {
		return err
	}
	candidates := []CanvasTunnel{}
	for rows.Next() {
		t, e := scanTunnel(rows)
		if e != nil {
			rows.Close()
			return e
		}
		candidates = append(candidates, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, t := range candidates {
		m, reason, e := r.inspectForwardMigration(ctx, tx, t)
		if e != nil {
			return e
		}
		if reason != "" {
			if _, err = tx.ExecContext(ctx, "INSERT INTO tunnel_migration_report VALUES(?,?,?)", t.ID, t.Name, reason); err != nil {
				return err
			}
			continue
		}
		if err = r.applyForwardMigration(ctx, tx, m); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE canvas_forward_reconcile SET migrated_ids=json_insert(migrated_ids,'$[#]',?) WHERE id=1", t.ID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE canvas_forward_reconcile SET done=1 WHERE id=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) inspectForwardMigration(ctx context.Context, tx *sql.Tx, t CanvasTunnel) (forwardMigration, string, error) {
	m := forwardMigration{legacy: t}
	reject := func(reason string) (forwardMigration, string, error) { return m, reason, nil }
	source, e := parseCanvasRef(t.Source)
	if e != nil || source.kind != "server" {
		return reject("入口不是明确的服务器引用，需手动处理")
	}
	target, e := parseCanvasRef(t.Target)
	if e != nil || target.kind != "server" || source.id == target.id {
		return reject("目标不是独立服务器，需手动处理")
	}
	m.source, m.target = source.id, target.id
	for _, node := range []string{m.source, m.target} {
		if e = portNode(ctx, tx, node); errors.Is(e, ErrNotFound) {
			return reject("服务器不存在或已删除")
		}
		if e != nil {
			return m, "", e
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+canvasSchemeColumns+` FROM canvas_schemes WHERE EXISTS(SELECT 1 FROM json_each(layout_json,'$.connections') c WHERE json_extract(c.value,'$.tunnelId')=?)`, t.ID)
	if err != nil {
		return m, "", err
	}
	schemes := []CanvasScheme{}
	for rows.Next() {
		s, e := scanCanvasScheme(rows)
		if e != nil {
			rows.Close()
			return m, "", e
		}
		schemes = append(schemes, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return m, "", err
	}
	if len(schemes) == 0 {
		return reject("没有仍引用此连接的已发布服务器方案")
	}
	for _, s := range schemes {
		if s.Execution != "server" || s.PublishedAt == "" {
			return reject("引用此连接的方案尚未发布")
		}
		refs := map[string]string{}
		for _, i := range s.Layout.Instances {
			refs[i.InstanceID] = i.Ref
		}
		for _, c := range s.Layout.Connections {
			if c.TunnelID == t.ID && (refs[c.Source] != t.Source || refs[c.Target] != t.Target) {
				return reject("共享连接在多个方案中的路径不一致")
			}
		}
		var published int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM canvas_tunnel_publications WHERE scheme_id=? AND tunnel_id=?", s.ID, t.ID).Scan(&published); err != nil {
			return m, "", err
		}
		if published != 1 {
			return reject("方案尚未发布此连接")
		}
	}
	if t.publishedVersion == "" || t.publishedVersion != t.UpdatedAt {
		return reject("连接存在尚未发布的更改")
	}
	if !managedName(t.Name) {
		return reject("连接名称不符合隧道管理要求")
	}
	rows, err = tx.QueryContext(ctx, "SELECT object_kind,object_id FROM canvas_tunnel_objects WHERE tunnel_id=?", t.ID)
	if err != nil {
		return m, "", err
	}
	ids := []string{}
	for rows.Next() {
		var kind, objectID string
		if err = rows.Scan(&kind, &objectID); err != nil {
			rows.Close()
			return m, "", err
		}
		if kind != "inbound" {
			rows.Close()
			return reject("生成对象包含非入站对象")
		}
		ids = append(ids, objectID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return m, "", err
	}
	objects := []Inbound{}
	for _, objectID := range ids {
		in, e := r.storedInbound(ctx, tx, objectID)
		if errors.Is(e, ErrNotFound) {
			return reject("生成入站已不存在")
		}
		if e != nil {
			return m, "", e
		}
		objects = append(objects, in)
	}
	want := 1
	if t.gostRelay() {
		want = 2
	}
	if len(objects) != want {
		return reject("生成入站未完整归属于此画布连接")
	}
	for _, in := range objects {
		if in.Protocol != "forward" || !in.Enabled || in.SubscriptionVisible || len(in.AllowedUserIDs) != 0 || in.DefaultOutbound != "direct" || in.Settings.Forward == nil {
			return reject("生成入站已被修改或不适合自动迁移")
		}
		if in.NodeID == m.source && !in.Settings.Forward.RelayMode() {
			m.entry = in
		} else if in.NodeID == m.target && in.Settings.Forward.RelayMode() {
			m.exit = in
		} else {
			return reject("生成入站与连接路径不一致")
		}
		occupied, e := r.portOccupants(ctx, tx, in.NodeID, []PortOwner{{Kind: "inbound", ID: in.ID}}, "all")
		if e != nil {
			return m, "", e
		}
		// A canvas claim of this very connection may be transferred.
		for _, owner := range occupied[in.Port] {
			if owner.Kind != "canvas" || owner.ID != t.ID {
				return reject("原监听端口与其他入站或端口声明冲突")
			}
		}
		var owned int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM tunnel_objects WHERE object_id=?", in.ID).Scan(&owned); e != nil {
			return m, "", e
		}
		if owned != 0 {
			return reject("生成入站已归属于其他隧道或规则")
		}
	}
	if m.entry.ID == "" || (t.gostRelay() && m.exit.ID == "") {
		return reject("缺少入口转发或出口中继入站")
	}
	f := m.entry.Settings.Forward
	if !t.SupportTCP && !t.SupportUDP {
		return reject("连接没有明确的 TCP 或 UDP 网络")
	}
	if len(f.Targets) > 0 || f.PortEnd != nil {
		return reject("目标池或端口范围需手动迁移")
	}
	m.network = "tcp"
	if t.SupportUDP {
		m.network = "udp"
		if t.SupportTCP {
			m.network = "both"
		}
	}
	network := m.network
	if network == "both" {
		network = "tcp,udp"
	}
	if f.Network != network || !validForwardHost(f.Host) || f.Port < 1 || f.Port > 65535 || (t.TargetHost != "" && t.TargetHost != f.Host) || (t.TargetPort != nil && *t.TargetPort != f.Port) {
		return reject("连接目标或网络与生成入站不一致")
	}
	if t.gostRelay() {
		material, e := r.openGostMaterial(t.ID, t.sealedMaterial)
		if e != nil {
			return reject("GOST 密封凭据不可用，需手动处理")
		}
		var host string
		if e = tx.QueryRowContext(ctx, "SELECT public_ip FROM nodes WHERE id=?", m.target).Scan(&host); e != nil {
			return m, "", e
		}
		relay := canvasRelay{port: m.exit.Port, material: material}
		expected := relay.relayInput(host).Settings.Forward
		if host == "" || !reflect.DeepEqual(expected, m.exit.Settings.Forward) || !reflect.DeepEqual(f.Hops, []desiredstate.GostHop{relay.hopFor(host)}) {
			return reject("GOST 运行对象与密封凭据或出口地址不一致")
		}
		var collision int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM tunnels WHERE id=? OR name=?", t.ID, t.Name).Scan(&collision); e != nil {
			return m, "", e
		}
		if collision != 0 {
			return reject("隧道名称或标识已存在")
		}
	} else if len(f.Hops) != 0 {
		return reject("普通转发包含未识别的中继路径")
	}
	var extraClaims int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM port_claims WHERE owner_kind='canvas' AND owner_id=? AND NOT ((node_id=? AND port=?) OR (node_id=? AND port=?))`, t.ID, m.source, m.entry.Port, m.target, m.exit.Port).Scan(&extraClaims); err != nil {
		return m, "", err
	}
	if extraClaims != 0 {
		return reject("画布端口声明与生成对象不一致")
	}
	var collision int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM tunnel_rules WHERE id=?", t.ID).Scan(&collision); err != nil {
		return m, "", err
	}
	if collision != 0 {
		return reject("规则标识已存在")
	}
	m.destination = TunnelTarget{Mode: "manual", Host: f.Host, Port: f.Port}
	// Only the successfully running revision can establish a published target.
	var public string
	if err = tx.QueryRowContext(ctx, "SELECT public_ip FROM nodes WHERE id=?", m.target).Scan(&public); err != nil {
		return m, "", err
	}
	if f.Host == public {
		var sealed []byte
		err = tx.QueryRowContext(ctx, `SELECT c.encrypted_state FROM node_runtime_state n JOIN config_revisions c ON c.id=n.revision WHERE n.node_id=?`, m.target).Scan(&sealed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return m, "", err
		}
		if err == nil {
			data, e := r.Vault.Open(sealed)
			if e != nil {
				return m, "", e
			}
			state, e := desiredstate.Decode(data)
			if e != nil {
				return m, "", e
			}
			matches := []string{}
			for _, in := range state.Inbounds {
				if in.Port == f.Port && in.Enabled && in.Protocol != "forward" {
					var count int
					if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM inbounds WHERE id=? AND node_id=? AND port=? AND protocol<>'forward' AND internal_relay=0`, in.ID, m.target, f.Port).Scan(&count); e != nil {
						return m, "", e
					}
					if count == 1 {
						matches = append(matches, in.ID)
					}
				}
			}
			if state.NodeID == m.target && len(matches) == 1 {
				m.destination = TunnelTarget{Mode: "linked", InboundID: matches[0]}
			}
		}
	}
	return m, "", nil
}

func (r *Repository) applyForwardMigration(ctx context.Context, tx *sql.Tx, m forwardMigration) error {
	t := m.legacy
	stamp := timestamp()
	var tunnelID *string
	if t.gostRelay() {
		tunnelID = &t.ID
		if _, err := tx.ExecContext(ctx, `INSERT INTO tunnels(id,name,transport,status,published_version,published_at,gost_material,created_at,updated_at) VALUES(?,?,'tls','published',1,?,?,?,?)`, t.ID, t.Name, stamp, t.sealedMaterial, t.CreatedAt, stamp); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tunnel_hops VALUES(?,'entry',0,?,NULL,'manual',''),(?,'exit',1,?,?,'manual','')`, t.ID, m.source, t.ID, m.target, m.exit.Port); err != nil {
			return err
		}
	}
	var inbound, host any
	var port any
	if m.destination.Mode == "linked" {
		inbound = m.destination.InboundID
	} else {
		host = m.destination.Host
		port = m.destination.Port
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tunnel_rules(id,name,entry_node_id,listen_port,port_mode,network,tunnel_id,target_mode,target_inbound_id,target_host,target_port,subscription_visible,enabled,status,published_version,published_at,created_at,updated_at) VALUES(?,?,?,?,'manual',?,?,?,?,?,?,0,1,'published',1,?,?,?)`, t.ID, t.Name, m.source, m.entry.Port, m.network, tunnelID, m.destination.Mode, inbound, host, port, stamp, t.CreatedAt, stamp); err != nil {
		return err
	}
	for _, in := range []Inbound{m.entry, m.exit} {
		if in.ID == "" {
			continue
		}
		kind, claim := "rule", "rule"
		if in.ID == m.exit.ID {
			kind, claim = "tunnel", "tunnel_hop"
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM port_claims WHERE node_id=? AND port=? AND owner_kind='canvas' AND owner_id=?", in.NodeID, in.Port, t.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO port_claims VALUES(?,?,?,?)", in.NodeID, in.Port, claim, t.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO tunnel_objects VALUES('inbound',?,?,?)", in.ID, kind, t.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM canvas_tunnel_objects WHERE tunnel_id=?", t.ID); err != nil {
		return err
	}
	// JSON removal retains all other layout fields and every unrelated edge.
	for _, column := range []string{"layout_json", "client_published_layout"} {
		query := `UPDATE canvas_schemes SET ` + column + `=json_set(` + column + `,'$.connections',json(coalesce((SELECT json_group_array(json(c.value)) FROM json_each(` + column + `,'$.connections') c WHERE coalesce(json_extract(c.value,'$.tunnelId'),'')<>?),'[]'))) WHERE execution='server' AND ` + column + ` IS NOT NULL AND ` + column + `<>''`
		if _, err := tx.ExecContext(ctx, query, t.ID); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM canvas_tunnels WHERE id=?", t.ID)
	return err
}

// migrationOwnerCurrent permits the first republish to keep the exact legacy
// materialization, including names/core/listen and a public rather than loopback
// target. Any draft edit or destination invalidation rebuilds normal inputs.
func migrationOwnerCurrent(p tunnelPublication, owner PortOwner) bool {
	for _, t := range p.tunnels {
		if owner.Kind == "tunnel" && owner.ID == t.ID {
			return t.Status == "published" && t.DraftVersion == t.PublishedVersion
		}
	}
	for _, r := range p.rules {
		if owner.Kind == "rule" && owner.ID == r.ID {
			if r.Status != "published" || r.DraftVersion != r.PublishedVersion {
				return false
			}
			if r.TunnelID != nil {
				return migrationOwnerCurrent(p, PortOwner{Kind: "tunnel", ID: *r.TunnelID})
			}
			return true
		}
	}
	return false
}
