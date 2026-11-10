package backup

import (
	"context"
	"database/sql"
	"encoding/json"

	"proxycontrol/internal/platform/vault"
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
	rows, err := database.QueryContext(ctx, "SELECT format, behavior, last_status, encrypted_cache FROM service_set_sources")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var format, behavior, status string
		var cache []byte
		if err := rows.Scan(&format, &behavior, &status, &cache); err != nil {
			return ErrArchive
		}
		if (format != "yaml" && format != "text" && format != "mrs") ||
			(behavior != "classical" && behavior != "domain" && behavior != "ipcidr") ||
			(status != "pending" && status != "ok" && status != "failed") ||
			(format == "mrs" && behavior == "classical") {
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
