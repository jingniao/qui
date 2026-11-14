package proxy

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

type CanvasRemoveConflict struct{ Plan CanvasRemovePlan }

func (e *CanvasRemoveConflict) Error() string {
	return "撤除需要确认人工改动或指定有效恢复目标"
}

// Previous revisions belong to the deployment adapter, never to the public API.
type CanvasRemovePublication struct {
	CanvasPublishResult
	Previous map[string]int64 `json:"-"`
}

type CanvasRemovalDeployer interface {
	PublishCanvasRemoval(context.Context, string, string, []NodeSnapshot) (CanvasRemovePublication, error)
	RollbackCanvasRemoval(context.Context, CanvasRemovePublication) error
}

type CanvasRemovalCanceler interface {
	CancelCanvasRemoval(context.Context, CanvasRemovePublication) error
}

type CanvasRemoveResult struct {
	SchemeID     string                    `json:"schemeId"`
	Removed      bool                      `json:"removed"`
	Publications []CanvasRemovePublication `json:"publications"`
}

// RemoveCanvasScheme holds the same lock as save and publish. The entry phase
// must be acknowledged by Agents before channels can stop. Physical deletion
// happens only after both phases succeed, so undo restores existing identities.
func (s *Service) RemoveCanvasScheme(ctx context.Context, schemeID string, input CanvasRemoveInput, author string, users AccessReader) (CanvasRemoveResult, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	result := CanvasRemoveResult{SchemeID: schemeID, Publications: []CanvasRemovePublication{}}
	plan, err := s.planRemoveCanvasScheme(ctx, schemeID, input)
	if err != nil {
		return result, err
	}
	if !plan.OK {
		return result, &CanvasRemoveConflict{Plan: plan}
	}
	deploy, ok := s.Deploy.(CanvasRemovalDeployer)
	if !ok || users == nil {
		return result, errors.New("canvas removal deployment unavailable")
	}
	undo := &canvasUndo{}
	fail := func(cause error) (CanvasRemoveResult, error) {
		cleanup := context.WithoutCancel(ctx)
		// Restore database intent before rollback receipts can trigger access reconciliation.
		cause = errors.Join(cause, undo.run(cleanup))
		for i := len(result.Publications) - 1; i >= 0; i-- {
			if ctx.Err() != nil {
				canceler, ok := deploy.(CanvasRemovalCanceler)
				if !ok {
					return result, errors.Join(cause, errors.New("撤除取消恢复服务不可用，方案保留"))
				}
				if e := canceler.CancelCanvasRemoval(cleanup, result.Publications[i]); e != nil {
					cause = errors.Join(cause, e)
					for earlier := i - 1; earlier >= 0; earlier-- {
						phase := result.Publications[earlier]
						for _, n := range phase.Nodes {
							message := fmt.Sprintf("中断撤除方案 %s：通道回滚确认前保持入口停止；节点 %s 待恢复到修订 %d（入口撤除修订 %d，批次 %s），请在节点配置历史恢复", schemeID, n.NodeID, phase.Previous[n.NodeID], n.Revision, phase.BatchID)
							cause = errors.Join(cause, errors.New(message))
							// The HTTP client may already be gone. Keep the manual recovery
							// target in the existing revision history, not only the response.
							if _, writeErr := s.Repository.Database.ExecContext(cleanup, "UPDATE config_revisions SET summary=summary || ? WHERE id=?", "；"+message, n.Revision); writeErr != nil {
								cause = errors.Join(cause, writeErr)
							}
						}
					}
					break
				}
				continue
			}
			if e := deploy.RollbackCanvasRemoval(cleanup, result.Publications[i]); e != nil {
				cause = errors.Join(cause, fmt.Errorf("撤除运行回滚失败：%w", e))
				break // Do not reopen an entry while its channel rollback failed.
			}
		}
		return result, cause
	}
	entryNodes := []string{}
	seen := map[string]bool{}
	for _, entry := range plan.Entries {
		if !seen[entry.NodeID] {
			entryNodes = append(entryNodes, entry.NodeID)
			seen[entry.NodeID] = true
		}
		before := entry.inbound
		expectedEnabled, expectedRoute := before.Enabled, before.DefaultOutbound
		if entry.Mode == "create" {
			expectedEnabled = false
			_, err = s.Repository.Database.ExecContext(ctx, "UPDATE inbounds SET enabled=0 WHERE id=?", entry.InboundID)
		} else {
			expectedRoute = entry.RestoreTarget
			_, err = s.Repository.Database.ExecContext(ctx, "UPDATE inbounds SET default_outbound=? WHERE id=?", entry.RestoreTarget, entry.InboundID)
		}
		if err != nil {
			return fail(err)
		}
		undo.add(func(ctx context.Context) error {
			res, e := s.Repository.Database.ExecContext(ctx, "UPDATE inbounds SET enabled=?,default_outbound=? WHERE id=? AND enabled=? AND default_outbound=?", before.Enabled, before.DefaultOutbound, before.ID, expectedEnabled, expectedRoute)
			if e != nil {
				return e
			}
			count, e := res.RowsAffected()
			if e != nil {
				return e
			}
			if count != 1 {
				return fmt.Errorf("入口 %s 在撤除期间已修改，未覆盖其当前配置", before.ID)
			}
			return nil
		})
	}
	if len(entryNodes) > 0 {
		publication, e := s.publishCanvasRemovalPhase(ctx, deploy, plan.scheme.Name+"：停止或恢复入口", author, entryNodes, users)
		result.Publications = append(result.Publications, publication)
		if e != nil {
			return fail(e)
		}
	}
	if err = s.checkCanvasRemovalEntries(ctx, plan); err != nil {
		return fail(err)
	}
	channelNodes := map[string]bool{}
	channelStates := map[string]any{}
	for _, object := range plan.Objects {
		if object.Keep {
			continue
		}
		table := canvasRemoveTable(object.Kind)
		state, e := s.canvasRemoveObjectState(ctx, object)
		if e != nil {
			return fail(e)
		}
		switch value := state.(type) {
		case Inbound:
			value.Enabled = false
			state = value
		case Outbound:
			value.Enabled = false
			state = value
		case Chain:
			value.Enabled = false
			state = value
		}
		channelStates[object.Kind+":"+object.ID] = state
		var enabled bool
		if err = s.Repository.Database.QueryRowContext(ctx, "SELECT enabled FROM "+table+" WHERE id=?", object.ID).Scan(&enabled); err != nil {
			return fail(err)
		}
		if _, err = s.Repository.Database.ExecContext(ctx, "UPDATE "+table+" SET enabled=0 WHERE id=?", object.ID); err != nil {
			return fail(err)
		}
		undo.add(func(ctx context.Context) error {
			res, e := s.Repository.Database.ExecContext(ctx, "UPDATE "+table+" SET enabled=? WHERE id=? AND enabled=0", enabled, object.ID)
			if e != nil {
				return e
			}
			count, e := res.RowsAffected()
			if e != nil {
				return e
			}
			if count != 1 {
				return fmt.Errorf("通道对象 %s 在撤除期间已修改，未覆盖其当前配置", object.ID)
			}
			return nil
		})
		channelNodes[object.NodeID] = true
	}
	nodes := []string{}
	for _, nodeID := range plan.Nodes {
		if channelNodes[nodeID] {
			nodes = append(nodes, nodeID)
		}
	}
	if len(nodes) > 0 {
		publication, e := s.publishCanvasRemovalPhase(ctx, deploy, plan.scheme.Name+"：清理通道", author, nodes, users)
		result.Publications = append(result.Publications, publication)
		if e != nil {
			return fail(e)
		}
	}
	if err = s.checkCanvasRemovalEntries(ctx, plan); err != nil {
		return fail(err)
	}
	for _, object := range plan.Objects {
		if object.Keep {
			continue
		}
		current, e := s.canvasRemoveObjectState(ctx, object)
		if e != nil {
			return fail(e)
		}
		if !reflect.DeepEqual(current, channelStates[object.Kind+":"+object.ID]) {
			return fail(fmt.Errorf("通道对象 %s 在撤除期间配置发生变化，请刷新影响预览", object.ID))
		}
	}
	checkInput := input
	checkInput.RestoreTargets = map[string]string{}
	for _, entry := range plan.Entries {
		if entry.Mode == "reuse" {
			checkInput.RestoreTargets[entry.BindingID] = entry.RestoreTarget
		}
	}
	current, e := s.planRemoveCanvasScheme(ctx, schemeID, checkInput)
	if e != nil {
		return fail(e)
	}
	if !current.OK {
		return fail(&CanvasRemoveConflict{Plan: current})
	}
	if !reflect.DeepEqual(plan.Objects, current.Objects) || !reflect.DeepEqual(plan.unsharedTunnelIDs, current.unsharedTunnelIDs) {
		return fail(errors.New("撤除期间通道引用发生变化，请刷新影响预览"))
	}
	guards, err := s.canvasRemovalRows(ctx, plan)
	if err != nil {
		return fail(err)
	}
	// Group writes retain the existing membership service and its authorization effects.
	for _, entry := range plan.Entries {
		if entry.Mode != "create" {
			continue
		}
		for _, groupRef := range entry.Groups {
			if s.EntryGroups == nil {
				return fail(errors.New("入口分组写入服务未配置"))
			}
			group, e := s.EntryGroups.Group(ctx, groupRef.ID)
			if e != nil {
				return fail(e)
			}
			before := canvasEntryGroupInput(group)
			after := before
			after.InboundIDs = []string{}
			for _, id := range before.InboundIDs {
				if id != entry.InboundID {
					after.InboundIDs = append(after.InboundIDs, id)
				}
			}
			if _, e = s.EntryGroups.SaveGroup(ctx, group.ID, after); e != nil {
				return fail(e)
			}
			undo.add(func(ctx context.Context) error { _, e := s.EntryGroups.SaveGroup(ctx, group.ID, before); return e })
		}
	}
	if err = s.finishCanvasRemoval(ctx, plan, guards); err != nil {
		return fail(err)
	}
	result.Removed = true
	return result, nil
}

