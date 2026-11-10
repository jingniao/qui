package proxy

import (
	"context"
	"database/sql"
	"errors"

	"proxycontrol/pkg/contracts/desiredstate"
)

type PortOwner struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	inbound bool
}
type PortCheck struct {
	NodeID string     `json:"nodeId"`
	Port   int        `json:"port"`
	Free   bool       `json:"free"`
	UsedBy *PortOwner `json:"usedBy,omitempty"`
}

type tunnelError struct {
	Code    string
	Status  int
	Message string
	Details map[string]any
}

func (e *tunnelError) Error() string { return e.Message }
func tunnelFail(code string, status int, message string) error {
	return &tunnelError{Code: code, Status: status, Message: message}
}

type portDatabase interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// portOccupants is the single source of numeric listener reservations. Exclusions
// are identities, never names; callers may add listeners from an unsaved plan.
func (r *Repository) portOccupants(ctx context.Context, q portDatabase, nodeID string, exclude []PortOwner, inboundMode ...string) (map[int][]PortOwner, error) {
	excluded := func(o PortOwner) bool {
		for _, e := range exclude {
			if o.Kind == e.Kind && o.ID == e.ID {
				return true
			}
		}
		return false
	}
	result := map[int][]PortOwner{}
	mode := "all"
	if len(inboundMode) > 0 {
		mode = inboundMode[0]
	}
	// Allocation and ports/check include disabled inbounds for UI warnings.
	// Manual saves may reuse a disabled inbound's port; claims always reserve.
	rows, err := q.QueryContext(ctx, `SELECT i.id,i.name,i.port,i.encrypted_settings,coalesce(o.tunnel_id,''),coalesce(m.owner_kind,''),coalesce(m.owner_id,'') FROM inbounds i LEFT JOIN canvas_tunnel_objects o ON o.object_kind='inbound' AND o.object_id=i.id LEFT JOIN tunnel_objects m ON m.object_kind='inbound' AND m.object_id=i.id WHERE i.node_id=? AND (?='all' OR (?='enabled' AND i.enabled=1)) ORDER BY i.id`, nodeID, mode, mode)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o PortOwner
		var port int
		var sealed []byte
		var canvas string
		var managedKind, managedID string
		if err = rows.Scan(&o.ID, &o.Name, &port, &sealed, &canvas, &managedKind, &managedID); err != nil {
			rows.Close()
			return nil, err
		}
		o.Kind = "inbound"
		o.inbound = true
		if excluded(o) {
			continue
		}
		// Managed listeners are represented by their persistent claims, once.
		if managedID != "" {
			continue
		}
		settings, e := r.decode(sealed)
		if e != nil {
			rows.Close()
			return nil, e
		}
		if canvas != "" {
			o.Kind = "canvas"
		}
		if excluded(o) {
			continue
		}
		for _, p := range desiredstate.ClaimedPorts(port, settings) {
			result[p] = append(result[p], o)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT c.port,c.owner_kind,c.owner_id,coalesce(t.name,r.name,a.name,c.owner_id) FROM port_claims c LEFT JOIN tunnels t ON c.owner_kind='tunnel_hop' AND t.id=c.owner_id LEFT JOIN tunnel_rules r ON c.owner_kind='rule' AND r.id=c.owner_id LEFT JOIN canvas_tunnels a ON c.owner_kind='canvas' AND a.id=c.owner_id WHERE c.node_id=? ORDER BY c.port`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p int
		var o PortOwner
		if err = rows.Scan(&p, &o.Kind, &o.ID, &o.Name); err != nil {
			return nil, err
		}
		if o.Kind == "tunnel_hop" {
			o.Kind = "tunnel"
		}
		if !excluded(o) {
			result[p] = append(result[p], o)
		}
	}
	return result, rows.Err()
}

func checkPort(nodeID string, port int, occupants map[int][]PortOwner) PortCheck {
	c := PortCheck{NodeID: nodeID, Port: port, Free: true}
	if used := occupants[port]; len(used) > 0 {
		c.Free = false
		o := used[0]
		c.UsedBy = &o
	}
	return c
}
func (r *Repository) CheckPort(ctx context.Context, nodeID string, port int, owner *PortOwner) (PortCheck, error) {
	if port < 1 || port > 65535 {
		return PortCheck{}, tunnelFail("port_out_of_range", 400, "端口必须在 1–65535 范围内")
	}
	if err := portNode(ctx, r.Database, nodeID); err != nil {
		return PortCheck{}, err
	}
	var exclude []PortOwner
	if owner != nil {
		exclude = append(exclude, *owner)
	}
	occupied, err := r.portOccupants(ctx, r.Database, nodeID, exclude)
	return checkPort(nodeID, port, occupied), err
}
func portNode(ctx context.Context, q portDatabase, nodeID string) error {
	var id string
	err := q.QueryRowContext(ctx, "SELECT id FROM nodes WHERE id=? AND coalesce(deleted_at,'')=''", nodeID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func allocatePorts(ctx context.Context, q portDatabase, nodeID string, count int, occupied map[int][]PortOwner, exclude map[int]bool) ([]int, error) {
	if count < 1 {
		return nil, tunnelFail("invalid", 400, "分配数量必须为正整数")
	}
	var start, end sql.NullInt64
	err := q.QueryRowContext(ctx, "SELECT port_range_start,port_range_end FROM node_meta WHERE node_id=?", nodeID).Scan(&start, &end)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	ports := []int{}
	if start.Valid && end.Valid {
		for p := int(start.Int64); p <= int(end.Int64) && len(ports) < count; p++ {
			if len(occupied[p]) == 0 && !exclude[p] {
				ports = append(ports, p)
			}
		}
	}
	if len(ports) != count {
		return nil, tunnelFail("no_free_port", 409, "服务器未设置端口范围或范围内没有可用端口")
	}
	return ports, nil
}
func (r *Repository) AllocatePorts(ctx context.Context, nodeID string, count int, exclude map[int]bool) ([]int, error) {
	if err := portNode(ctx, r.Database, nodeID); err != nil {
		return nil, err
	}
	occupied, err := r.portOccupants(ctx, r.Database, nodeID, nil)
	if err != nil {
		return nil, err
	}
	return allocatePorts(ctx, r.Database, nodeID, count, occupied, exclude)
}
