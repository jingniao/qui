package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"proxycontrol/internal/panel/node"
)

// validateCanvasUserCores gates both saved drawings and plans. Legacy GOST
// port-forward drawings retain their tunnel semantics.
func (s *Service) validateCanvasUserCores(ctx context.Context, layout CanvasLayout) error {
	requires := canvasUserCoreInstances(layout)
	for _, instance := range layout.Instances {
		if !requires[instance.InstanceID] {
			continue
		}
		nodeID, err := canvasRefNode(ctx, s.Repository.Database, instance.Ref)
		if err != nil {
			return err
		}
		if nodeID == "" {
			continue
		}
		n, err := s.Nodes.Get(ctx, nodeID)
		if err != nil {
			return err
		}
		if node.CoreOptional(n.Capabilities) {
			return node.ErrNoUserCore
		}
	}
	return nil
}

func canvasUserCoreInstances(layout CanvasLayout) map[string]bool {
	requires, gost := map[string]bool{}, map[string]bool{}
	for _, c := range layout.Connections {
		if c.Type == "port_forward" && c.TunnelProtocol == "gost" {
			gost[c.Source], gost[c.Target] = true, true
		} else {
			requires[c.Source], requires[c.Target] = true, true
		}
	}
	for _, instance := range layout.Instances {
		if !gost[instance.InstanceID] || instance.EntryBinding != nil {
			requires[instance.InstanceID] = true
		}
	}
	return requires
}

type canvasNodeReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func canvasRefNode(ctx context.Context, db canvasNodeReader, ref string) (string, error) {
	kind, id, _ := strings.Cut(ref, ":")
	if kind == "server" {
		return id, nil
	}
	var query string
	switch kind {
	case "inbound":
		query = "SELECT node_id FROM inbounds WHERE id=?"
	case "outbound":
		query = "SELECT node_id FROM outbounds WHERE id=?"
	default:
		return "", nil
	}
	var nodeID string
	err := db.QueryRowContext(ctx, query, id).Scan(&nodeID)
	return nodeID, err
}

// UserCoreCanvasDependenciesTx includes drafts, disabled resources and shared
// relay definitions even when no generated proxy objects exist yet.
func UserCoreCanvasDependenciesTx(ctx context.Context, tx *sql.Tx, nodeID string) ([]string, []string, error) {
	relays, schemes := []string{}, []string{}
	rows, err := tx.QueryContext(ctx, "SELECT id,connection_json,source_ref,target_ref FROM canvas_tunnels ORDER BY id")
	if err != nil {
		return nil, nil, err
	}
	type relay struct{ id, source, target string }
	definitions := []relay{}
	connections := map[string]CanvasConnection{}
	for rows.Next() {
		var item relay
		var raw []byte
		if err := rows.Scan(&item.id, &raw, &item.source, &item.target); err != nil {
			rows.Close()
			return nil, nil, err
		}
		var c CanvasConnection
		if err := json.Unmarshal(raw, &c); err != nil {
			rows.Close()
			return nil, nil, err
		}
		connections[item.id] = c
		if c.Type != "port_forward" || c.TunnelProtocol != "gost" {
			definitions = append(definitions, item)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	for _, item := range definitions {
		for _, ref := range []string{item.source, item.target} {
			owner, err := canvasRefNode(ctx, tx, ref)
			if err != nil {
				return nil, nil, err
			}
			if owner == nodeID {
				relays = append(relays, item.id)
				break
			}
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,layout_json FROM canvas_schemes WHERE execution='server' ORDER BY id")
	if err != nil {
		return nil, nil, err
	}
	type drawing struct {
		id     string
		layout CanvasLayout
	}
	drawings := []drawing{}
	for rows.Next() {
		var item drawing
		var raw []byte
		if err := rows.Scan(&item.id, &raw); err != nil {
			rows.Close()
			return nil, nil, err
		}
		item.layout, err = decodeCanvasLayout(raw)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		drawings = append(drawings, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	for _, item := range drawings {
		for i, c := range item.layout.Connections {
			if c.TunnelID != "" {
				value, ok := connections[c.TunnelID]
				if !ok {
					return nil, nil, ErrNotFound
				}
				value.Source, value.Target = c.Source, c.Target
				item.layout.Connections[i] = value
			}
		}
		requires := canvasUserCoreInstances(item.layout)
		for _, instance := range item.layout.Instances {
			if !requires[instance.InstanceID] {
				continue
			}
			owner, err := canvasRefNode(ctx, tx, instance.Ref)
			if err != nil {
				return nil, nil, err
			}
			if owner == nodeID {
				schemes = append(schemes, item.id)
				break
			}
		}
	}
	return relays, schemes, nil
}
