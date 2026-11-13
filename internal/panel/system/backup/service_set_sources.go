package backup

import (
	"context"
	"database/sql"
	"encoding/json"

	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/policy"
)

func validateServiceSetSources(ctx context.Context, database *sql.DB, configurationVault *vault.Vault) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0140_service_set_sources.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	// 0143 added the native format and the Xray file columns; older archives
	// have neither and select empty strings instead.
	var nativeApplied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0143_native_rule_sets.sql'").Scan(&nativeApplied); err != nil {
		return ErrArchive
	}
	query := "SELECT id, url, format, behavior, last_status, encrypted_cache, interval_seconds, '', '' FROM service_set_sources"
	if nativeApplied > 0 {
		query = "SELECT id, url, format, behavior, last_status, encrypted_cache, interval_seconds, xray_url, xray_tag FROM service_set_sources"
	}
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var id, url, format, behavior, status, xrayURL, xrayTag string
		var interval int
		var cache []byte
		if err := rows.Scan(&id, &url, &format, &behavior, &status, &cache, &interval, &xrayURL, &xrayTag); err != nil {
			return ErrArchive
		}
		if (format != "yaml" && format != "text" && format != "mrs" && format != "native") ||
			(behavior != "classical" && behavior != "domain" && behavior != "ipcidr") ||
			(status != "pending" && status != "ok" && status != "failed") ||
			(format == "mrs" && behavior == "classical") {
			return ErrArchive
		}
		if format == "native" {
			// A native source is node-only: the panel never caches it, and
			// its two file addresses must pass the same checks as on save.
			if nativeApplied == 0 || len(cache) > 0 {
				return ErrArchive
			}
			if err := desiredstate.ValidateRuleSet(desiredstate.RuleSet{ID: "native", Behavior: behavior, SingBoxURL: url, XrayURL: xrayURL, XrayTag: xrayTag, RefreshSeconds: interval}); err != nil {
				return ErrArchive
			}
			continue
		}
		if xrayURL != "" || xrayTag != "" {
			return ErrArchive
		}
		if len(cache) > 0 {
			plain, err := configurationVault.Open(cache)
			if err != nil {
				return ErrArchive
			}
			var entries policy.ParsedEntries
			if err := json.Unmarshal(plain, &entries); err != nil {
				return ErrArchive
			}
		}
	}
	if err := rows.Err(); err != nil {
		return ErrArchive
	}
	return nil
}
