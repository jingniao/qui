package published

import (
	"context"
	"database/sql"
	"encoding/json"

	"proxycontrol/internal/panel/subscription/proxy"
)

// Relay is the credential-free endpoint captured by a successful publication.
type Relay struct {
	ID                 string
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
	rows, err := s.Database.QueryContext(ctx, `SELECT published_snapshot FROM tunnel_rules WHERE published_snapshot IS NOT NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Relay{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item Relay
		if err = json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
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
	endpoint.Name += " · 经 " + relay.Entry.Name
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
