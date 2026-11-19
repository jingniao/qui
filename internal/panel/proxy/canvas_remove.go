package proxy

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"reflect"
)

type RelayCleanupNode struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
type CanvasRemoveResult struct {
	SchemeID     string             `json:"schemeId"`
	Removed      bool               `json:"removed"`
	PendingNodes []RelayCleanupNode `json:"pendingNodes"`
}

// Removal commits local ownership and durable cleanup work together. Agent
// availability never controls whether the scheme can leave the database.
func (s *Service) RemoveCanvasScheme(ctx context.Context, schemeID string, input CanvasRemoveInput, author string, users AccessReader) (CanvasRemoveResult, error) {
	s.canvasMu.Lock()
	result := CanvasRemoveResult{SchemeID: schemeID, PendingNodes: []RelayCleanupNode{}}
	plan, err := s.canvasRemovalScope(ctx, schemeID, input)
	if err == nil {
		var guards []canvasRemovalRow
		guards, err = s.canvasRemovalRows(ctx, plan)
		if err == nil {
			err = s.finishCanvasRemoval(ctx, plan, guards)
		}
	}
	s.canvasMu.Unlock()
	if err != nil {
		return result, err
	}
	result.Removed = true
	if s.RelayCleanupRequested != nil {
		// The transaction is already authoritative. A dispatcher failure leaves
		// durable work for the minute/startup scan and is recorded in panel logs.
		if err = s.RelayCleanupRequested(ctx); err != nil {
			slog.Error("relay cleanup dispatch failed", "scheme_id", schemeID, "error", err)
		}
	}
	for _, id := range plan.Nodes {
		var reason, code string
		err = s.Repository.Database.QueryRowContext(ctx, "SELECT reason,last_code FROM relay_cleanup_nodes WHERE node_id=?", id).Scan(&reason, &code)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return result, err
		}
		n, e := s.Nodes.Get(ctx, id)
		if e != nil {
			return result, e
		}
		if n.Status != "online" && n.Status != "degraded" || code != "" {
			result.PendingNodes = append(result.PendingNodes, RelayCleanupNode{NodeID: id, Name: n.Name, Reason: reason})
		}
	}
	return result, nil
}

func (s *Service) finishCanvasRemoval(ctx context.Context, plan CanvasRemovePlan, guards []canvasRemovalRow) error {
	// Guards were captured after the original ownership calculation. Recheck
	// that calculation now: a consumer added before guard capture must not be
	// accepted as a new baseline for deleting its provider.
	current, err := s.canvasRemovalScope(ctx, plan.SchemeID, CanvasRemoveInput{UpdatedAt: plan.UpdatedAt})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(plan, current) {
		return errors.New("撤除资源引用已变化，请刷新后重试")
	}
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkCanvasRemovalRows(ctx, tx, guards); err != nil {
		return err
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
	// The scheme FK detaches its bindings; delete them before their restricted
	// inbound FK. Group membership and relay-binding rows cascade with inbounds.
	for _, entry := range plan.Entries {
		if _, err = tx.ExecContext(ctx, "DELETE FROM canvas_entry_bindings WHERE inbound_id=?", entry.InboundID); err != nil {
			return err
		}
	}
	if err = deleteCanvasCleanupObjects(ctx, tx, plan.Objects); err != nil {
		return err
	}
	for _, entry := range plan.Entries {
		if _, err = tx.ExecContext(ctx, "DELETE FROM inbounds WHERE id=?", entry.InboundID); err != nil {
			return err
		}
	}
	for _, id := range plan.unsharedTunnelIDs {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM canvas_tunnel_objects WHERE tunnel_id=?", id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err = deleteCanvasTunnelRow(ctx, tx, id); err != nil {
				return err
			}
		}
	}
	for _, id := range plan.Nodes {
		if err = registerRelayCleanup(ctx, tx, id); err != nil {
			return err
		}
	}
	return s.Repository.commitDraft(tx)
}

func deleteCanvasCleanupObjects(ctx context.Context, tx *sql.Tx, objects []CanvasRemoveObject) error {
	credentials := map[string]bool{}
	for _, object := range objects {
		if object.Keep || object.Kind == "chain" {
			continue
		}
		column := "credential_id"
		if object.Kind == "inbound" {
			column = "relay_credential_id"
		}
		var credentialID string
		err := tx.QueryRowContext(ctx, "SELECT "+column+" FROM "+canvasRemoveTable(object.Kind)+" WHERE id=?", object.ID).Scan(&credentialID)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if credentialID != "" {
			credentials[credentialID] = true
		}
	}
	for _, kind := range []string{"chain", "outbound", "inbound"} {
		for _, object := range objects {
			if object.Keep || object.Kind != kind {
				continue
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+canvasRemoveTable(kind)+" WHERE id=?", object.ID); err != nil {
				return err
			}
		}
	}
	for id := range credentials {
		if _, err := tx.ExecContext(ctx, `DELETE FROM relay_credentials WHERE id=?
AND NOT EXISTS (SELECT 1 FROM inbounds WHERE relay_credential_id=relay_credentials.id)
AND NOT EXISTS (SELECT 1 FROM outbounds WHERE credential_id=relay_credentials.id)
AND NOT EXISTS (SELECT 1 FROM relay_bindings WHERE credential_id=relay_credentials.id)
AND NOT EXISTS (SELECT 1 FROM proxy_chain_hops WHERE credential_id=relay_credentials.id)`, id); err != nil {
			return err
		}
	}
	return nil
}

func registerRelayCleanup(ctx context.Context, tx *sql.Tx, nodeID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO relay_cleanup_nodes(node_id,reason,attempts,last_code,next_attempt_at,created_at,updated_at)
VALUES(?,'relay cleanup',0,'',?,?,?) ON CONFLICT(node_id) DO UPDATE SET
attempts=0,last_code='',next_attempt_at=excluded.next_attempt_at,updated_at=excluded.updated_at,
revision_id=CASE WHEN EXISTS(SELECT 1 FROM deployment_targets WHERE node_id=excluded.node_id AND revision=relay_cleanup_nodes.revision_id AND status='pending') THEN relay_cleanup_nodes.revision_id ELSE NULL END`, nodeID, timestamp(), timestamp(), timestamp())
	return err
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
