package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"

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
	// 0133 added the last result and the last success. An archive from before it
	// has neither column and is still a valid input; when it does have them, each
	// result must be one this panel could have written and must agree with its own
	// row: same source, same attempt and success time, and the status that result
	// implies.
	var results int64
	if db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0133_subscription_source_results.sql'").Scan(&results) != nil {
		return ErrArchive
	}
	queries := []string{"SELECT id,name,interval_seconds,last_fetch_at,last_status,last_error,created_at,updated_at FROM subscription_sources LIMIT 0", "SELECT user_id,source_id FROM user_source_grants LIMIT 0", "SELECT client_published_layout FROM canvas_schemes LIMIT 0"}
	if results == 1 {
		queries = append(queries, "SELECT last_success_at,latest_result_json FROM subscription_sources LIMIT 0")
	}
	for _, q := range queries {
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			return ErrArchive
		}
		rows.Close()
	}
	if results == 1 {
		rows, err := db.QueryContext(ctx, "SELECT id,last_fetch_at,last_success_at,last_status,latest_result_json FROM subscription_sources WHERE latest_result_json<>'' ORDER BY id")
		if err != nil {
			return ErrArchive
		}
		for rows.Next() {
			var sourceID, lastFetch, lastSuccess, status, stored string
			if rows.Scan(&sourceID, &lastFetch, &lastSuccess, &status, &stored) != nil ||
				importer.ValidateStoredResult(sourceID, lastFetch, lastSuccess, status, []byte(stored)) != nil {
				rows.Close()
				return ErrArchive
			}
		}
		if rows.Err() != nil {
			rows.Close()
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
					decoder := json.NewDecoder(bytes.NewReader(raw))
					decoder.DisallowUnknownFields()
					e = decoder.Decode(&p)
					if e == nil {
						var extra any
						if decoder.Decode(&extra) != io.EOF {
							e = ErrArchive
						}
					}
					if e == nil && p.Protocol == "shadowsocks" && proxy.SS2022(p.Method) {
						e = importer.ValidateProxy(p)
					}

					if e == nil {
						if code, _ := proxy.ValidateClientSettings(p); code != "" {
							e = ErrArchive
						}
					}
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
