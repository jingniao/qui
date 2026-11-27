package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/platform/id"
)

// Only non-secret identities remain outside the Vault so reservations can be
// checked within other modules' deployment transactions.
type tunnelPublicationEnvelope struct {
	Phase  string      `json:"phase"`
	Nodes  []string    `json:"nodes"`
	Owners []PortOwner `json:"owners"`
	Sealed []byte      `json:"sealed"`
}
type tunnelPublicationObject struct {
	ID      string
	Status  string
	Version int64
}
type tunnelPublicationInbound struct {
	ID    string
	Owner PortOwner
	Input InboundInput
}
type tunnelPublicationRecord struct {
	Tunnels  []tunnelPublicationObject
	Rules    []tunnelPublicationObject
	Previous []tunnelPublicationInbound
	Desired  []tunnelPublicationInbound
	Relays   map[string]published.Relay
}

func (r *Repository) encodeTunnelPublication(p tunnelPublication) ([]byte, error) {
	record := tunnelPublicationRecord{Relays: p.relays}
	envelope := tunnelPublicationEnvelope{Phase: "preparing"}
	for _, server := range p.view.Servers {
		envelope.Nodes = append(envelope.Nodes, server.NodeID)
	}
	for _, t := range p.tunnels {
		record.Tunnels = append(record.Tunnels, tunnelPublicationObject{t.ID, t.Status, t.DraftVersion})
		envelope.Owners = append(envelope.Owners, PortOwner{Kind: "tunnel", ID: t.ID})
	}
	for _, rule := range p.rules {
		record.Rules = append(record.Rules, tunnelPublicationObject{rule.ID, rule.Status, rule.DraftVersion})
		envelope.Owners = append(envelope.Owners, PortOwner{Kind: "rule", ID: rule.ID})
	}
	for _, in := range p.previous {
		record.Previous = append(record.Previous, tunnelPublicationInbound{in.id, in.owner, in.input})
	}
	for _, in := range p.desired {
		record.Desired = append(record.Desired, tunnelPublicationInbound{in.id, in.owner, in.input})
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	envelope.Sealed, err = r.Vault.Seal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

func (r *Repository) decodeTunnelPublication(raw []byte) (tunnelPublication, error) {
	p := tunnelPublication{}
	var envelope tunnelPublicationEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return p, err
	}
	data, err := r.Vault.Open(envelope.Sealed)
	if err != nil {
		return p, err
	}
	var record tunnelPublicationRecord
	if err = json.Unmarshal(data, &record); err != nil {
		return p, err
	}
	p.relays = record.Relays
	for _, t := range record.Tunnels {
		p.tunnels = append(p.tunnels, Tunnel{ID: t.ID, Status: t.Status, DraftVersion: t.Version})
	}
	for _, rule := range record.Rules {
		p.rules = append(p.rules, TunnelRule{ID: rule.ID, Status: rule.Status, DraftVersion: rule.Version})
	}
	for _, in := range record.Previous {
		p.previous = append(p.previous, managedInbound{id: in.ID, owner: in.Owner, input: in.Input})
	}
	for _, in := range record.Desired {
		p.desired = append(p.desired, managedInbound{id: in.ID, owner: in.Owner, input: in.Input})
	}
	return p, nil
}

// Called under canvasMu. The journal and the materialized listeners commit
// together, before any revision can be dispatched to an Agent.
func (s *Service) prepareTunnelPublication(ctx context.Context, selection PublishSelection, author string, users AccessReader, deploy TunnelDeployer) (tunnelPublication, string, []NodeSnapshot, error) {
	p, err := s.buildTunnelPublication(ctx, selection)
	if err != nil {
		return p, "", nil, err
	}
	if !p.view.OK {
		return p, "", nil, tunnelFail("port_in_use", 409, p.view.Message)
	}
	if len(p.view.InstallGost) > 0 {
		ids := []string{}
		for _, item := range p.view.InstallGost {
			ids = append(ids, item.NodeID)
		}
		return p, "", nil, &tunnelError{Code: "gost_install_required", Status: 409, Message: "服务器 " + p.view.InstallGost[0].Name + " 需要先安装 GOST 3.3.2", Details: map[string]any{"nodeIds": ids}}
	}
	for _, t := range p.tunnels {
		if err = s.Repository.tunnelPublicationEditable(ctx, "tunnel", t.ID); err != nil {
			return p, "", nil, err
		}
	}
	for _, rule := range p.rules {
		if err = s.Repository.tunnelPublicationEditable(ctx, "rule", rule.ID); err != nil {
			return p, "", nil, err
		}
	}
	for i := range p.desired {
		if p.desired[i].id == "" {
			p.desired[i].id, err = id.New()
			if err != nil {
				return p, "", nil, err
			}
		}
	}
	raw, err := s.Repository.encodeTunnelPublication(p)
	if err != nil {
		return p, "", nil, err
	}
	batchID, err := id.New()
	if err != nil {
		return p, "", nil, err
	}
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return p, "", nil, err
	}
	defer tx.Rollback()
	// Reserve the entire plan, including nodes that snapshot pruning may later
	// omit. Check and persist the reservation in this same write transaction,
	// before any generated listener can enter another publication's snapshot.
	nodes := make([]string, 0, len(p.view.Servers))
	for _, server := range p.view.Servers {
		nodes = append(nodes, server.NodeID)
	}
	if err = deploy.CheckTunnelPublicationNodes(ctx, tx, nodes); err != nil {
		return p, "", nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO deployment_batches(id,chain_id,name,author,status,created_at,tunnel_publication_json,tunnel_finalized) VALUES(?,'','隧道与规则发布',?,'draft',?,?,0)", batchID, author, timestamp(), string(raw)); err != nil {
		return p, "", nil, err
	}
	if err = s.Repository.replaceManagedInboundsTx(ctx, tx, p.previous, p.desired); err != nil {
		return p, "", nil, err
	}
	if err = s.Repository.commitDraft(tx); err != nil {
		return p, "", nil, err
	}
	snapshots := []NodeSnapshot{}
	for _, server := range p.view.Servers {
		state, intent, e := s.snapshotIntent(ctx, server.NodeID, users, p.relayTargets)
		if e != nil {
			return p, batchID, nil, e
		}
		snapshots = append(snapshots, NodeSnapshot{State: state, InboundIntent: intent, ExpandedOnly: true})
	}
	return p, batchID, snapshots, nil
}

