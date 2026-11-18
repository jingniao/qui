package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/vault"
)

// validateImportIdentity checks the stable identity contract of imported nodes.
func validateImportIdentity(ctx context.Context, database *sql.DB, configurationVault *vault.Vault) error {
	var invalid int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM imported_proxies WHERE revision IS NULL OR typeof(revision)<>'integer' OR revision<1").Scan(&invalid) != nil || invalid != 0 {
		return ErrArchive
	}
	rows, err := database.QueryContext(ctx, "SELECT name,protocol,payload_encrypted FROM imported_proxies")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var name, protocol string
		var sealed []byte
		if rows.Scan(&name, &protocol, &sealed) != nil {
			rows.Close()
			return ErrArchive
		}
		raw, err := configurationVault.Open(sealed)
		var p proxy.Proxy
		if err == nil {
			err = json.Unmarshal(raw, &p)
		}
		clear(raw)
		if err != nil || p.Name != name || p.Protocol != protocol {
			rows.Close()
			return ErrArchive
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	rows, err = database.QueryContext(ctx, `SELECT e.proxy_key,s.url_encrypted,p.id IS NOT NULL
		FROM node_group_imports e JOIN subscription_sources s ON s.id=e.source_id
		LEFT JOIN imported_proxies p ON p.source_id=e.source_id AND e.proxy_key='id:'||p.id`)
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var sealed []byte
		var exists bool
		if rows.Scan(&key, &sealed, &exists) != nil {
			return ErrArchive
		}
		raw, err := configurationVault.Open(sealed)
		content := len(raw) == 0
		clear(raw)
		if err != nil {
			return ErrArchive
		}
		if !content || exists {
			continue
		}
		// Dangling members are a legal, reported state: a stable ID whose node was
		// deleted, or a name|protocol member the conversion could not resolve. A
		// whole-source member exists only before the one-time conversion.
		separator := strings.LastIndexByte(key, '|')
		stable := strings.HasPrefix(key, "id:") && len(key) > len("id:")
		legacy := separator > 0 && separator < len(key)-1
		if !stable && !legacy {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}
