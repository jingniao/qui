package proxy

import (
	"context"
)

// replaceManagedInbounds is one atomic reconciliation, including ID ownership.
// It never touches ordinary or canvas objects, even if their names match.
func (r *Repository) replaceManagedInbounds(ctx context.Context, remove, desired []managedInbound) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = r.replaceManagedInboundsTx(ctx, tx, remove, desired); err != nil {
		return err
	}
	return r.commitDraft(tx)
}

func (r *Repository) finishTunnelPublication(ctx context.Context, p tunnelPublication) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = r.finishTunnelPublicationTx(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit()
}
