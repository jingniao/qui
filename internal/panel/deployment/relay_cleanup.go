package deployment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

// RelayCleanupDeployer owns durable retries. Only the published state and object
// identities are read; saved draft configuration never enters an automatic revision.
type RelayCleanupDeployer struct {
	repository *Repository
	nodes      *node.Repository
	logger     *slog.Logger
	mu         sync.Mutex
	Now        func() time.Time
}

func NewRelayCleanupDeployer(repository *Repository, nodes *node.Repository, logger *slog.Logger) *RelayCleanupDeployer {
	return &RelayCleanupDeployer{repository: repository, nodes: nodes, logger: logger, Now: time.Now}
}

func (d *RelayCleanupDeployer) Reconcile(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	rows, err := d.repository.Database.QueryContext(ctx, "SELECT node_id FROM relay_cleanup_nodes ORDER BY created_at,node_id")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	var result error
	for _, id := range ids {
		if ctx.Err() != nil {
			return errors.Join(result, ctx.Err())
		}
		result = errors.Join(result, d.reconcileNode(ctx, id, false))
	}
	return result
}

func (d *RelayCleanupDeployer) ReconcileNode(ctx context.Context, nodeID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reconcileNode(ctx, nodeID, false)
}

// ReconcileConnectedNode is called only by a live control stream after storing
// its message. Capabilities precede the first heartbeat, so the stored offline
// status must not delay cleanup on that authenticated connection.
func (d *RelayCleanupDeployer) ReconcileConnectedNode(ctx context.Context, nodeID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reconcileNode(ctx, nodeID, true)
}

