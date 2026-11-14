package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/platform/vault"
)

type importRestoreIdentity struct {
	source, id string
}

// Archive templates and canvas drafts survive the authority merge, whereas
// imported rows and group members come from current evidence. Keep provenance
// before replacing those rows; names never identify a row across snapshots.
type importRestoreSnapshot struct {
	keys map[string]map[string][]importRestoreIdentity
	refs map[string]importRestoreIdentity
}

func readImportRestoreSnapshot(ctx context.Context, database *sql.DB, key *vault.Vault) (importRestoreSnapshot, error) {
	result := importRestoreSnapshot{keys: map[string]map[string][]importRestoreIdentity{}, refs: map[string]importRestoreIdentity{}}
	rows, err := database.QueryContext(ctx, "SELECT id,url_encrypted FROM subscription_sources ORDER BY id")
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var sourceID string
		var sealed []byte
		if err := rows.Scan(&sourceID, &sealed); err != nil {
			rows.Close()
			return result, err
		}
		url, err := key.Open(sealed)
		if err != nil {
			rows.Close()
			return result, err
		}
		if len(url) == 0 {
			result.keys[sourceID] = map[string][]importRestoreIdentity{"": {}}
		}
		clear(url)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = database.QueryContext(ctx, `SELECT p.id,p.source_id,p.name,p.protocol,coalesce(m.old_proxy_id,p.id)
FROM imported_proxies p LEFT JOIN import_identity_map m ON m.source_id=p.source_id AND m.new_proxy_id=p.id ORDER BY p.id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var rowID, sourceID, name, protocol, canonical string
		if err := rows.Scan(&rowID, &sourceID, &name, &protocol, &canonical); err != nil {
			return result, err
		}
		identity := importRestoreIdentity{sourceID, canonical}
		if keys, content := result.keys[sourceID]; content {
			result.refs[rowID] = identity
			for _, member := range []string{"", name + "|" + protocol, "id:" + rowID} {
				keys[member] = append(keys[member], identity)
			}
		}
	}
	return result, rows.Err()
}

func reconcileRestoredImportIdentities(ctx context.Context, database *sql.DB, key *vault.Vault, snapshot importRestoreSnapshot) error {
	store := &importer.Store{Database: database, Vault: key}
	// Groups are copied exclusively from current authority. Convert them with
	// its rows, marker and mapping using exactly the startup conversion.
	if err := store.ReconcileImportedIdentities(ctx, func(ctx context.Context, tx *sql.Tx, resolve func(string, string) ([]string, bool), _ map[string]string) ([]string, error) {
		return node.ReconcileImportIdentitiesTx(ctx, tx, resolve)
	}); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.source_id,coalesce(m.old_proxy_id,p.id)
FROM imported_proxies p LEFT JOIN import_identity_map m ON m.source_id=p.source_id AND m.new_proxy_id=p.id ORDER BY p.id`)
	if err != nil {
		return err
	}
	current := map[importRestoreIdentity]string{}
	refs := map[string]string{}
	for rows.Next() {
		var rowID, sourceID, canonical string
		if err := rows.Scan(&rowID, &sourceID, &canonical); err != nil {
			rows.Close()
			return err
		}
		current[importRestoreIdentity{sourceID, canonical}] = rowID
		refs[rowID], refs[canonical] = rowID, rowID
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// An archived reference to a node deleted since the backup stays dangling and
	// reported (decision 4); restoring an older backup must not fail on it.
	var missing []string
	for old, identity := range snapshot.refs {
		if next, ok := current[identity]; ok {
			refs[old] = next
		}
	}
	report, err := rules.ReconcileImportIdentitiesTx(ctx, tx, func(sourceID, member string) ([]string, bool) {
		keys, content := snapshot.keys[sourceID]
		if !content {
			return nil, false
		}
		var ids []string
		for _, identity := range keys[member] {
			if next, ok := current[identity]; ok {
				ids = append(ids, next)
			} else {
				missing = append(missing, fmt.Sprintf("restore import identity %s/%s: no current node, reference retained", identity.source, identity.id))
			}
		}
		return ids, true
	})
	if err != nil {
		return err
	}
	items, err := proxy.ReconcileImportRefsTx(ctx, tx, func(old string) (string, bool) {
		next, exists := refs[old]
		return next, exists
	})
	if err != nil {
		return err
	}
	report = append(append(report, items...), missing...)
	if len(report) != 0 {
		var raw string
		err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key='import_identity_report'").Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var previous []string
		if err == nil {
			if err := json.Unmarshal([]byte(raw), &previous); err != nil {
				return err
			}
		}
		for _, item := range report {
			if !slices.Contains(previous, item) {
				previous = append(previous, item)
			}
		}
		data, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('import_identity_report',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(data)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