func (s *Service) canvasRemoveObjectState(ctx context.Context, object CanvasRemoveObject) (any, error) {
	switch object.Kind {
	case "inbound":
		return s.Repository.Inbound(ctx, object.ID, true)
	case "outbound":
		return s.Repository.Outbound(ctx, object.ID, true)
	case "chain":
		return s.Repository.Chain(ctx, object.ID)
	}
	return nil, errors.New("invalid canvas object kind")
}

func (s *Service) checkCanvasRemovalEntries(ctx context.Context, plan CanvasRemovePlan) error {
	for _, entry := range plan.Entries {
		current, err := s.Repository.Inbound(ctx, entry.InboundID, true)
		if err != nil {
			return err
		}
		expected := entry.inbound
		if entry.Mode == "create" {
			expected.Enabled = false
		} else {
			expected.DefaultOutbound = entry.RestoreTarget
		}
		if !reflect.DeepEqual(current, expected) {
			return fmt.Errorf("入口 %s 在撤除期间配置发生变化，请刷新影响预览", entry.InboundID)
		}
		groups, err := s.canvasEntryGroups(ctx, entry.InboundID, false)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(groups, entry.Groups) {
			return fmt.Errorf("入口 %s 在撤除期间分组发生变化，请刷新影响预览", entry.InboundID)
		}
	}
	return nil
}

