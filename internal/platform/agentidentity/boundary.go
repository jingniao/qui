// Package agentidentity carries the authenticated transport identity to the
// transaction that consumes its message. An earlier stream check cannot seal
// the race with a concurrent identity replacement.
package agentidentity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"proxycontrol/pkg/contracts/traffic"
)

type contextKey struct{}
type boundary struct{ node, fingerprint, batch, digest string }

var ErrRetired = errors.New("node identity was retired before transaction")

func WithIdentity(ctx context.Context, node, fingerprint string) context.Context {
	return context.WithValue(ctx, contextKey{}, boundary{node: node, fingerprint: fingerprint})
}

// WithPending is only constructed after persisting or authenticating the
// encrypted original batch. Every accounting transaction rechecks its digest.
func WithPending(ctx context.Context, batch traffic.Batch) (context.Context, error) {
	data, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return context.WithValue(ctx, contextKey{}, boundary{node: batch.NodeID, batch: batch.ID, digest: hex.EncodeToString(sum[:])}), nil
}

func Pending(ctx context.Context) bool {
	b, ok := ctx.Value(contextKey{}).(boundary)
	return ok && b.batch != ""
}

func CheckTx(ctx context.Context, tx *sql.Tx, node string) error {
	b, ok := ctx.Value(contextKey{}).(boundary)
	if !ok {
		return nil
	} // module-local operations have no network identity
	if b.node != node {
		return ErrRetired
	}
	var count int
	var err error
	if b.batch != "" {
		err = tx.QueryRowContext(ctx, "SELECT count(*) FROM traffic_pending_batches WHERE node_id=? AND batch_id=? AND digest=?", node, b.batch, b.digest).Scan(&count)
	} else {
		err = tx.QueryRowContext(ctx, "SELECT count(*) FROM node_identities i JOIN nodes n ON n.id=i.node_id WHERE i.node_id=? AND i.fingerprint=? AND i.revoked_at IS NULL AND n.enabled=1 AND n.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM node_identity_retirements r WHERE r.fingerprint=i.fingerprint)", node, b.fingerprint).Scan(&count)
	}
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrRetired
	}
	return nil
}