func (d *RelayCleanupDeployer) reconcileNode(ctx context.Context, nodeID string, connected bool) error {
	var revision sql.NullInt64
	var attempts int
	var nextAttempt, registeredAt string
	err := d.repository.Database.QueryRowContext(ctx, "SELECT revision_id,attempts,next_attempt_at,updated_at FROM relay_cleanup_nodes WHERE node_id=?", nodeID).Scan(&revision, &attempts, &nextAttempt, &registeredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var deleted bool
	if err := d.repository.Database.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM nodes WHERE id=?", nodeID).Scan(&deleted); err != nil {
		return err
	}
	if deleted {
		return d.clear(ctx, nodeID, registeredAt)
	}
	due, err := time.Parse(time.RFC3339Nano, nextAttempt)
	if err != nil {
		return fmt.Errorf("relay cleanup %s next_attempt_at: %w", nodeID, err)
	}
	if !revision.Valid && d.Now().Before(due) {
		return nil
	}
	if revision.Valid {
		status, err := d.repository.TargetStatus(ctx, nodeID, revision.Int64)
		if err != nil {
			return d.fail(ctx, nodeID, attempts, "status_unreadable", err)
		}
		switch status {
		case "pending":
			return nil
		case "failed", "rollback", "rolled_back":
			var code string
			if err := d.repository.Database.QueryRowContext(ctx, "SELECT result_code FROM deployment_targets WHERE node_id=? AND revision=?", nodeID, revision.Int64).Scan(&code); err != nil {
				return err
			}
			if code == "" {
				code = "agent_" + status
			}
			return d.fail(ctx, nodeID, attempts, code, fmt.Errorf("cleanup revision %d: %s", revision.Int64, status))
		case "success":
			revision.Valid = false
		case "draft":
		default:
			return d.fail(ctx, nodeID, attempts, "unknown_status", fmt.Errorf("cleanup revision %d: %s", revision.Int64, status))
		}
	}
	published, intent, err := d.repository.PublishedWithIntent(ctx, nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return d.clear(ctx, nodeID, registeredAt)
	}
	if err != nil {
		return d.fail(ctx, nodeID, attempts, "published_unreadable", err)
	}
	next, nextIntent, err := d.withoutDeleted(ctx, published, intent)
	if err != nil {
		return d.fail(ctx, nodeID, attempts, "objects_unreadable", err)
	}
	if relayCleanupStatesEqual(published, next) {
		return d.clear(ctx, nodeID, registeredAt)
	}
	if d.Now().Before(due) {
		return nil
	}
	n, err := d.nodes.Get(ctx, nodeID)
	if err != nil {
		return err
	}
	if !n.Enabled || n.DeletedAt != "" || (!connected && n.Status != "online" && n.Status != "degraded") {
		return nil
	}
	if revision.Valid {
		state, err := d.repository.State(ctx, revision.Int64)
		if err != nil {
			return d.fail(ctx, nodeID, attempts, "revision_unreadable", err)
		}
		storedIntent, err := d.repository.InboundIntent(ctx, revision.Int64)
		if err != nil {
			return d.fail(ctx, nodeID, attempts, "revision_unreadable", err)
		}
		if !relayCleanupStatesEqual(state, next) || !reflect.DeepEqual(storedIntent, nextIntent) {
			revision.Valid = false
		}
	}
	if !revision.Valid {
		// The revision and durable pointer commit together. A process stop cannot
		// leave an untracked queued cleanup revision.
		tx, err := d.repository.Database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err = nodeAvailable(ctx, tx, nodeID, ""); err != nil {
			if errors.Is(err, ErrNodeBusy) {
				return nil
			}
			return err
		}
		current, err := d.repository.publishedStateInTransaction(ctx, tx, nodeID)
		if err != nil {
			return err
		}
		if current.Revision != published.Revision {
			return nil
		}
		created, err := d.repository.createStateTx(ctx, tx, next, nextIntent, "auto", "auto: relay cleanup")
		if err != nil {
			tx.Rollback()
			return d.fail(ctx, nodeID, attempts, "revision_failed", err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE relay_cleanup_nodes SET revision_id=?,updated_at=? WHERE node_id=?", created.ID, d.Now().UTC().Format(time.RFC3339Nano), nodeID); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		revision = sql.NullInt64{Int64: created.ID, Valid: true}
	}
	if err = d.repository.Deploy(ctx, revision.Int64); err != nil {
		if errors.Is(err, ErrNodeBusy) {
			return nil
		}
		return d.fail(ctx, nodeID, attempts, "deploy_failed", err)
	}
	return nil
}

func (d *RelayCleanupDeployer) clear(ctx context.Context, nodeID, registeredAt string) error {
	// Another removal may have registered work after the published comparison.
	// Its update token must survive this pass, even if this pass had no changes.
	_, err := d.repository.Database.ExecContext(ctx, "DELETE FROM relay_cleanup_nodes WHERE node_id=? AND updated_at=?", nodeID, registeredAt)
	return err
}

func (d *RelayCleanupDeployer) fail(ctx context.Context, nodeID string, attempts int, code string, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	d.logger.Error("relay cleanup attempt failed", "node", nodeID, "code", code, "error", cause)
	attempts++
	delay := 30 * time.Minute
	if attempts == 1 {
		delay = time.Minute
	} else if attempts == 2 {
		delay = 5 * time.Minute
	}
	stamp := d.Now().UTC()
	_, err := d.repository.Database.ExecContext(ctx, "UPDATE relay_cleanup_nodes SET attempts=?,last_code=?,next_attempt_at=?,updated_at=?,revision_id=NULL WHERE node_id=?", attempts, code, stamp.Add(delay).Format(time.RFC3339Nano), stamp.Format(time.RFC3339Nano), nodeID)
	return err
}

func relayCleanupStatesEqual(a, b desiredstate.State) bool {
	a.Revision, b.Revision = 0, 0
	return reflect.DeepEqual(a, b)
}

func (d *RelayCleanupDeployer) withoutDeleted(ctx context.Context, published desiredstate.State, intent []string) (desiredstate.State, []string, error) {
	// Encoding a copy keeps all nested settings independent of the published
	// revision, while preserving nil slices and every unrelated field.
	data, err := json.Marshal(published)
	if err != nil {
		return desiredstate.State{}, nil, err
	}
	var next desiredstate.State
	if err = json.Unmarshal(data, &next); err != nil {
		return next, nil, err
	}
	ids := map[string]map[string]bool{}
	for _, table := range []string{"inbounds", "outbounds", "proxy_chains"} {
		rows, err := d.repository.Database.QueryContext(ctx, "SELECT id FROM "+table)
		if err != nil {
			return next, nil, err
		}
		ids[table] = map[string]bool{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return next, nil, err
			}
			ids[table][id] = true
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return next, nil, err
		}
	}
	removedCredentials := map[string]bool{}
	keptCredentials := map[string]bool{}
	inbounds := next.Inbounds[:0]
	for _, inbound := range next.Inbounds {
		kept := ids["inbounds"][inbound.ID]
		for _, id := range inbound.CredentialIDs {
			if kept {
				keptCredentials[id] = true
			} else {
				removedCredentials[id] = true
			}
		}
		if kept {
			inbounds = append(inbounds, inbound)
		}
	}
	next.Inbounds = inbounds
	outbounds := next.Outbounds[:0]
	for _, outbound := range next.Outbounds {
		// Snapshot synthesizes a continuation outbound for each chain. Its
		// owning chain identity, rather than an outbound row, determines lifetime.
		continuation := strings.TrimPrefix(outbound.ID, "_chain_continue_")
		kept := outbound.ID == "direct" || outbound.ID == "block" || ids["outbounds"][outbound.ID] || (continuation != outbound.ID && ids["proxy_chains"][continuation])
		if outbound.CredentialID != "" {
			if kept {
				keptCredentials[outbound.CredentialID] = true
			} else {
				removedCredentials[outbound.CredentialID] = true
			}
		}
		if kept {
			outbounds = append(outbounds, outbound)
		}
	}
	next.Outbounds = outbounds
	chains := next.ProxyChains[:0]
	for _, chain := range next.ProxyChains {
		if ids["proxy_chains"][chain.ID] {
			chains = append(chains, chain)
		}
	}
	next.ProxyChains = chains
	credentials := next.Credentials[:0]
	for _, credential := range next.Credentials {
		if !credential.Internal || !removedCredentials[credential.ID] || keptCredentials[credential.ID] {
			credentials = append(credentials, credential)
		}
	}
	next.Credentials = credentials
	var nextIntent []string
	if intent != nil {
		nextIntent = []string{}
		for _, id := range intent {
			if ids["inbounds"][id] {
				nextIntent = append(nextIntent, id)
			}
		}
	}
	return next, nextIntent, nil
}
