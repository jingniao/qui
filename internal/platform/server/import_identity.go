package server

import (
	"context"
	"database/sql"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/rules"
)

// reconcileImportReferences keeps module writes inside the importer's transaction.
func reconcileImportReferences(ctx context.Context, tx *sql.Tx, resolve func(string, string) ([]string, bool), identities map[string]string) ([]string, error) {
	var report []string
	for _, rewrite := range []func(context.Context, *sql.Tx, func(string, string) ([]string, bool)) ([]string, error){node.ReconcileImportIdentitiesTx, rules.ReconcileImportIdentitiesTx} {
		items, err := rewrite(ctx, tx, resolve)
		if err != nil {
			return nil, err
		}
		report = append(report, items...)
	}
	// Existing URL rows retain their identity; absent rows remain unresolved.
	rows, err := tx.QueryContext(ctx, "SELECT id FROM imported_proxies")
	if err != nil {
		return nil, err
	}
	current := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		current[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items, err := proxy.ReconcileImportRefsTx(ctx, tx, func(old string) (string, bool) {
		if next, ok := identities[old]; ok {
			return next, true
		}
		return old, current[old]
	})
	return append(report, items...), err
}
