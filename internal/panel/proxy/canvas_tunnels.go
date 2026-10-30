package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"proxycontrol/internal/platform/id"
)

// CanvasTunnel endpoints are instance refs, never drawing-local instance IDs.
// The connection's ID is retained internally as the legacy forward-name seed.
type CanvasTunnelInput struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Target         string `json:"target"`
	Type           string `json:"type"`
	TunnelProtocol string `json:"tunnelProtocol,omitempty"`
	TunnelPort     *int   `json:"tunnelPort,omitempty"`
	ListenPort     *int   `json:"listenPort,omitempty"`
	TargetHost     string `json:"targetHost,omitempty"`
	TargetPort     *int   `json:"targetPort,omitempty"`
	SupportTCP     bool   `json:"supportTcp"`
	SupportUDP     bool   `json:"supportUdp"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
}
type CanvasSchemeRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type CanvasTunnelObject struct {
	Kind             string   `json:"kind"`
	ID               string   `json:"id"`
	NodeID           string   `json:"nodeId"`
	Name             string   `json:"name"`
	Port             int      `json:"port"`
	CandidateTunnels []string `json:"candidateTunnels,omitempty"`
}
type CanvasTunnel struct {
	CanvasTunnelInput
	ID               string               `json:"id"`
	CreatedAt        string               `json:"createdAt"`
	Schemes          []CanvasSchemeRef    `json:"schemes"`
	Objects          []CanvasTunnelObject `json:"objects"`
	PublishStatus    string               `json:"publishStatus"`
	connection       CanvasConnection
	prefix           string
	inlineScheme     string
	publishedVersion string
	allocatedPort    *int
}

func tunnelInput(c CanvasConnection, name, source, target, stamp string) CanvasTunnelInput {
	return CanvasTunnelInput{Name: name, Source: source, Target: target, Type: c.Type, TunnelProtocol: c.TunnelProtocol, TunnelPort: c.TunnelPort, ListenPort: c.ListenPort, TargetHost: c.TargetHost, TargetPort: c.TargetPort, SupportTCP: c.SupportTCP, SupportUDP: c.SupportUDP, UpdatedAt: stamp}
}
func (i CanvasTunnelInput) connectionValue(seed string) CanvasConnection {
	return CanvasConnection{ID: seed, Source: "source", Target: "target", Type: i.Type, TunnelProtocol: i.TunnelProtocol, TunnelPort: i.TunnelPort, ListenPort: i.ListenPort, TargetHost: i.TargetHost, TargetPort: i.TargetPort, SupportTCP: i.SupportTCP, SupportUDP: i.SupportUDP}
}
func scanTunnel(row interface{ Scan(...any) error }) (CanvasTunnel, error) {
	var t CanvasTunnel
	var raw, source, target, stamp, name string
	err := row.Scan(&t.ID, &name, &raw, &source, &target, &t.inlineScheme, &t.prefix, &t.CreatedAt, &stamp, &t.publishedVersion, &t.allocatedPort)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&t.connection); err != nil {
		return t, errCanvasLayout
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return t, errCanvasLayout
	}
	t.CanvasTunnelInput = tunnelInput(t.connection, name, source, target, stamp)
	t.Schemes = []CanvasSchemeRef{}
	t.Objects = []CanvasTunnelObject{}
	t.PublishStatus = "draft"
	if t.publishedVersion == stamp {
		t.PublishStatus = "published"
	} else if t.publishedVersion != "" {
		t.PublishStatus = "modified"
	}
	return t, nil
}

const tunnelColumns = "id,name,connection_json,source_ref,target_ref,inline_scheme_id,object_prefix,created_at,updated_at,published_version,allocated_port"

func (r *Repository) canvasTunnel(ctx context.Context, tunnelID string) (CanvasTunnel, error) {
	return scanTunnel(r.Database.QueryRowContext(ctx, "SELECT "+tunnelColumns+" FROM canvas_tunnels WHERE id=?", tunnelID))
}

func (r *Repository) tunnelSchemes(ctx context.Context, tunnelID string) ([]CanvasSchemeRef, error) {
	rows, err := r.Database.QueryContext(ctx, `SELECT DISTINCT s.id,s.name FROM canvas_schemes s,json_each(s.layout_json,'$.connections') c WHERE s.execution='server' AND json_extract(c.value,'$.tunnelId')=? ORDER BY s.id`, tunnelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CanvasSchemeRef{}
	for rows.Next() {
		var item CanvasSchemeRef
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) CanvasTunnels(ctx context.Context) ([]CanvasTunnel, error) {
	if err := r.ReconcileCanvasTunnels(ctx); err != nil {
		return nil, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT "+tunnelColumns+" FROM canvas_tunnels ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	items := []CanvasTunnel{}
	for rows.Next() {
		t, e := scanTunnel(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, t)
	}
	err = rows.Close()
	if err != nil {
		return nil, err
	}
	for n := range items {
		t := &items[n]
		t.Schemes, err = r.tunnelSchemes(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		t.Objects, err = r.tunnelObjects(ctx, t.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) SaveCanvasTunnel(ctx context.Context, tunnelID string, input CanvasTunnelInput) (CanvasTunnel, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	if err := s.Repository.ReconcileCanvasTunnels(ctx); err != nil {
		return CanvasTunnel{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > canvasMaxNameBytes {
		return CanvasTunnel{}, errors.New("tunnel name must be 1 to 80 bytes")
	}
	c := input.connectionValue("edge")
	layout := CanvasLayout{Instances: []CanvasInstance{{InstanceID: "source", Ref: input.Source, Role: "entry"}, {InstanceID: "target", Ref: input.Target, Role: "exit"}}, Connections: []CanvasConnection{c}}
	if err := layout.validate(); err != nil {
		return CanvasTunnel{}, err
	}
	if err := s.validateRefs(ctx, layout); err != nil {
		return CanvasTunnel{}, err
	}
	for _, ref := range []string{input.Source, input.Target} {
		p, _ := parseCanvasRef(ref)
		if p.kind != "server" {
			return CanvasTunnel{}, errors.New("server tunnels require server refs")
		}
	}
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return CanvasTunnel{}, err
	}
	defer tx.Rollback()
	stamp := timestamp()
	if tunnelID == "" {
		tunnelID, err = id.New()
		if err != nil {
			return CanvasTunnel{}, err
		}
		c.ID = tunnelID
		raw, _ := json.Marshal(c)
		_, err = tx.ExecContext(ctx, "INSERT INTO canvas_tunnels(id,name,connection_json,source_ref,target_ref,object_prefix,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", tunnelID, input.Name, string(raw), input.Source, input.Target, canvasObjectPrefix("tunnel:"+tunnelID), stamp, stamp)
	} else {
		old, e := scanTunnel(tx.QueryRowContext(ctx, "SELECT "+tunnelColumns+" FROM canvas_tunnels WHERE id=?", tunnelID))
		if e != nil {
			return CanvasTunnel{}, e
		}
		if input.UpdatedAt != old.UpdatedAt {
			return CanvasTunnel{}, ErrStaleScheme
		}
		if stamp <= old.UpdatedAt {
			stamp = nextCanvasStamp(old.UpdatedAt)
		}
		if input.Source != old.Source || input.Target != old.Target {
			if err := retargetCanvasTunnel(ctx, tx, tunnelID, input.Source, input.Target); err != nil {
				return CanvasTunnel{}, err
			}
		}
		c.ID = old.connection.ID
		raw, _ := json.Marshal(c)
		_, err = tx.ExecContext(ctx, "UPDATE canvas_tunnels SET name=?,connection_json=?,source_ref=?,target_ref=?,updated_at=? WHERE id=?", input.Name, string(raw), input.Source, input.Target, stamp, tunnelID)
	}
	if err != nil {
		return CanvasTunnel{}, err
	}
	if err = tx.Commit(); err != nil {
		return CanvasTunnel{}, err
	}
	return s.Repository.canvasTunnel(ctx, tunnelID)
}

// Reference-only connections are hydrated from the resource on every read and
// plan. Inline edits can only update a tunnel created for this same scheme.
func (r *Repository) resolveCanvasLayout(ctx context.Context, execution string, layout CanvasLayout) (CanvasLayout, error) {
	layout = layout.normalized()
	if execution != "server" {
		for _, c := range layout.Connections {
			if c.TunnelID != "" {
				return layout, errors.New("client connections cannot reference server tunnels")
			}
		}
		return layout, nil
	}
	instances := map[string]string{}
	for _, i := range layout.Instances {
		instances[i.InstanceID] = i.Ref
	}
	layout.Connections = append([]CanvasConnection{}, layout.Connections...)
	for n, c := range layout.Connections {
		if c.TunnelID == "" {
			continue
		}
		t, err := r.canvasTunnel(ctx, c.TunnelID)
		if err != nil {
			return layout, err
		}
		if instances[c.Source] != t.Source || instances[c.Target] != t.Target {
			return layout, fmt.Errorf("tunnel %s endpoints do not match layout refs", c.TunnelID)
		}
		value := t.connection
		value.ID = c.ID
		value.Source = c.Source
		value.Target = c.Target
		value.TunnelID = c.TunnelID
		layout.Connections[n] = value
	}
	return layout, nil
}

// Convert inline compatibility requests inside the scheme transaction. Existing
// inline edges are identified by (scheme ID, connection ID); shared references
// never mutate their tunnel implicitly.
func saveInlineTunnels(ctx context.Context, tx *sql.Tx, schemeID string, input *CanvasSchemeInput) error {
	if input.Execution != "server" {
		return nil
	}
	refs := map[string]string{}
	instances := map[string]CanvasInstance{}
	for _, i := range input.Layout.Instances {
		refs[i.InstanceID] = i.Ref
		instances[i.InstanceID] = i
	}
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT layout_json FROM canvas_schemes WHERE id=?", schemeID).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	oldIDs := map[string]string{}
	if previous != "" {
		old, e := decodeCanvasLayout([]byte(previous))
		if e != nil {
			return e
		}
		for _, c := range old.Connections {
			oldIDs[c.ID] = c.TunnelID
		}
	}
	for n, c := range input.Layout.Connections {
		if c.TunnelID != "" {
			continue
		}
		tunnelID := oldIDs[c.ID]
		var owner, seed, prefix string
		if tunnelID != "" {
			var raw string
			if err := tx.QueryRowContext(ctx, "SELECT inline_scheme_id,connection_json,object_prefix FROM canvas_tunnels WHERE id=?", tunnelID).Scan(&owner, &raw, &prefix); err != nil {
				return err
			}
			var old CanvasConnection
			if err := json.Unmarshal([]byte(raw), &old); err != nil {
				return err
			}
			seed = old.ID
			if owner != schemeID {
				tunnelID = ""
			}
		}
		fresh := tunnelID == ""
		if fresh {
			var err error
			tunnelID, err = id.New()
			if err != nil {
				return err
			}
			seed = c.ID
			prefix = canvasObjectPrefix(input.Name)
		}
		stored := c
		stored.ID = seed
		stored.TunnelID = ""
		if stored.Type == "port_forward" {
			if stored.ListenPort == nil {
				stored.ListenPort = instances[c.Source].ListenPort
			}
			if stored.TargetPort == nil {
				stored.TargetPort = instances[c.Target].TargetPort
			}
		}
		raw, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		stamp := timestamp()
		if fresh {
			_, err = tx.ExecContext(ctx, "INSERT INTO canvas_tunnels(id,name,connection_json,source_ref,target_ref,inline_scheme_id,object_prefix,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)", tunnelID, tunnelID, string(raw), refs[c.Source], refs[c.Target], schemeID, prefix, stamp, stamp)
		} else {
			if err := retargetCanvasTunnel(ctx, tx, tunnelID, refs[c.Source], refs[c.Target], schemeID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "UPDATE canvas_tunnels SET connection_json=?,source_ref=?,target_ref=?,updated_at=CASE WHEN connection_json<>? OR source_ref<>? OR target_ref<>? THEN ? ELSE updated_at END WHERE id=?", string(raw), refs[c.Source], refs[c.Target], string(raw), refs[c.Source], refs[c.Target], stamp, tunnelID)
		}
		if err != nil {
			return err
		}
		c.TunnelID = tunnelID
		input.Layout.Connections[n] = c
	}
	return nil
}

// ReconcileCanvasTunnels is one-shot and transactional. Only pre-migration,
// published, exact current-name claims participate; duplicate claims adopt
// nothing. No future drawing can retroactively acquire a deleted legacy owner.
func (r *Repository) ReconcileCanvasTunnels(ctx context.Context) error {
	var done int
	if err := r.Database.QueryRowContext(ctx, "SELECT done FROM canvas_tunnel_reconcile WHERE id=1").Scan(&done); err != nil {
		return err
	}
	if done == 1 {
		return nil
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT done FROM canvas_tunnel_reconcile WHERE id=1").Scan(&done); err != nil {
		return err
	}
	if done == 1 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.id,t.connection_json,t.source_ref,t.target_ref,t.object_prefix FROM canvas_tunnels t JOIN canvas_schemes s ON s.id=t.inline_scheme_id WHERE s.published_at<>'' AND s.execution='server'`)
	if err != nil {
		return err
	}
	type legacy struct{ id, raw, source, target, prefix string }
	items := []legacy{}
	for rows.Next() {
		var x legacy
		if err := rows.Scan(&x.id, &x.raw, &x.source, &x.target, &x.prefix); err != nil {
			rows.Close()
			return err
		}
		items = append(items, x)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	type claim struct{ tunnel, kind, id string }
	claims := map[string][]claim{}
	for _, x := range items {
		var c CanvasConnection
		if err := json.Unmarshal([]byte(x.raw), &c); err != nil {
			return err
		}
		source := strings.TrimPrefix(x.source, "server:")
		target := strings.TrimPrefix(x.target, "server:")
		var a, b string
		if err := tx.QueryRowContext(ctx, "SELECT name FROM nodes WHERE id=?", source).Scan(&a); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT name FROM nodes WHERE id=?", target).Scan(&b); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		protocol := canvasTunnels[c.TunnelProtocol]
		if c.TunnelProtocol == "" {
			protocol = "trojan"
		}
		port := canvasDefaultPort(protocol)
		if c.TunnelPort != nil {
			port = *c.TunnelPort
		}
		if c.Type != "port_forward" && c.TunnelPort == nil {
			rs, e := tx.QueryContext(ctx, "SELECT name,port FROM inbounds WHERE node_id=? AND protocol=? AND internal_relay=1", target, protocol)
			if e != nil {
				return e
			}
			matched := []int{}
			for rs.Next() {
				var name string
				var candidate int
				if e := rs.Scan(&name, &candidate); e != nil {
					rs.Close()
					return e
				}
				expected, _ := canvasEdgeNames(x.prefix, a, b, protocol, candidate)
				if name == expected {
					matched = append(matched, candidate)
				}
			}
			if e := rs.Close(); e != nil {
				return e
			}
			if len(matched) > 1 {
				continue
			}
			if len(matched) == 1 {
				port = matched[0]
			}
		}
		in, out := canvasEdgeNames(x.prefix, a, b, protocol, port)
		specs := []struct{ kind, table, node, name string }{{"inbound", "inbounds", target, in}, {"outbound", "outbounds", source, out}}
		if c.Type == "port_forward" {
			hash := sha256.Sum256([]byte(c.ID + ":" + source + ":" + target))
			specs = specs[:1]
			specs[0].node = source
			specs[0].name = fmt.Sprintf("%sforward %x%s", x.prefix, hash[:8], canvasInboundSuffix)
		}
		for _, sp := range specs {
			query := "SELECT id FROM " + sp.table + " WHERE node_id=? AND name=? AND protocol=?"
			wantProtocol := protocol
			if c.Type == "port_forward" {
				wantProtocol = "forward"
			}
			args := []any{sp.node, sp.name, wantProtocol}
			if sp.kind == "inbound" {
				query += " AND (internal_relay=1 OR protocol='forward')"
				if c.Type != "port_forward" {
					query += " AND port=?"
					args = append(args, port)
				} else if c.ListenPort != nil {
					query += " AND port=?"
					args = append(args, *c.ListenPort)
				}
			} else {
				query += " AND target_inbound_id IS NOT NULL"
			}
			rs, e := tx.QueryContext(ctx, query, args...)
			if e != nil {
				return e
			}
			matched := []string{}
			for rs.Next() {
				var object string
				if e = rs.Scan(&object); e != nil {
					rs.Close()
					return e
				}
				matched = append(matched, object)
			}
			if e = rs.Close(); e != nil {
				return e
			}
			if len(matched) == 1 {
				object := matched[0]
				key := sp.kind + ":" + object
				claims[key] = append(claims[key], claim{x.id, sp.kind, object})
			}
		}
	}
	for _, cs := range claims {
		if len(cs) != 1 {
			continue
		}
		c := cs[0]
		if _, err = tx.ExecContext(ctx, "INSERT INTO canvas_tunnel_objects(tunnel_id,object_kind,object_id) VALUES(?,?,?)", c.tunnel, c.kind, c.id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE canvas_tunnels SET published_version=updated_at WHERE id IN (SELECT t.id FROM canvas_tunnels t JOIN canvas_schemes s ON s.id=t.inline_scheme_id WHERE s.published_at<>'' AND (SELECT count(*) FROM canvas_tunnel_objects o WHERE o.tunnel_id=t.id)=CASE WHEN json_extract(t.connection_json,'$.type')='port_forward' THEN 1 ELSE 2 END)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE canvas_tunnels SET allocated_port=(SELECT i.port FROM canvas_tunnel_objects o JOIN inbounds i ON i.id=o.object_id WHERE o.object_kind='inbound' AND o.tunnel_id=canvas_tunnels.id AND i.protocol<>'forward') WHERE published_version<>''`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE canvas_tunnel_reconcile SET done=1 WHERE id=1"); err != nil {
		return err
	}
	return tx.Commit()
}
