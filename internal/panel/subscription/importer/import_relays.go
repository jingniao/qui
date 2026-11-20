package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"proxycontrol/internal/panel/subscription/importtarget"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/platform/id"
)

func (s *Store) importRelays(ctx context.Context, viewer source.Viewer) ([]proxy.Proxy, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT r.published_snapshot,r.name,
		COALESCE(NULLIF(n.connect_host,''),NULLIF(n.public_ip,''),''),
		group_concat(DISTINCT g.id)
		FROM tunnel_rules r JOIN node_group_relays e ON e.rule_id=r.id
		JOIN node_groups g ON g.id=e.group_id JOIN user_node_group_grants a ON a.group_id=g.id
		LEFT JOIN nodes n ON n.id=json_extract(r.published_snapshot,'$.Entry.ID')
		WHERE a.user_id=? AND r.published_snapshot IS NOT NULL
		AND g.enabled=1 AND g.subscription_visible=1 AND g.purpose IN ('subscription','mixed')
		AND COALESCE(json_extract(r.published_snapshot,'$.importId'),'')<>''
		GROUP BY r.id ORDER BY r.id`, viewer.UserID)
	if err != nil {
		return nil, err
	}
	type savedRelay struct {
		relay  published.Relay
		groups string
	}
	var saved []savedRelay
	for rows.Next() {
		var raw, name, address, groups string
		if err = rows.Scan(&raw, &name, &address, &groups); err != nil {
			rows.Close()
			return nil, err
		}
		var relay published.Relay
		if err = json.Unmarshal([]byte(raw), &relay); err != nil {
			rows.Close()
			return nil, err
		}
		relay.Name = strings.TrimSpace(name)
		if address != "" {
			relay.Server = address
		}
		saved = append(saved, savedRelay{relay, groups})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	entries := []proxy.Proxy{}
	for _, item := range saved {
		relay := item.relay
		if !relay.Enabled || !relay.Visible {
			continue
		}
		endpoint, err := importtarget.Load(ctx, s.reader(), s.Vault, relay.ImportID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !viewer.AllowsProtocol(endpoint.Protocol) {
			continue
		}
		// The payload carries today's credentials and protocol options. Its
		// original hostname remains the implicit TLS identity before redirection.
		relay.DestinationAddress = endpoint.Server
		endpoint.Origin = proxy.Origin{Source: "published"}
		for _, group := range strings.Split(item.groups, ",") {
			if viewer.GrantsGroup(group) {
				endpoint.Origin.Labels = append(endpoint.Origin.Labels, "nodeGroup:"+group)
			}
		}
		if len(endpoint.Origin.Labels) == 0 {
			continue
		}
		if relayed, ok := published.RelayEndpoint(endpoint, relay); ok {
			entries = append(entries, relayed)
		}
	}
	return entries, nil
}

// URL subscriptions use name/protocol as their stable member identity, while
// cached row IDs hash their full content. Rebind rule references before removing
// the old hashes so a credential or address refresh does not delete the target.
func (s *Store) replaceImportEntries(ctx context.Context, tx *sql.Tx, sourceID string, entries []proxy.Proxy, stamp string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,name,protocol FROM imported_proxies WHERE source_id=? ORDER BY id`, sourceID)
	if err != nil {
		return err
	}
	type oldEntry struct{ id, name, protocol string }
	var old []oldEntry
	for rows.Next() {
		var item oldEntry
		if err = rows.Scan(&item.id, &item.name, &item.protocol); err != nil {
			rows.Close()
			return err
		}
		old = append(old, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = s.insertEntries(ctx, tx, sourceID, entries, stamp, sourceKindURL); err != nil {
		return err
	}
	byIdentity := map[string]proxy.Proxy{}
	ids := map[string]string{}
	for _, endpoint := range entries {
		raw, err := json.Marshal(endpoint)
		if err != nil {
			return err
		}
		identity := endpoint.Name + "|" + endpoint.Protocol
		byIdentity[identity] = endpoint
		ids[identity] = sourceID + "-" + id.Hash(string(raw))
		clear(raw)
	}
	for _, item := range old {
		identity := item.name + "|" + item.protocol
		nextID, exists := ids[identity]
		if !exists {
			if _, err = tx.ExecContext(ctx, `DELETE FROM imported_proxies WHERE id=?`, item.id); err != nil {
				return err
			}
			continue
		}
		if nextID == item.id {
			continue
		}
		previous, err := importtarget.Load(ctx, tx, s.Vault, item.id)
		if err != nil {
			return err
		}
		next := byIdentity[identity]
		if previous.Server != next.Server || previous.Port != next.Port {
			if err = invalidateImportRules(ctx, tx, item.id); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tunnel_rules SET target_import_id=? WHERE target_mode='import' AND target_import_id=?`, nextID, item.id); err != nil {
			return err
		}
		// Only the identity field changes: the last successful runtime endpoint
		// and publication flags remain exactly as published.
		if _, err = tx.ExecContext(ctx, `UPDATE tunnel_rules SET published_snapshot=json_set(published_snapshot,'$.importId',?) WHERE json_extract(published_snapshot,'$.importId')=?`, nextID, item.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM imported_proxies WHERE id=?`, item.id); err != nil {
			return err
		}
	}
	return nil
}

func invalidateImportRules(ctx context.Context, tx *sql.Tx, importID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE tunnel_rules SET
		status=CASE WHEN published_at IS NOT NULL THEN 'needs_publish' ELSE status END,
		draft_version=draft_version+1,
		updated_at=strftime('%Y-%m-%dT%H:%M:%f','now') || printf('%06d',(draft_version+1)%1000000) || 'Z'
		WHERE target_mode='import' AND target_import_id=? AND status<>'pending_delete'`, importID)
	return err
}
