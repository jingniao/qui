package server

import (
	"context"
	"proxycontrol/pkg/contracts/desiredstate"
)

// Even when an unrelated draft cannot render, an old published binding must
// not bypass today's group membership. This only removes access; it does not
// adopt draft routing, listener settings or enablement intent.
func (d *quotaDeployer) filterPublishedGroupAccess(ctx context.Context, state desiredstate.State) (desiredstate.State, error) {
	next := state
	next.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	credentials := map[string]desiredstate.Credential{}
	for _, c := range state.Credentials {
		credentials[c.ID] = c
	}
	used := map[string]bool{}
	for index, inbound := range state.Inbounds {
		ids := []string{}
		keep := map[string]bool{}
		for _, id := range inbound.CredentialIDs {
			c := credentials[id]
			if !c.Internal && c.UserID != "" {
				groups, err := d.nodes.UserInboundGroups(ctx, inbound.ID, c.UserID)
				if err != nil {
					return state, err
				}
				if len(groups) == 0 {
					continue
				}
			}
			ids = append(ids, id)
			keep[id] = true
			used[id] = true
		}
		next.Inbounds[index].CredentialIDs = ids
		if inbound.Settings.WireGuard != nil {
			wg := *inbound.Settings.WireGuard
			wg.Peers = []desiredstate.WireGuardPeer{}
			for _, p := range inbound.Settings.WireGuard.Peers {
				if p.CredentialID == "" || keep[p.CredentialID] {
					wg.Peers = append(wg.Peers, p)
				}
			}
			next.Inbounds[index].Settings.WireGuard = &wg
		}
		if len(ids) == 0 && inbound.Protocol != "forward" {
			next.Inbounds[index].Enabled = false
		}
	}
	removed := map[string]bool{}
	for _, c := range state.Credentials {
		if !c.Internal && c.UserID != "" && !used[c.ID] {
			removed[c.ID] = true
		}
	}
	return next.WithoutCredentials(removed)
}
