package deployment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrNothingPublished = errors.New("node has no published configuration")

func (r *Repository) RedeployPublished(ctx context.Context, nodeID, author string) (int64, error) {
	state, intent, err := r.PublishedWithIntent(ctx, nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNothingPublished
	}
	if err != nil {
		return 0, err
	}
	rev, err := r.CreateWithInboundIntent(ctx, state, intent, author, fmt.Sprintf("身份恢复：重新下发 r%d", state.Revision))
	if err != nil {
		return 0, err
	}
	if err := r.Deploy(ctx, rev.ID); err != nil {
		if errors.Is(err, ErrNodeBusy) {
			_, _ = r.Database.ExecContext(ctx, "DELETE FROM config_revisions WHERE id=? AND status='draft'", rev.ID)
		}
		return 0, err
	}
	return rev.ID, nil
}
