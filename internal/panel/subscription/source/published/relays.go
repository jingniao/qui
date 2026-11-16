package published

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"proxycontrol/internal/panel/subscription/proxy"
)

// Relay is the credential-free endpoint captured by a successful publication.
// Name and Server are display/dial metadata, not runtime configuration:
// RelayStore refreshes them from the rule name and the entry node's current
// client connect address, so neither needs a republish to reach subscriptions.
type Relay struct {
	ID                 string
	Name               string `json:"-"`
	InboundID          string
	Entry              proxy.Ref
	Server             string
	Port               int
	Network            string
	DestinationAddress string
	DestinationPort    int
	Enabled            bool
	Visible            bool
}

type Relays interface {
	Relays(context.Context) ([]Relay, error)
}

type RelayStore struct {
	Database interface {
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	}
}

func (s *RelayStore) Relays(ctx context.Context) ([]Relay, error) {
	rows, err := s.Database.QueryContext(ctx, `SELECT r.published_snapshot,r.name,n.id IS NOT NULL,COALESCE(NULLIF(n.connect_host,''),n.public_ip,'')
		FROM tunnel_rules r LEFT JOIN nodes n ON n.id=r.entry_node_id
		WHERE r.published_snapshot IS NOT NULL ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Relay{}
	for rows.Next() {
		var raw, name, address string
		var entryKnown bool
		if err = rows.Scan(&raw, &name, &entryKnown, &address); err != nil {
			return nil, err
		}
		var item Relay
		if err = json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		item.Name = strings.TrimSpace(name)
		// An entry node without any address keeps the published one rather
		// than silently dropping the endpoint.
		if entryKnown && address != "" {
			item.Server = address
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RelayEndpoint preserves all destination options, including an implicit TLS
// identity, while changing the socket the client connects to.
func RelayEndpoint(endpoint proxy.Proxy, relay Relay) (proxy.Proxy, bool) {
	udp := endpoint.Protocol == "hysteria2" || endpoint.Protocol == "tuic" || endpoint.Protocol == "wireguard"
	if relay.Network != "both" && ((udp && relay.Network != "udp") || (!udp && relay.Network != "tcp")) {
		return proxy.Proxy{}, false
	}
	if relay.Server == "" {
		return proxy.Proxy{}, false
	}
	if endpoint.TLS != nil {
		tls := *endpoint.TLS
		if tls.ServerName == "" {
			tls.ServerName = relay.DestinationAddress
		}
		endpoint.TLS = &tls
	}
	if relay.Name != "" {
		endpoint.Name = relay.Name
	} else {
		endpoint.Name += " · 经 " + relay.Entry.Name
	}
	if endpoint.QUIC != nil {
		quic := *endpoint.QUIC
		quic.HopPorts, quic.HopInterval = "", 0
		endpoint.QUIC = &quic
	}
	endpoint.Server, endpoint.Port = relay.Server, relay.Port
	endpoint.Origin.Ref, endpoint.Origin.Node = relay.ID, relay.Entry
	endpoint.Origin.Relayed = true
	return endpoint, true
}
