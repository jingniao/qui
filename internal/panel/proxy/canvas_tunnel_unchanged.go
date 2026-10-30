package proxy

import (
	"context"
	"reflect"
)

// An unchanged resource stamp alone does not make hand-edited server objects
// unchanged. No-op publication also checks the fields the canvas would save.
func (s *Service) canvasObjectsUnchanged(ctx context.Context, p *canvasPlan) (bool, error) {
	outboundIDs := map[string]string{}
	for _, edge := range p.edges {
		in, err := s.Repository.canvasInboundNamed(ctx, edge.targetNode, edge.inboundName)
		if err != nil {
			return false, nil
		}
		if in.NodeID != edge.targetNode || in.Protocol != edge.protocol || in.Port != edge.port || in.Listen != "0.0.0.0" || !in.Enabled || !in.InternalRelay || in.DefaultOutbound != "direct" {
			return false, nil
		}
		out, err := s.Repository.canvasOutboundNamed(ctx, edge.sourceNode, edge.outboundName)
		if err != nil {
			return false, nil
		}
		if out.TargetInboundID != in.ID || out.Protocol != edge.protocol || !out.Enabled || out.SupportTCP != edge.tcp || out.SupportUDP != edge.udp || out.FailurePolicy != "Block" {
			return false, nil
		}
		outboundIDs[edge.outboundName] = out.ID
	}
	for _, f := range p.forwards {
		in, err := s.Repository.canvasInboundNamed(ctx, f.NodeID, f.Name)
		if err != nil {
			return false, nil
		}
		if in.NodeID != f.NodeID || in.Protocol != "forward" || in.Port != f.Port || !in.Enabled || in.Listen != f.Listen || in.DefaultOutbound != f.DefaultOutbound || !reflect.DeepEqual(in.Settings.Forward, f.Settings.Forward) {
			return false, nil
		}
	}
	for _, chain := range p.chains {
		existing, err := s.Repository.canvasChainNamed(ctx, chain.name)
		if err != nil {
			return false, nil
		}
		desired := []string{}
		tcp, udp := true, true
		for _, edge := range chain.outbounds {
			desired = append(desired, outboundIDs[edge.outboundName])
			tcp = tcp && edge.tcp
			udp = udp && edge.udp
		}
		if !existing.Enabled || existing.FinalOutboundID != "direct" || existing.SupportTCP != tcp || existing.SupportUDP != udp || !reflect.DeepEqual(existing.NodeIDs, chain.nodes) || !reflect.DeepEqual(existing.OutboundIDs, desired) {
			return false, nil
		}
	}
	return true, nil
}
