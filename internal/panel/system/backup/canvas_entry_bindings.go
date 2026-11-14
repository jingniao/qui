package backup

import (
	"context"
	"database/sql"
	"proxycontrol/internal/panel/proxy"
	"strings"
	"unicode"
)

func validateCanvasEntryBindings(ctx context.Context, database *sql.DB) error {
	if err := validateCanvasEntryBaselines(ctx, database); err != nil {
		return err
	}
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0146_canvas_entry_bindings.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	var invalid int
	err := database.QueryRowContext(ctx, `SELECT count(*) FROM canvas_entry_bindings b
 LEFT JOIN inbounds i ON i.id=b.inbound_id LEFT JOIN canvas_schemes s ON s.id=b.scheme_id
 WHERE b.mode IS NULL OR b.mode NOT IN ('create','reuse')
 OR b.state IS NULL OR b.state NOT IN ('applied','detached')
 OR (b.scheme_id IS NULL AND b.state <> 'detached')
 OR (b.scheme_id IS NOT NULL AND b.state = 'detached')
 OR b.original_default_outbound IS NULL OR b.applied_default_outbound IS NULL
 OR (b.mode='create' AND b.original_default_outbound <> '')
 OR (b.mode='reuse' AND (b.original_default_outbound IS NULL OR b.original_default_outbound=''))
 OR i.id IS NULL OR (b.scheme_id IS NOT NULL AND s.id IS NULL)`).Scan(&invalid)
	if err != nil || invalid != 0 {
		return ErrArchive
	}
	// Detached entries have no owner; instance IDs need only be unique within a scheme.
	err = database.QueryRowContext(ctx, `SELECT count(*) FROM (
 SELECT scheme_id,instance_id FROM canvas_entry_bindings WHERE scheme_id IS NOT NULL
 GROUP BY scheme_id,instance_id HAVING count(*) > 1)`).Scan(&invalid)
	if err != nil || invalid != 0 {
		return ErrArchive
	}
	rows, err := database.QueryContext(ctx, "SELECT original_default_outbound,applied_default_outbound FROM canvas_entry_bindings")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var original, applied string
		if err := rows.Scan(&original, &applied); err != nil || !validCanvasBindingRoute(original) || !validCanvasBindingRoute(applied) {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

func validateCanvasEntryBaselines(ctx context.Context, database *sql.DB) error {
	var applied int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE version='0148_canvas_entry_baseline.sql'").Scan(&applied); err != nil {
		return ErrArchive
	}
	if applied == 0 {
		return nil
	}
	rows, err := database.QueryContext(ctx, "SELECT creation_config_json,creation_groups_json FROM canvas_entry_bindings")
	if err != nil {
		return ErrArchive
	}
	defer rows.Close()
	for rows.Next() {
		var config, groups *string
		if rows.Scan(&config, &groups) != nil || proxy.ValidateCanvasEntryBaseline(config, groups) != nil {
			return ErrArchive
		}
	}
	if rows.Err() != nil {
		return ErrArchive
	}
	return nil
}

// Match the proxy.SaveInbound route encodings without constraining the ID
// alphabet, which no contract fixes. Do not resolve references: a missing original
// outbound is preserved evidence that removal needs an explicit restore target.
func validCanvasBindingRoute(value string) bool {
	if value == "" || value == "direct" || value == "block" {
		return true
	}
	if kind, target, found := strings.Cut(value, ":"); found {
		if kind != "chain" && kind != "group" {
			return false
		}
		value = target
	}
	return value != "" && !strings.ContainsFunc(value, unicode.IsControl)
}
