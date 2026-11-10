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
		// The GOST-only forward options are checked against the resolved core, so
		// a publish cannot carry them onto a core that cannot render them.
		if err := proxy.ValidateInboundForwardOptions(c, in.Core, in.Settings); err != nil {
			return fmt.Errorf("inbound %s: %w", in.ID, err)
		}
		// The xhttp/Reality settings are published only to a core that reported the
		// settings contract for this combination. An older Agent never declared it,
		// so a deployment carrying these settings fails here instead of applying a
		// configuration with the fields dropped.
		if err := proxy.ValidateInboundServerSettings(c, in.Core, in.Protocol, in.Settings); err != nil {
			return fmt.Errorf("inbound %s: %w", in.ID, err)
		}
	}
	cores := node.EnabledCores(c)
	if err := desiredstate.RequireKeywordSupport(state, node.KeywordCapabilities(cores)); err != nil {
		return fmt.Errorf("%w: 该服务器的 Agent 不支持关键字规则，请先升级 Agent", desiredstate.ErrKeywordUnsupported)
	}
	for _, core := range cores {
		// A GOST forward leaves through its own target and carries no outbound
		// resources at all (desiredstate.Validate enforces its enabled direct
		// default), so only cores that open outbounds are checked here.
		if core.Name == desiredstate.CoreGost {
			continue
		}
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
