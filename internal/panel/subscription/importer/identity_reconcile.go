package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/platform/id"
)

// IdentityReconciler rewrites a module's references in the identity transaction.
// Resolve returns raw new IDs and whether the source is content. An empty key
// selects the complete stored source, including caches of failed sources.
type IdentityReconciler func(context.Context, *sql.Tx, func(string, string) ([]string, bool), map[string]string) ([]string, error)

// ReconcileImportedIdentities commits identities, references, report and marker
// together. Startup supplies the module adapters before exposing any readers.
func (s *Store) ReconcileImportedIdentities(ctx context.Context, reconcilers ...IdentityReconciler) error {
	s.refresh.Lock()
	defer s.refresh.Unlock()
	tx, err := s.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var done bool
	if err = tx.QueryRowContext(ctx, "SELECT done FROM import_identity_reconcile WHERE id=1").Scan(&done); err != nil {
		return err
	}
	if done {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,url_encrypted FROM subscription_sources ORDER BY id")
	if err != nil {
		return err
	}
	sources := []string{}
	for rows.Next() {
		var sourceID string
		var sealed []byte
		if err = rows.Scan(&sourceID, &sealed); err != nil {
			rows.Close()
			return err
		}
		kind, e := s.sourceKindStrict(sealed)
		if e != nil {
			rows.Close()
			return e
		}
		if kind == sourceKindContent {
			sources = append(sources, sourceID)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	keys := map[string]map[string][]string{}
	refs := map[string]string{}
	for _, src := range sources {
		keys[src] = map[string][]string{"": {}}
		rows, err = tx.QueryContext(ctx, "SELECT id,name,protocol,payload_encrypted FROM imported_proxies WHERE source_id=? ORDER BY id", src)
		if err != nil {
			return err
		}
		type entry struct{ old, name, protocol string }
		entries := []entry{}
		newIDs := []string{}
		for rows.Next() {
			var e entry
			var sealed []byte
			if err = rows.Scan(&e.old, &e.name, &e.protocol, &sealed); err != nil {
				rows.Close()
				return err
			}
			raw, openErr := s.Vault.Open(sealed)
			if openErr != nil {
				rows.Close()
				return openErr
			}
			var p proxy.Proxy
			decodeErr := json.Unmarshal(raw, &p)
			clear(raw)
			if decodeErr != nil {
				rows.Close()
				return decodeErr
			}
			if p.Name != e.name || p.Protocol != e.protocol {
				rows.Close()
				return fmt.Errorf("import identity %s/%s: cached name or protocol mismatch", src, e.old)
			}
			entries = append(entries, e)
			next, idErr := id.New()
			if idErr != nil {
				rows.Close()
				return idErr
			}
			newIDs = append(newIDs, next)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		// Preserve the existing ID order used by subscription name deduplication.
		sort.Strings(newIDs)
		for i, e := range entries {
			next := newIDs[i]
			refs[e.old] = next
			key := e.name + "|" + e.protocol
			keys[src][""] = append(keys[src][""], next)
			keys[src][key] = append(keys[src][key], next)
			keys[src]["id:"+e.old] = []string{next}
			if _, err = tx.ExecContext(ctx, "UPDATE imported_proxies SET id=? WHERE source_id=? AND id=?", next, src, e.old); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO import_identity_map(source_id,old_proxy_id,old_proxy_key,new_proxy_id) VALUES(?,?,?,?)", src, e.old, key, next); err != nil {
				return err
			}
		}
	}
	resolve := func(src, key string) ([]string, bool) { byKey, ok := keys[src]; return byKey[key], ok }
	report := []string{}
	for _, reconcile := range reconcilers {
		items, e := reconcile(ctx, tx, resolve, refs)
		if e != nil {
			return e
		}
		report = append(report, items...)
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('import_identity_report',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", string(data)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE import_identity_reconcile SET done=1 WHERE id=1"); err != nil {
		return err
	}
	return tx.Commit()
}
