package deployment

import (
	"context"
	"database/sql"
	"fmt"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

func validateStateCoresTx(ctx context.Context, tx *sql.Tx, state desiredstate.State) error {
	var raw []byte
	if err := tx.QueryRowContext(ctx, "SELECT capabilities_json FROM nodes WHERE id=?", state.NodeID).Scan(&raw); err != nil {
		return err
	}
	c := &pb.Capabilities{}
	if err := storedJSON.Unmarshal(raw, c); err != nil {
		return err
	}
	for _, in := range state.Inbounds {
		if err := proxy.ValidateInboundCore(c, in.Core, in.Protocol); err != nil {
			name := in.Core
			if name == "" {
				name = c.GetCoreName()
			}
			return fmt.Errorf("inbound %s: core %q is unavailable: %w", in.ID, name, err)
		}
	}
	for _, core := range node.EnabledCores(c) {
		part := state.ForCore(core.Name, c.CoreName)
		for _, out := range part.Outbounds {
			if out.Enabled {
				if err := proxy.ValidateCoreProtocol(core.Name, out.Protocol, false); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
