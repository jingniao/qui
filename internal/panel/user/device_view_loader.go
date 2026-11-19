package user

import (
	"context"
	"database/sql"
	"errors"
	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/desiredstate"
)

// loadDeviceViewNodes reads only node configuration and credential ownership.
// It has no access to address snapshots or admission state.
func loadDeviceViewNodes(ctx context.Context, repository *node.Repository, states interface {
	Published(context.Context, string) (desiredstate.State, error)
}) ([]deviceViewNode, error) {
	nodes := []deviceViewNode{}
	for page := 1; ; page++ {
		batch, total, err := repository.Choices(ctx, "", page, 200)
		if err != nil {
			return nil, err
		}
		for _, n := range batch {
			state, err := states.Published(ctx, n.ID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, deviceViewNode{n, state, entryCredentials(state)})
		}
		if page*200 >= total {
			break
		}
	}
	return nodes, nil
}
