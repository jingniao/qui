package proxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type canvasQueryRow interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// This helper runs against either a read-only database or the adoption
// transaction, keeping existence, compatibility and ownership assignment atomic.
func compatibleCanvasObject(ctx context.Context, q canvasQueryRow, t CanvasTunnel, o CanvasTunnelObject) error {
	source, target := strings.TrimPrefix(t.Source, "server:"), strings.TrimPrefix(t.Target, "server:")
	protocol := canvasTunnels[t.TunnelProtocol]
	if t.TunnelProtocol == "" {
		protocol = "trojan"
	}
	port := canvasDefaultPort(protocol)
	if t.TunnelPort != nil {
		port = *t.TunnelPort
	}
	implicit := t.TunnelPort == nil
	var node, actualProtocol, actualTarget string
	var actualPort, relay int
	var err error
	switch o.Kind {
	case "inbound":
		err = q.QueryRowContext(ctx, "SELECT node_id,protocol,port,internal_relay FROM inbounds WHERE id=?", o.ID).Scan(&node, &actualProtocol, &actualPort, &relay)
		if t.Type == "port_forward" {
			protocol = "forward"
			target = source
			implicit = false
			if t.ListenPort == nil {
				return errors.New("tunnel has no listen port")
			}
			port = *t.ListenPort
		}
		if err == nil && node == target && actualProtocol == protocol && (actualPort == port || implicit) && (relay == 1 || protocol == "forward") {
			return nil
		}
	case "outbound":
		err = q.QueryRowContext(ctx, "SELECT b.node_id,b.protocol,i.node_id,i.port FROM outbounds b JOIN inbounds i ON i.id=b.target_inbound_id AND i.protocol=b.protocol WHERE b.id=?", o.ID).Scan(&node, &actualProtocol, &actualTarget, &actualPort)
		if err == nil && t.Type != "port_forward" && node == source && actualTarget == target && actualProtocol == protocol && (actualPort == port || implicit) {
			return nil
		}
	case "chain":
		err = q.QueryRowContext(ctx, `SELECT json_extract(c.node_ids_json,'$[0]'),json_extract(c.node_ids_json,'$[1]'),b.protocol,i.port FROM proxy_chains c JOIN proxy_chain_hops h ON h.chain_id=c.id AND h.position=(SELECT min(h2.position) FROM proxy_chain_hops h2 WHERE h2.chain_id=c.id) JOIN outbounds b ON b.id=h.outbound_id JOIN inbounds i ON i.id=b.target_inbound_id AND i.protocol=b.protocol WHERE c.id=? AND b.node_id=json_extract(c.node_ids_json,'$[0]') AND i.node_id=json_extract(c.node_ids_json,'$[1]')`, o.ID).Scan(&node, &actualTarget, &actualProtocol, &actualPort)
		if err == nil && t.Type != "port_forward" && node == source && actualTarget == target && actualProtocol == protocol && (actualPort == port || implicit) {
			return nil
		}
	default:
		return errors.New("invalid object kind")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("object %s is incompatible with tunnel %s", o.ID, t.ID)
}