func (r *Repository) tunnelPublicationEditable(ctx context.Context, kind, objectID string) error {
	if objectID == "" {
		return nil
	}
	var batchID string
	err := r.Database.QueryRowContext(ctx, `SELECT b.id FROM deployment_batches b,json_each(b.tunnel_publication_json,'$.owners') o WHERE b.tunnel_finalized=0 AND json_extract(o.value,'$.kind')=? AND json_extract(o.value,'$.id')=? LIMIT 1`, kind, objectID).Scan(&batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &tunnelError{Code: "tunnel_publishing", Status: 409, Message: "发布仍在进行，请等待发布完成", Details: map[string]any{"batchId": batchID}}
}

func (s *Service) abortTunnelPreparation(ctx context.Context, batchID string) error {
	if _, err := s.Repository.Database.ExecContext(ctx, "UPDATE deployment_batches SET status='failed' WHERE id=? AND status='draft' AND tunnel_finalized=0", batchID); err != nil {
		return err
	}
	_, err := s.FinalizeTunnelPublication(ctx, batchID)
	return err
}

// FinalizeTunnelPublication commits the business outcome and its completion bit
// atomically. A running batch can never restore the before image.
func (s *Service) FinalizeTunnelPublication(ctx context.Context, batchID string) (bool, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	r := s.Repository
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var raw []byte
	var status string
	var finalized bool
	if err = tx.QueryRowContext(ctx, "SELECT status,tunnel_publication_json,tunnel_finalized FROM deployment_batches WHERE id=?", batchID).Scan(&status, &raw, &finalized); err != nil {
		return false, err
	}
	if finalized {
		return true, nil
	}
	if status != "success" && status != "failed" && status != "rolled_back" && status != "rollback_failed" {
		return false, nil
	}
	p, err := r.decodeTunnelPublication(raw)
	if err != nil {
		return false, err
	}
	if status == "success" {
		err = r.finishTunnelPublicationTx(ctx, tx, p)
	} else {
		err = r.replaceManagedInboundsTx(ctx, tx, p.desired, p.previous)
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE deployment_batches SET tunnel_finalized=1 WHERE id=? AND tunnel_finalized=0", batchID); err != nil {
		return false, err
	}
	if err = r.commitDraft(tx); err != nil {
		return false, err
	}
	return true, nil
}

// startup is called before listeners accept requests: only there is a draft
// preparation known to belong to a process that no longer exists.
func (s *Service) ReconcileTunnelPublications(ctx context.Context, startup bool) error {
	query := "SELECT id FROM deployment_batches WHERE tunnel_publication_json IS NOT NULL AND tunnel_finalized=0 AND status IN ('success','failed','rolled_back','rollback_failed') ORDER BY created_at,id"
	if startup {
		query = "SELECT id FROM deployment_batches WHERE tunnel_publication_json IS NOT NULL AND tunnel_finalized=0 ORDER BY created_at,id"
	}
	rows, err := s.Repository.Database.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var batchID string
		if err = rows.Scan(&batchID); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, batchID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var failures []error
	for _, batchID := range ids {
		if startup {
			err = s.abortTunnelPreparation(ctx, batchID)
		} else {
			_, err = s.FinalizeTunnelPublication(ctx, batchID)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("tunnel publication %s finalization failed: %w", batchID, err))
		}
	}
	return errors.Join(failures...)
}
