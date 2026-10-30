package desiredstate

import "strings"

// ForCore returns the inbound assignment and its reachable routing graph. It
// preserves source ordering for stable rendering and never mutates the input.
// Policies without an inbound scope apply to every core serving their users.
func (s State) ForCore(name, primary string) State {
	r := s
	r.Inbounds = nil
	r.Outbounds = nil
	r.OutboundGroups = nil
	r.ProxyChains = nil
	r.Policies = nil
	r.Users = nil
	r.Credentials = nil
	credentials, users, targets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, in := range s.Inbounds {
		assigned := in.Core
		if assigned == "" {
			assigned = primary
		}
		if assigned == name {
			r.Inbounds = append(r.Inbounds, in)
			if in.Enabled {
				targets[coreTarget(in.DefaultOutbound)] = true
			}
			for _, id := range in.CredentialIDs {
				credentials[id] = true
			}
		}
	}
	for _, c := range s.Credentials {
		if credentials[c.ID] {
			users[c.UserID] = true
		}
	}
	for _, p := range s.Policies {
		matches := len(p.Scope.UserIDs) == 0 && len(p.Scope.DeviceIDs) == 0
		for _, id := range p.Scope.UserIDs {
			matches = matches || users[id]
		}
		for _, c := range s.Credentials {
			if !credentials[c.ID] {
				continue
			}
			for _, id := range p.Scope.DeviceIDs {
				matches = matches || c.DeviceID == id
			}
		}
		if matches && len(r.Inbounds) > 0 {
			r.Policies = append(r.Policies, p)
			if p.Enabled {
				targets[coreTarget(p.Action.TargetID)], targets[coreTarget(p.Action.BackupID)] = true, true
			}
		}
	}
	// Direct/Block are implicit routing and fail-closed targets, selected by
	// protocol by the native renderers rather than by a resource ID.
	if len(r.Inbounds) > 0 {
		for _, o := range s.Outbounds {
			if o.Enabled && (o.Protocol == "direct" || o.Protocol == "block") {
				targets[o.ID] = true
			}
		}
	}
	// Reachability includes failover edges and every hop of a selected chain.
	for changed := true; changed; {
		changed = false
		add := func(id string) {
			id = coreTarget(id)
			if id != "" && !targets[id] {
				targets[id] = true
				changed = true
			}
		}
		for _, o := range s.Outbounds {
			if targets[o.ID] {
				add(o.BackupTargetID)
			}
		}
		for _, g := range s.OutboundGroups {
			if targets[g.ID] {
				for _, m := range g.Members {
					add(m.OutboundID)
				}
				add(g.BackupTargetID)
			}
		}
		for _, c := range s.ProxyChains {
			if targets[c.ID] {
				for _, id := range c.OutboundIDs {
					add(id)
				}
				add(c.FinalOutboundID)
				add(c.BackupTargetID)
			}
		}
	}
	for _, o := range s.Outbounds {
		if targets[o.ID] {
			r.Outbounds = append(r.Outbounds, o)
			credentials[o.CredentialID] = true
		}
	}
	for _, g := range s.OutboundGroups {
		if targets[g.ID] {
			r.OutboundGroups = append(r.OutboundGroups, g)
		}
	}
	for _, c := range s.ProxyChains {
		if targets[c.ID] {
			r.ProxyChains = append(r.ProxyChains, c)
		}
	}
	for _, c := range s.Credentials {
		if credentials[c.ID] {
			r.Credentials = append(r.Credentials, c)
			users[c.UserID] = true
		}
	}
	for _, u := range s.Users {
		if users[u.ID] {
			r.Users = append(r.Users, u)
		}
	}
	return r
}

func coreTarget(id string) string {
	return strings.TrimPrefix(strings.TrimPrefix(id, "group:"), "chain:")
}
