package proxy

import (
	"bytes"
	"context"
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
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	Port   int    `json:"port"`
}
type CanvasTunnel struct {
	CanvasTunnelInput
	ID               string                   `json:"id"`
	CreatedAt        string                   `json:"createdAt"`
	Schemes          []CanvasSchemeRef        `json:"schemes"`
	Objects          []CanvasTunnelObject     `json:"objects"`
	PublishStatus    string                   `json:"publishStatus"`
	Credentials      *CanvasTunnelCredentials `json:"credentials"`
	connection       CanvasConnection
	prefix           string
	inlineScheme     string
	publishedVersion string
	allocatedPort    *int
	sealedMaterial   []byte
	material         *gostMaterial
}

// CanvasTunnelCredentials is the redacted view of a GOST tunnel's credential set:
// the generation, its expiry and the certificate fingerprint. The certificates,
// the keys and the CA are never part of an API response, the plan or the log.
type CanvasTunnelCredentials struct {
	Generation  int64  `json:"generation"`
	ServerName  string `json:"serverName"`
	NotAfter    string `json:"notAfter"`
	Fingerprint string `json:"fingerprint"`
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
	err := row.Scan(&t.ID, &name, &raw, &source, &target, &t.inlineScheme, &t.prefix, &t.CreatedAt, &stamp, &t.publishedVersion, &t.allocatedPort, &t.sealedMaterial)
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

const tunnelColumns = "id,name,connection_json,source_ref,target_ref,inline_scheme_id,object_prefix,created_at,updated_at,published_version,allocated_port,gost_material"

func (r *Repository) canvasTunnel(ctx context.Context, tunnelID string) (CanvasTunnel, error) {
	t, err := scanTunnel(r.Database.QueryRowContext(ctx, "SELECT "+tunnelColumns+" FROM canvas_tunnels WHERE id=?", tunnelID))
	if err != nil {
		return t, err
	}
	r.attachCredentials(&t)
	return t, nil
}

// attachCredentials resolves the redacted credential view of a tunnel. A set that
// cannot be opened is left out instead of failing the read: the Owner still sees
// the tunnel and can rotate it, while a publication refuses it.
func (r *Repository) attachCredentials(t *CanvasTunnel) {
	t.Credentials, t.material = nil, nil
	if len(t.sealedMaterial) == 0 {
		return
	}
	material, err := r.openGostMaterial(t.ID, t.sealedMaterial)
	if err != nil {
		return
	}
	t.material = &material
	t.Credentials = &CanvasTunnelCredentials{Generation: material.Generation, ServerName: material.ServerName, NotAfter: material.NotAfter, Fingerprint: material.Fingerprint}
}

const canvasTunnelSchemesQuery = `SELECT DISTINCT s.id,s.name FROM canvas_schemes s,json_each(s.layout_json,'$.connections') c WHERE s.execution='server' AND json_extract(c.value,'$.tunnelId')=? ORDER BY s.id`

func (r *Repository) tunnelSchemes(ctx context.Context, tunnelID string) ([]CanvasSchemeRef, error) {
	rows, err := r.Database.QueryContext(ctx, canvasTunnelSchemesQuery, tunnelID)
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
	return r.canvasTunnelsReadOnly(ctx)
}

func (r *Repository) canvasTunnelsReadOnly(ctx context.Context) ([]CanvasTunnel, error) {
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
		r.attachCredentials(t)
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
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > canvasMaxNameBytes {
		return CanvasTunnel{}, errors.New("tunnel name must be 1 to 80 bytes")
	}
	c := input.connectionValue("edge")
	if c.TunnelProtocol == "gost" && c.Type != "port_forward" {
		return CanvasTunnel{}, errors.New("gost tunnels carry port forwards only")
	}
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
	if err := s.Repository.syncGostMaterial(ctx, tx, tunnelID, c.TunnelProtocol == "gost" && c.Type == "port_forward", stamp); err != nil {
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
func (r *Repository) saveInlineTunnels(ctx context.Context, tx *sql.Tx, schemeID string, input *CanvasSchemeInput) error {
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
		// A GOST tunnel gets its credential set when it is created, exactly like
		// the explicit save path, so a publication never has to issue one.
		if err := r.syncGostMaterial(ctx, tx, tunnelID, stored.Type == "port_forward" && stored.TunnelProtocol == "gost", stamp); err != nil {
			return err
		}
		c.TunnelID = tunnelID
		input.Layout.Connections[n] = c
	}
	return nil
}
