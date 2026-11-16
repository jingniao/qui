// Package importtarget opens the saved client endpoint shared by forwarding
// rules and subscription imports. It never fetches the external source.
package importtarget

import (
	"context"
	"database/sql"
	"encoding/json"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/vault"
)

type Queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Load(ctx context.Context, q Queryer, v *vault.Vault, id string) (proxy.Proxy, error) {
	var sealed []byte
	if err := q.QueryRowContext(ctx, "SELECT payload_encrypted FROM imported_proxies WHERE id=?", id).Scan(&sealed); err != nil {
		return proxy.Proxy{}, err
	}
	raw, err := v.Open(sealed)
	if err != nil {
		return proxy.Proxy{}, err
	}
	defer clear(raw)
	var endpoint proxy.Proxy
	err = json.Unmarshal(raw, &endpoint)
	return endpoint, err
}
