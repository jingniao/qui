package proxy

import (
	"context"
	"errors"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/id"
)

func (s *Service) canvasEnsureEntry(ctx context.Context, entry *canvasEntry, undo *canvasUndo) error {
	target := entry.finalOutbound
	if entry.chain != nil {
		target = "chain:" + entry.chain.id
	} else if entry.edge != nil {
		target = entry.edge.outboundID
	}
	previous := entry.existing
	created := previous.ID == ""
	if created {
		input := InboundInput{Name: entry.preview.Name, NodeID: entry.preview.NodeID, Protocol: entry.binding.Protocol, Core: entry.binding.Core, Listen: "0.0.0.0", Port: entry.preview.Port, Enabled: true, SubscriptionVisible: true, AllowedUserIDs: []string{}, DefaultOutbound: target}
		if input.Protocol == "trojan" || input.Protocol == "hysteria2" || input.Protocol == "tuic" {
			input.TLSSource = TLSSourceSelfSigned
		}
		saved, err := s.SaveInbound(ctx, "", input)
		if err != nil {
			return err
		}
		entry.existing = saved
		undo.add(func(ctx context.Context) error { return s.Repository.DeleteInbound(ctx, saved.ID) })
	}
	record, err := s.Repository.CanvasEntryBinding(ctx, entry.schemeID, entry.preview.InstanceID)
	newBinding := errors.Is(err, ErrNotFound)
	if err != nil && !newBinding {
		return err
	}
	oldRecord := record
	if newBinding {
		bindingID, err := id.New()
		if err != nil {
			return err
		}
		record = CanvasEntryBindingRecord{ID: bindingID, SchemeID: &entry.schemeID, InstanceID: entry.preview.InstanceID, InboundID: entry.existing.ID, Mode: entry.binding.Mode}
		if created {
			stored, err := s.Repository.Inbound(ctx, entry.existing.ID, true)
			if err != nil {
				return err
			}
			baseline, err := canvasEntryBaseline(stored)
			if err != nil {
				return err
			}
			groups := canvasBaselineGroups(entry.preview.Groups)
			record.CreationConfigJSON, record.CreationGroupsJSON = &baseline, &groups
		}
	}
	if err := s.Repository.applyCanvasEntryBinding(ctx, record, target, newBinding); err != nil {
		return err
	}
	undo.add(func(ctx context.Context) error {
		return s.Repository.undoCanvasEntryBinding(ctx, record.ID, entry.existing.ID, previous.DefaultOutbound, oldRecord, newBinding, created)
	})
	if created && len(entry.preview.Groups) > 0 {
		if s.EntryGroups == nil {
			return errors.New("入口分组写入服务未配置")
		}
		for _, selected := range entry.preview.Groups {
			group, err := s.EntryGroups.Group(ctx, selected.ID)
			if err != nil {
				return err
			}
			before := canvasEntryGroupInput(group)
			input := before
			input.InboundIDs = append(append([]string{}, group.InboundIDs...), entry.existing.ID)
			if _, err := s.EntryGroups.SaveGroup(ctx, group.ID, input); err != nil {
				return err
			}
			undo.add(func(ctx context.Context) error { _, err := s.EntryGroups.SaveGroup(ctx, group.ID, before); return err })
		}
	}
	entry.existing.DefaultOutbound = target
	entry.preview.InboundID, entry.preview.Route = entry.existing.ID, target
	return nil
}

func canvasEntryGroupInput(g node.Group) node.GroupInput {
	return node.GroupInput{Name: g.Name, Purpose: g.Purpose, Enabled: g.Enabled, SubscriptionVisible: g.SubscriptionVisible, ServerIDs: g.ServerIDs, InboundIDs: g.InboundIDs, Imports: g.Imports, Remark: g.Remark}
}
