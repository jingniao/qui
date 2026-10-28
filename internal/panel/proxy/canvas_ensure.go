package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// canvasEnsure creates or updates the objects the drawing describes, in
// dependency order: relay inbounds on the targets, outbounds on the sources,
// proxy chains for two-edge paths, and finally the entry inbounds' default
// outbound. Every change registers its reversal in undo; nothing here removes
// an object the previous publication created.
func (s *Service) canvasEnsure(ctx context.Context, plan *canvasPlan, stale *canvasStale, undo *canvasUndo) ([]string, error) {
	warnings := []string{}
	for _, input := range plan.forwards {
		existing, err := s.Repository.canvasInboundNamed(ctx, input.NodeID, input.Name)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return warnings, err
		}
		saved, err := s.SaveInbound(ctx, existing.ID, input)
		if err != nil {
			return warnings, err
		}
		if existing.ID == "" {
			undo.add(func(ctx context.Context) error { return s.Repository.DeleteInbound(ctx, saved.ID) })
		} else {
			previous := existing
			undo.add(func(ctx context.Context) error {
				_, err := s.SaveInbound(ctx, previous.ID, inboundInputOf(previous, previous.DefaultOutbound))
				return err
			})
		}
		warnings = append(warnings, "端口转发不验证用户身份；用户额度、设备限制不适用，也不生成用户域名访问记录")
	}
	for _, edge := range plan.edges {
		input := InboundInput{Name: edge.inboundName, NodeID: edge.targetNode, Protocol: edge.protocol, Listen: "0.0.0.0", Port: edge.port, Enabled: true, InternalRelay: true, SubscriptionVisible: false, AllowedUserIDs: []string{}, DefaultOutbound: "direct"}
		existing, err := s.Repository.canvasInboundNamed(ctx, edge.targetNode, edge.inboundName)
		switch {
		case errors.Is(err, ErrNotFound) && edge.adopt.ID != "":
			existing = edge.adopt
		case errors.Is(err, ErrNotFound):
		case err != nil:
			return warnings, err
		}
		if existing.ID == "" {
			inbound, err := s.SaveInbound(ctx, "", input)
			if err != nil {
				return warnings, err
			}
			undo.add(func(ctx context.Context) error { return s.Repository.DeleteInbound(ctx, inbound.ID) })
			edge.inboundID = inbound.ID
			continue
		}
		previous := existing
		inbound, err := s.SaveInbound(ctx, existing.ID, input)
		if err != nil {
			return warnings, err
		}
		undo.add(func(ctx context.Context) error {
			_, err := s.SaveInbound(ctx, previous.ID, inboundInputOf(previous, previous.DefaultOutbound))
			return err
		})
		edge.inboundID = inbound.ID
	}
	for _, edge := range plan.edges {
		input := OutboundInput{Name: edge.outboundName, NodeID: edge.sourceNode, Protocol: edge.protocol, Enabled: true, TargetInboundID: edge.inboundID, SupportTCP: edge.tcp, SupportUDP: edge.udp, FailurePolicy: "Block"}
		existing, err := s.Repository.canvasOutboundNamed(ctx, edge.sourceNode, edge.outboundName)
		if errors.Is(err, ErrNotFound) {
			outbound, err := s.SaveOutbound(ctx, "", input)
			if err != nil {
				return warnings, err
			}
			undo.add(func(ctx context.Context) error { return s.Repository.DeleteOutbound(ctx, outbound.ID) })
			edge.outboundID = outbound.ID
			continue
		}
		if err != nil {
			return warnings, err
		}
		if existing.TargetInboundID != edge.inboundID || existing.Protocol != edge.protocol {
			// The name encodes the binding, so this outbound was changed by hand.
			// Its relay target is immutable; replacing it would mean deleting an
			// object before the publication is known to succeed.
			return warnings, fmt.Errorf("生成的出站 %s 已被手动修改，请删除它后重新发布", existing.Name)
		}
		previous := existing
		outbound, err := s.SaveOutbound(ctx, existing.ID, input)
		if err != nil {
			return warnings, err
		}
		undo.add(func(ctx context.Context) error {
			_, err := s.SaveOutbound(ctx, previous.ID, OutboundInput{Name: previous.Name, NodeID: previous.NodeID, Protocol: previous.Protocol, Enabled: previous.Enabled, TargetInboundID: previous.TargetInboundID, SupportTCP: previous.SupportTCP, SupportUDP: previous.SupportUDP, FailurePolicy: previous.FailurePolicy, BackupOutboundID: previous.BackupOutboundID, ProbeURL: previous.ProbeURL, ProbeInterval: previous.ProbeInterval})
			return err
		})
		edge.outboundID = outbound.ID
	}
	for index := range plan.chains {
		chain := &plan.chains[index]
		input := ChainInput{Name: chain.name, NodeIDs: chain.nodes, OutboundIDs: []string{}, FinalOutboundID: "direct", SupportTCP: true, SupportUDP: true, Enabled: true, FailurePolicy: "Block"}
		for _, edge := range chain.outbounds {
			input.OutboundIDs = append(input.OutboundIDs, edge.outboundID)
			input.SupportTCP = input.SupportTCP && edge.tcp
			input.SupportUDP = input.SupportUDP && edge.udp
		}
		existing, err := s.Repository.canvasChainNamed(ctx, chain.name)
		if errors.Is(err, ErrNotFound) {
			saved, err := s.SaveChain(ctx, "", input)
			if err != nil {
				return warnings, err
			}
			undo.add(func(ctx context.Context) error { return s.Repository.DeleteChain(ctx, saved.ID) })
			chain.id = saved.ID
			continue
		}
		if err != nil {
			return warnings, err
		}
		previous := existing
		saved, err := s.SaveChain(ctx, existing.ID, input)
		if err != nil {
			return warnings, err
		}
		undo.add(func(ctx context.Context) error {
			_, err := s.SaveChain(ctx, previous.ID, ChainInput{Name: previous.Name, NodeIDs: previous.NodeIDs, OutboundIDs: previous.OutboundIDs, FinalOutboundID: previous.FinalOutboundID, SupportTCP: previous.SupportTCP, SupportUDP: previous.SupportUDP, Enabled: previous.Enabled, FailurePolicy: previous.FailurePolicy, BackupTargetID: previous.BackupTargetID})
			return err
		})
		chain.id = saved.ID
	}
	for index := range plan.entries {
		entry := &plan.entries[index]
		current := entry.existing.DefaultOutbound
		target := current
		switch {
		case entry.chain != nil:
			target = "chain:" + entry.chain.id
		case entry.edge != nil:
			target = entry.edge.outboundID
		case stale.outbound[current] || stale.chainID[strings.TrimPrefix(current, "chain:")]:
			// The drawing no longer routes this inbound anywhere: the Owner
			// removed its arrow. Its old route is about to be removed, so it
			// falls back to Direct, and the canvas says so.
			target = "direct"
			warnings = append(warnings, fmt.Sprintf("入站 %s 在方案中已没有出站连接，默认出站改为 direct", entry.existing.Name))
		}
		if target == current {
			continue
		}
		previous := entry.existing
		if _, err := s.SaveInbound(ctx, previous.ID, inboundInputOf(previous, target)); err != nil {
			return warnings, err
		}
		undo.add(func(ctx context.Context) error {
			_, err := s.SaveInbound(ctx, previous.ID, inboundInputOf(previous, previous.DefaultOutbound))
			return err
		})
	}
	return warnings, nil
}
