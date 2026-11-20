// Package availability reads successful route proofs and current node facts.
// It never changes credentials or starts a deployment.
package availability

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/desiredstate"
)

type Reader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Dependencies struct {
	Ref             string
	OwnerNodeID     string
	PublicationID   string
	SourceDigest    string
	NodeIDs         []string
	PathNodeIDs     []string
	TargetInboundID string
}

func Digest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func Unique(ids []string) []string {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func Save(ctx context.Context, tx *sql.Tx, item Dependencies) error {
	if item.Ref == "" || item.OwnerNodeID == "" || item.PublicationID == "" || item.SourceDigest == "" || len(item.NodeIDs) == 0 || len(item.PathNodeIDs) == 0 {
		return fmt.Errorf("subscription route %s: incomplete publication proof", item.Ref)
	}
	raw, err := json.Marshal(Unique(item.NodeIDs))
	if err != nil {
		return err
	}
	path, err := json.Marshal(Unique(item.PathNodeIDs))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO subscription_route_dependencies(endpoint_ref,owner_node_id,publication_id,source_digest,node_ids_json,path_node_ids_json,target_inbound_id)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(endpoint_ref) DO UPDATE SET owner_node_id=excluded.owner_node_id,publication_id=excluded.publication_id,
		source_digest=excluded.source_digest,node_ids_json=excluded.node_ids_json,path_node_ids_json=excluded.path_node_ids_json,target_inbound_id=excluded.target_inbound_id`,
		item.Ref, item.OwnerNodeID, item.PublicationID, item.SourceDigest, string(raw), string(path), item.TargetInboundID)
	return err
}

func Read(ctx context.Context, q Reader, ref string) (Dependencies, error) {
	item := Dependencies{Ref: ref}
	var raw, path string
	err := q.QueryRowContext(ctx, `SELECT owner_node_id,publication_id,source_digest,node_ids_json,path_node_ids_json,target_inbound_id FROM subscription_route_dependencies WHERE endpoint_ref=?`, ref).
		Scan(&item.OwnerNodeID, &item.PublicationID, &item.SourceDigest, &raw, &path, &item.TargetInboundID)
	if err != nil {
		return item, fmt.Errorf("subscription route %s: publication dependencies unavailable: %w", ref, err)
	}
	if err = json.Unmarshal([]byte(raw), &item.NodeIDs); err != nil {
		return item, fmt.Errorf("subscription route %s: %w", ref, err)
	}
	if err = json.Unmarshal([]byte(path), &item.PathNodeIDs); err != nil {
		return item, fmt.Errorf("subscription route %s: %w", ref, err)
	}
	if item.PublicationID == "" || item.SourceDigest == "" || len(item.NodeIDs) == 0 || len(item.PathNodeIDs) == 0 {
		return item, fmt.Errorf("subscription route %s: incomplete publication dependencies", ref)
	}
	return item, nil
}

// Import refreshes may rebind an imported endpoint's cache ID without changing
// the already published socket or the managed part of its path.
func RelayDigest(raw []byte) (string, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	delete(value, "importId")
	return Digest(value)
}

// CaptureNodeTx runs with the runtime revision update in the same transaction.
// Older databases rebuild the same proof from their confirmed configurations.
func CaptureNodeTx(ctx context.Context, tx *sql.Tx, key *vault.Vault, nodeID string) error {
	var revision int64
	var sealed []byte
	err := tx.QueryRowContext(ctx, `SELECT s.revision,r.encrypted_state FROM node_runtime_state s LEFT JOIN config_revisions r ON r.id=s.revision WHERE s.node_id=?`, nodeID).Scan(&revision, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `DELETE FROM subscription_route_dependencies WHERE owner_node_id=? AND endpoint_ref LIKE 'inbound:%'`, nodeID)
		return err
	} // A successful rollback to revision zero removes it.
	if err != nil {
		return err
	}
	raw, err := key.Open(sealed)
	if err != nil {
		return fmt.Errorf("subscription node %s revision %d: %w", nodeID, revision, err)
	}
	state, err := desiredstate.Decode(raw)
	clear(raw)
	if err != nil {
		return fmt.Errorf("subscription node %s revision %d: %w", nodeID, revision, err)
	}
	if state.NodeID != nodeID || state.Revision != revision {
		return fmt.Errorf("subscription node %s: confirmed revision identity mismatch", nodeID)
	}
	digest, err := Digest(state)
	if err != nil {
		return err
	}
	var records []Dependencies
	for _, inbound := range state.Inbounds {
		if !inbound.Enabled || !inbound.SubscriptionVisible {
			continue
		}
		// The sealed owner revision retains its declared route materials. Other
		// nodes can temporarily run a different revision during apply/rollback;
		// validating their listeners here would revoke a real successful ack.
		// Filter proves the complete route from the current confirmed graph.
		ids := []string{nodeID}
		records = append(records, Dependencies{Ref: "inbound:" + inbound.ID, OwnerNodeID: nodeID,
			PublicationID: fmt.Sprintf("revision:%d", state.Revision), SourceDigest: digest, NodeIDs: ids, PathNodeIDs: ids})
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM subscription_route_dependencies WHERE owner_node_id=? AND endpoint_ref LIKE 'inbound:%'`, nodeID); err != nil {
		return err
	}
	for _, record := range records {
		if err = Save(ctx, tx, record); err != nil {
			return err
		}
	}
	return nil
}

// Graph contains only the configurations acknowledged by their Agents. Neither
// inbounds/outbounds tables nor canvas drafts participate in this read.
type Graph struct{ States map[string]desiredstate.State }

func LoadGraph(ctx context.Context, q Reader, key *vault.Vault) (*Graph, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.node_id,s.revision,r.encrypted_state FROM node_runtime_state s JOIN config_revisions r ON r.id=s.revision ORDER BY s.node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	g := &Graph{States: map[string]desiredstate.State{}}
	for rows.Next() {
		var id string
		var revision int64
		var sealed []byte
		if err = rows.Scan(&id, &revision, &sealed); err != nil {
			return nil, err
		}
		raw, err := key.Open(sealed)
		if err != nil {
			return nil, fmt.Errorf("subscription node %s revision %d: %w", id, revision, err)
		}
		state, err := desiredstate.Decode(raw)
		clear(raw)
		if err != nil {
			return nil, fmt.Errorf("subscription node %s revision %d: %w", id, revision, err)
		}
		if state.NodeID != id || state.Revision != revision {
			return nil, fmt.Errorf("subscription node %s: confirmed revision identity mismatch", id)
		}
		g.States[id] = state
	}
	return g, rows.Err()
}
