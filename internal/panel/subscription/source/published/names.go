package published

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"sort"
	"time"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

func endpointName(nodeName, protocol string) string {
	return nodeName + " · " + protocol
}

// EndpointNameStore reserves names of subscription-visible self-hosted inbounds.
type EndpointNameStore struct {
	Vault         *vault.Vault
	CanvasEntries CanvasEntries
}

// EndpointNames reads drafts and running revisions in the rename transaction.
// Canvas metadata uses its injected reader. A subscriber may have only one
// inbound of a protocol, so both base and disambiguated names matter.
func (s EndpointNameStore) EndpointNames(ctx context.Context, tx *sql.Tx) ([]string, error) {
	var canvasNames map[string]string
	if s.CanvasEntries != nil {
		var err error
		canvasNames, err = s.CanvasEntries.CanvasEntryNames(ctx)
		if err != nil {
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT n.id,n.name,i.id,i.protocol,i.port,i.listen,i.enabled
		FROM inbounds i JOIN nodes n ON n.id=i.node_id
		WHERE i.subscription_visible=1 AND i.internal_relay=0 AND n.deleted_at IS NULL
		ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	endpoints := []proxy.Proxy{}
	names := map[string]bool{}
	visible := map[string]string{}
	for rows.Next() {
		var nodeID, nodeName, inboundID, protocol, listen string
		var port int
		var enabled bool
		if err := rows.Scan(&nodeID, &nodeName, &inboundID, &protocol, &port, &listen, &enabled); err != nil {
			return nil, err
		}
		visible[inboundID] = nodeID
		if !enabled || protocol == "forward" {
			continue
		}
		if address := net.ParseIP(listen); address == nil || address.IsLoopback() {
			continue
		}
		name := endpointName(nodeName, protocol)
		if createdName, created := canvasNames[inboundID]; created {
			name = createdName
		}
		endpoints = append(endpoints, proxy.Proxy{Name: name, Port: port})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	reserveEndpointNames(names, endpoints)
	relays, err := (&RelayStore{Database: tx}).Relays(ctx)
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT n.id,n.name,c.encrypted_state FROM nodes n
		JOIN node_runtime_state r ON r.node_id=n.id JOIN config_revisions c ON c.id=r.revision
		WHERE n.deleted_at IS NULL ORDER BY n.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	endpoints = nil
	for rows.Next() {
		var nodeID, nodeName string
		var sealed []byte
		if err := rows.Scan(&nodeID, &nodeName, &sealed); err != nil {
			return nil, err
		}
		data, err := s.Vault.Open(sealed)
		if err != nil {
			return nil, err
		}
		state, err := desiredstate.Decode(data)
		clear(data)
		if err != nil {
			return nil, err
		}
		if state.NodeID != nodeID {
			return nil, errors.New("published state identity mismatch")
		}
		state, err = state.RuntimeAt(time.Now().UTC())
		if err != nil {
			return nil, err
		}
		for _, inbound := range state.Inbounds {
			if visible[inbound.ID] != nodeID || !inbound.Enabled || !inbound.SubscriptionVisible || inbound.Protocol == "forward" {
				continue
			}
			if address := net.ParseIP(inbound.Listen); address == nil || address.IsLoopback() {
				continue
			}
			endpoint := proxy.Proxy{Name: endpointName(nodeName, inbound.Protocol), Protocol: inbound.Protocol, Port: inbound.Port}
			if name, created := canvasNames[inbound.ID]; created {
				endpoint.Name = name
			}
			endpoints = append(endpoints, endpoint)
			for _, relay := range relays {
				if relay.InboundID == inbound.ID && relay.Enabled && relay.Visible {
					if relayed, ok := RelayEndpoint(endpoint, relay); ok {
						endpoints = append(endpoints, relayed)
					}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reserveEndpointNames(names, endpoints)
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func reserveEndpointNames(names map[string]bool, endpoints []proxy.Proxy) {
	for _, endpoint := range endpoints {
		names[endpoint.Name] = true
	}
	uniqueNames(endpoints)
	for _, endpoint := range endpoints {
		names[endpoint.Name] = true
	}
}
