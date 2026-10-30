package proxy

import "context"

// A shared edge can be a hop of another drawing's chain. Its chain and entry
// route must move together with that edge; otherwise deleting the old chain
// would either be blocked or break the other drawing. Expand only routing
// dependencies of shared tunnels, not disconnected edits in other drawings.
func (s *Service) canvasSharedDependencies(ctx context.Context, p *canvasPlan) error {
	schemes, _, err := s.Repository.CanvasSchemes(ctx, 1, 1000000)
	if err != nil {
		return err
	}
	others := []*canvasPlan{}
	wanted := map[string]bool{}
	for tunnelID := range p.tunnelIDs {
		wanted[tunnelID] = true
	}
	processed := map[string]bool{}
	for {
		progress := false
		for _, scheme := range schemes {
			if scheme.ID == p.schemeID || scheme.Execution != "server" || processed[scheme.ID] {
				continue
			}
			related := false
			for _, c := range scheme.Layout.Connections {
				if wanted[c.TunnelID] {
					related = true
				}
			}
			if !related {
				continue
			}
			scheme.Layout, err = s.canvasDependencyLayout(ctx, scheme.Layout, wanted)
			if err != nil {
				return err
			}
			other, err := s.canvasBuildPlanInternal(ctx, scheme, true)
			if err != nil {
				return err
			}
			others = append(others, other)
			processed[scheme.ID] = true
			progress = true
			for tunnelID := range other.tunnelIDs {
				wanted[tunnelID] = true
			}
		}
		if !progress {
			break
		}
	}
	edges := map[string]*canvasEdge{}
	for _, edge := range p.edges {
		edges[edge.outboundName] = edge
	}
	chains := map[string]bool{}
	for _, chain := range p.chains {
		chains[chain.name] = true
	}
	entries := map[string]bool{}
	for _, entry := range p.entries {
		entries[entry.existing.ID] = true
	}
	for _, other := range others {
		needed := map[string]bool{}
		for _, chain := range other.chains {
			shared := false
			for _, edge := range chain.outbounds {
				if p.tunnelIDs[edge.tunnelID] {
					shared = true
				}
			}
			if shared {
				needed[chain.name] = true
				for _, edge := range chain.outbounds {
					needed[edge.outboundName] = true
				}
			}
		}
		for _, edge := range other.edges {
			if !p.tunnelIDs[edge.tunnelID] && !needed[edge.outboundName] {
				continue
			}
			if edges[edge.outboundName] == nil {
				p.edges = append(p.edges, edge)
				edges[edge.outboundName] = edge
				p.inbounds[edge.inboundName] = true
				p.outbounds[edge.outboundName] = true
				p.owners[edge.inboundName] = edge.tunnelID
				p.owners[edge.outboundName] = edge.tunnelID
				p.tunnelIDs[edge.tunnelID] = true
				p.tunnelVersions[edge.tunnelID] = other.tunnelVersions[edge.tunnelID]
				p.node(edge.sourceNode)
				p.node(edge.targetNode)
			}
		}
		for _, chain := range other.chains {
			if !needed[chain.name] || chains[chain.name] {
				continue
			}
			chain.outbounds = append([]*canvasEdge(nil), chain.outbounds...)
			for n, edge := range chain.outbounds {
				chain.outbounds[n] = edges[edge.outboundName]
			}
			p.chains = append(p.chains, chain)
			chains[chain.name] = true
			p.chainName[chain.name] = true
			p.owners[chain.name] = other.owners[chain.name]
		}
		for _, entry := range other.entries {
			related := entry.edge != nil && edges[entry.edge.outboundName] != nil || entry.chain != nil && needed[entry.chain.name]
			if related && !entries[entry.existing.ID] {
				p.entries = append(p.entries, entry)
				entries[entry.existing.ID] = true
				p.node(entry.existing.NodeID)
			}
		}
	}
	// Appending chains may move their backing array; rebind every entry pointer.
	for n := range p.entries {
		entry := &p.entries[n]
		if entry.edge != nil {
			entry.edge = edges[entry.edge.outboundName]
		}
		if entry.chain != nil {
			name := entry.chain.name
			for index := range p.chains {
				if p.chains[index].name == name {
					entry.chain = &p.chains[index]
					break
				}
			}
		}
	}
	return nil
}
