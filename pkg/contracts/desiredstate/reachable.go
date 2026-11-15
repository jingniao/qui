package desiredstate

// ReachableOutbounds returns enabled outbounds reachable from enabled non-GOST
// inbounds in a rendered state. ForCore owns the policy, group, chain and backup
// traversal; filtering roots first excludes entries disabled for lack of users.
// The returned slice preserves source order and does not mutate the state.
func (s State) ReachableOutbounds() []Outbound {
	active := s
	active.Inbounds = nil
	active.Outbounds = nil
	cores := map[string]bool{}
	for _, in := range s.Inbounds {
		if in.Enabled && in.Core != CoreGost {
			active.Inbounds = append(active.Inbounds, in)
			cores[in.Core] = true
		}
	}
	for _, out := range s.Outbounds {
		if out.Enabled {
			active.Outbounds = append(active.Outbounds, out)
		}
	}
	seen := map[string]bool{}
	for name := range cores {
		for _, out := range active.ForCore(name, "").Outbounds {
			seen[out.ID] = true
		}
	}
	var result []Outbound
	for _, out := range active.Outbounds {
		if seen[out.ID] {
			result = append(result, out)
		}
	}
	return result
}