func canvasRemoveTable(kind string) string {
	switch kind {
	case "inbound":
		return "inbounds"
	case "outbound":
		return "outbounds"
	case "chain":
		return "proxy_chains"
	}
	panic("invalid canvas object kind")
}

func (s *Service) publishCanvasRemovalPhase(ctx context.Context, deploy CanvasRemovalDeployer, name, author string, nodes []string, users AccessReader) (CanvasRemovePublication, error) {
	snapshots := make([]NodeSnapshot, 0, len(nodes))
	names := map[string]string{}
	for _, nodeID := range nodes {
		n, err := s.Nodes.Get(ctx, nodeID)
		if err != nil {
			return CanvasRemovePublication{}, err
		}
		names[nodeID] = n.Name
		state, intent, err := s.SnapshotIntent(ctx, nodeID, users)
		if err != nil {
			return CanvasRemovePublication{}, err
		}
		snapshots = append(snapshots, NodeSnapshot{State: state, InboundIntent: intent})
	}
	publication, err := deploy.PublishCanvasRemoval(ctx, "撤除方案 "+name, author, snapshots)
	for i := range publication.Nodes {
		publication.Nodes[i].Name = names[publication.Nodes[i].NodeID]
	}
	return publication, err
}

func (s *Service) finishCanvasRemoval(ctx context.Context, plan CanvasRemovePlan, guards []canvasRemovalRow) error {
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkCanvasRemovalRows(ctx, tx, guards); err != nil {
		return err
	}
	for _, entry := range plan.Entries {
		if entry.Mode != "create" {
			continue
		}
		var members int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM node_group_inbounds WHERE inbound_id=?", entry.InboundID).Scan(&members); err != nil {
			return err
		}
		if members != 0 {
			return fmt.Errorf("入口 %s 在最终删除前新增了分组成员，请刷新影响预览", entry.InboundID)
		}
	}
	// Bindings must go first because their inbound FK intentionally restricts deletion.
	if _, err = tx.ExecContext(ctx, "DELETE FROM canvas_entry_bindings WHERE scheme_id=?", plan.SchemeID); err != nil {
		return err
	}
	for _, entry := range plan.Entries {
		if entry.Mode == "create" {
			if _, err = tx.ExecContext(ctx, "DELETE FROM inbounds WHERE id=?", entry.InboundID); err != nil {
				return err
			}
		}
	}
	for _, kind := range []string{"chain", "outbound", "inbound"} {
		for _, object := range plan.Objects {
			if object.Keep || object.Kind != kind {
				continue
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM "+canvasRemoveTable(kind)+" WHERE id=?", object.ID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "DELETE FROM canvas_tunnel_objects WHERE object_kind=? AND object_id=?", kind, object.ID); err != nil {
				return err
			}
		}
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM canvas_schemes WHERE id=? AND updated_at=?", plan.SchemeID, plan.UpdatedAt)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrStaleScheme
	}
	for _, tunnelID := range plan.unsharedTunnelIDs {
		var objects int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM canvas_tunnel_objects WHERE tunnel_id=?", tunnelID).Scan(&objects); err != nil {
			return err
		}
		if objects == 0 {
			if err := deleteCanvasTunnelRow(ctx, tx, tunnelID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
