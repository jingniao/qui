package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/vault"
)

// validateImportIdentity applies the stable identity contract only to archives
// that have migration 0147. Older archives are reconciled during recovery.
func validateImportIdentity(ctx context.Context, database *sql.DB, configurationVault *vault.Vault) error {
	var applied int
	if database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0147_imported_proxy_identity.sql'").Scan(&applied) != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	var count, done, invalid int
	if database.QueryRowContext(ctx, "SELECT count(*),max(done),sum(CASE WHEN id=1 AND typeof(done)='integer' AND done IN (0,1) THEN 0 ELSE 1 END) FROM import_identity_reconcile").Scan(&count, &done, &invalid) != nil || count != 1 || invalid != 0 {
		return ErrArchive
	}
	for _, query := range []string{
		"SELECT count(*) FROM import_identity_map m LEFT JOIN imported_proxies p ON p.id=m.new_proxy_id LEFT JOIN subscription_sources s ON s.id=m.source_id WHERE p.id IS NULL OR s.id IS NULL OR p.source_id<>m.source_id",
		"SELECT count(*) FROM imported_proxies WHERE revision IS NULL OR typeof(revision)<>'integer' OR revision<1",
	} {
		if database.QueryRowContext(ctx, query).Scan(&invalid) != nil || invalid != 0 {
			return ErrArchive
		}
	}
	rows, err := database.QueryContext(ctx, "SELECT value_json FROM system_settings WHERE key='import_identity_report'")
	if err != nil {
		return ErrArchive
	}
	for rows.Next() {
		var report string
		if rows.Scan(&report) != nil || !json.Valid([]byte(report)) {
			rows.Close()
			return ErrArchive
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrArchive
	}
	rows, err = database.QueryContext(ctx, "SELECT name,protocol,payload_encrypted FROM imported_proxies")
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
		if !stable && !legacy && (key != "" || done != 0) {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}
