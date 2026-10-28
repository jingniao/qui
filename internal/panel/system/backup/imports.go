package backup

import (
	"context"
	"database/sql"
	"encoding/json"

	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/vault"
)

func validateImports(ctx context.Context, db *sql.DB, v *vault.Vault) error {
	var applied int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0116_subscription_sources.sql'").Scan(&applied) != nil {
		return ErrArchive
	}
	for _, table := range []string{"subscription_sources", "imported_proxies", "user_source_grants"} {
		var present int
		if db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&present) != nil || present != applied {
			return ErrArchive
		}
	}
	if applied == 0 {
		return nil
	}
	for _, q := range []string{"SELECT id,name,interval_seconds,last_fetch_at,last_status,last_error,created_at,updated_at FROM subscription_sources LIMIT 0", "SELECT user_id,source_id FROM user_source_grants LIMIT 0", "SELECT client_published_layout FROM canvas_schemes LIMIT 0"} {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return ErrArchive
		}
		rows.Close()
	}
	for _, item := range []struct {
		query   string
		payload bool
	}{
		{"SELECT CASE WHEN length(url_encrypted)<=8256 THEN url_encrypted END FROM subscription_sources", false},
		{"SELECT CASE WHEN length(payload_encrypted)<=2097216 THEN payload_encrypted END FROM imported_proxies", true},
	} {
		rows, err := db.QueryContext(ctx, item.query)
		if err != nil {
			return ErrArchive
		}
		for rows.Next() {
			var encrypted []byte
			if rows.Scan(&encrypted) != nil {
				rows.Close()
				return ErrArchive
			}
			raw, e := v.Open(encrypted)
			if e == nil {
				if item.payload {
					var p proxy.Proxy
					e = json.Unmarshal(raw, &p)
					if e == nil && (p.Name == "" || p.Protocol == "" || p.Port < 1 || p.Port > 65535 || p.DialerProxy != "" || p.ClientChain != "") {
						e = ErrArchive
					}
				} else if len(raw) > 0 && !importer.ValidURL(string(raw)) {
					e = ErrArchive
				}
			}
			clear(raw)
			if e != nil {
				rows.Close()
				return ErrArchive
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrArchive
		}
	}
	return nil
}
