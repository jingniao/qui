package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/id"
	"proxycontrol/pkg/contracts/desiredstate"
	"strings"
)

type Chain struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	NodeIDs         []string   `json:"nodeIds"`
	OutboundIDs     []string   `json:"outboundIds"`
	Hops            []ChainHop `json:"hops"`
	FinalOutboundID string     `json:"finalOutboundId"`
	SupportTCP      bool       `json:"supportTcp"`
	SupportUDP      bool       `json:"supportUdp"`
	Enabled         bool       `json:"enabled"`
	FailurePolicy   string     `json:"failurePolicy"`
	BackupTargetID  string     `json:"backupTargetId"`
	CreatedAt       string     `json:"createdAt"`
}
type ChainHop struct {
	Position        int    `json:"position"`
	SourceNodeID    string `json:"sourceNodeId"`
	TargetNodeID    string `json:"targetNodeId"`
	OutboundID      string `json:"outboundId"`
	TargetInboundID string `json:"targetInboundId"`
	CredentialID    string `json:"credentialId"`
	Protocol        string `json:"protocol"`
	FailurePolicy   string `json:"failurePolicy"`
}
type ChainInput struct {
	Name            string   `json:"name"`
	NodeIDs         []string `json:"nodeIds"`
	OutboundIDs     []string `json:"outboundIds"`
	FinalOutboundID string   `json:"finalOutboundId"`
	SupportTCP      bool     `json:"supportTcp"`
	SupportUDP      bool     `json:"supportUdp"`
	Enabled         bool     `json:"enabled"`
	FailurePolicy   string   `json:"failurePolicy"`
	BackupTargetID  string   `json:"backupTargetId"`
}

func (c Chain) Desired() desiredstate.ProxyChain {
	return desiredstate.ProxyChain{ID: c.ID, NodeIDs: c.NodeIDs, OutboundIDs: c.OutboundIDs, Failover: c.FailurePolicy, FinalOutboundID: c.FinalOutboundID, BackupTargetID: c.BackupTargetID}
}
func (r *Repository) Chain(ctx context.Context, chainID string) (Chain, error) {
	chain := Chain{NodeIDs: []string{}, OutboundIDs: []string{}, Hops: []ChainHop{}}
	var nodes string
	err := r.Database.QueryRowContext(ctx, "SELECT id,name,node_ids_json,final_outbound_id,support_tcp,support_udp,enabled,CASE WHEN failure_policy_v2='' THEN failure_policy ELSE failure_policy_v2 END,backup_target_id,created_at FROM proxy_chains WHERE id=?", chainID).Scan(&chain.ID, &chain.Name, &nodes, &chain.FinalOutboundID, &chain.SupportTCP, &chain.SupportUDP, &chain.Enabled, &chain.FailurePolicy, &chain.BackupTargetID, &chain.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return chain, ErrNotFound
	}
	if err != nil {
		return chain, err
	}
	if err := json.Unmarshal([]byte(nodes), &chain.NodeIDs); err != nil {
		return chain, err
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT position,source_node_id,target_node_id,outbound_id,target_inbound_id,credential_id,protocol,failure_policy FROM proxy_chain_hops WHERE chain_id=? ORDER BY position", chainID)
	if err != nil {
		return chain, err
	}
	defer rows.Close()
	for rows.Next() {
		var hop ChainHop
		if err := rows.Scan(&hop.Position, &hop.SourceNodeID, &hop.TargetNodeID, &hop.OutboundID, &hop.TargetInboundID, &hop.CredentialID, &hop.Protocol, &hop.FailurePolicy); err != nil {
			return chain, err
		}
		chain.Hops = append(chain.Hops, hop)
		chain.OutboundIDs = append(chain.OutboundIDs, hop.OutboundID)
	}
	return chain, rows.Err()
}
func (r *Repository) Chains(ctx context.Context, nodeID string, page, size int) ([]Chain, int, error) {
	filter := ""
	args := []any{}
	if nodeID != "" {
		filter = " WHERE EXISTS (SELECT 1 FROM json_each(node_ids_json) WHERE value=?)"
		args = append(args, nodeID)
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM proxy_chains"+filter, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	args = append(args, size, (page-1)*size)
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM proxy_chains"+filter+" ORDER BY created_at,id LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	chains := []Chain{}
	for _, id := range ids {
		chain, err := r.Chain(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		chains = append(chains, chain)
	}
	return chains, count, nil
}
func supports(n node.Node, protocol string, inbound, tcp, udp bool) bool {
	for _, ability := range n.Capabilities.Protocols {
		if ability.Protocol == protocol && (!inbound || ability.Inbound) && (inbound || ability.Outbound) && (!tcp || ability.Tcp) && (!udp || ability.Udp) {
			return true
		}
	}
	return false
}
func chainInput(chain Chain) ChainInput {
	return ChainInput{Name: chain.Name, NodeIDs: chain.NodeIDs, OutboundIDs: chain.OutboundIDs, FinalOutboundID: chain.FinalOutboundID, SupportTCP: chain.SupportTCP, SupportUDP: chain.SupportUDP, Enabled: chain.Enabled, FailurePolicy: chain.FailurePolicy, BackupTargetID: chain.BackupTargetID}
}

func (s *Service) SaveChain(ctx context.Context, chainID string, input ChainInput) (Chain, error) {
	chain, err := s.validateChain(ctx, chainID, input)
	if err != nil {
		return Chain{}, err
	}
	if err := s.checkSchedulingChange(ctx, chain.NodeIDs[0], chain.ID, chain); err != nil {
		return Chain{}, err
	}
	return s.Repository.saveChain(ctx, chain.ID, chainInput(chain), chain.Hops, chainID == "")
}

func (s *Service) validateChain(ctx context.Context, chainID string, input ChainInput) (Chain, error) {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 120 || len(input.NodeIDs) < 2 || len(input.NodeIDs) > 3 || len(input.OutboundIDs) != len(input.NodeIDs)-1 || !input.SupportTCP && !input.SupportUDP {
		return Chain{}, errors.New("chain requires a name, two or three nodes, matching hops and a transport capability")
	}
	if input.FinalOutboundID == "" {
		input.FinalOutboundID = "direct"
	}
	if input.FailurePolicy == "" {
		input.FailurePolicy = "Block"
	}
	if !desiredstate.ValidFailover(input.FailurePolicy) {
		return Chain{}, errors.New("invalid chain failure policy")
	}
	if strings.HasPrefix(input.FailurePolicy, "Backup") && input.BackupTargetID == "" {
		return Chain{}, errors.New("chain backup target required")
	}
	create := chainID == ""
	if create {
		var err error
		chainID, err = id.New()
		if err != nil {
			return Chain{}, err
		}
	} else {
		if _, err := s.Repository.Chain(ctx, chainID); err != nil {
			return Chain{}, err
		}
	}
	seen := map[string]bool{}
	nodes := map[string]node.Node{}
	for _, nodeID := range input.NodeIDs {
		if seen[nodeID] {
			return Chain{}, errors.New("chain has duplicate node or loop")
		}
		seen[nodeID] = true
		n, err := s.Nodes.Get(ctx, nodeID)
		if err != nil || !n.Enabled {
			return Chain{}, errors.New("chain node missing or disabled")
		}
		nodes[nodeID] = n
	}
	hops := []ChainHop{}
	for index, outboundID := range input.OutboundIDs {
		outbound, err := s.Repository.Outbound(ctx, outboundID, false)
		if err != nil || !outbound.Enabled || outbound.NodeID != input.NodeIDs[index] || outbound.TargetNodeID != input.NodeIDs[index+1] || outbound.TargetInboundID == "" || outbound.CredentialID == "" {
			return Chain{}, errors.New("chain hop does not match source, target or internal credential")
		}
		target, err := s.Repository.Inbound(ctx, outbound.TargetInboundID, false)
		if err != nil || !target.Enabled || !target.InternalRelay || target.Protocol != outbound.Protocol {
			return Chain{}, errors.New("chain target inbound invalid")
		}
		if input.SupportTCP && !outbound.SupportTCP || input.SupportUDP && !outbound.SupportUDP || !supports(nodes[outbound.NodeID], outbound.Protocol, false, input.SupportTCP, input.SupportUDP) || !supports(nodes[outbound.TargetNodeID], outbound.Protocol, true, input.SupportTCP, input.SupportUDP) {
			return Chain{}, errors.New("chain protocol or TCP/UDP capability mismatch")
		}
		bindings, err := s.Repository.Bindings(ctx, target.ID)
		if err != nil {
			return Chain{}, err
		}
		if len(bindings) != 1 || bindings[0].OutboundID != outboundID {
			return Chain{}, errors.New("chain hop requires a dedicated internal inbound binding")
		}
		references, err := s.Repository.ChainInboundReferences(ctx, target.ID, chainID)
		if err != nil {
			return Chain{}, err
		}
		if references > 0 {
			return Chain{}, errors.New("internal chain inbound already belongs to another chain")
		}
		credential, err := s.Repository.Credential(ctx, outbound.CredentialID)
		if err != nil || !credential.Internal {
			return Chain{}, errors.New("chain credential unavailable")
		}
		hops = append(hops, ChainHop{Position: index, SourceNodeID: outbound.NodeID, TargetNodeID: outbound.TargetNodeID, OutboundID: outbound.ID, TargetInboundID: target.ID, CredentialID: credential.ID, Protocol: outbound.Protocol, FailurePolicy: outbound.FailurePolicy})
	}
	exit := input.NodeIDs[len(input.NodeIDs)-1]
	if input.FinalOutboundID != "direct" && input.FinalOutboundID != "block" {
		final, err := s.Repository.Outbound(ctx, input.FinalOutboundID, false)
		if err != nil || !final.Enabled || final.NodeID != exit || final.TargetNodeID != "" {
			return Chain{}, errors.New("chain final outbound missing on exit node")
		}
		if input.SupportTCP && !final.SupportTCP || input.SupportUDP && !final.SupportUDP {
			return Chain{}, errors.New("chain final transport mismatch")
		}
	}
	if err := s.validateBackup(ctx, input.NodeIDs[0], input.FailurePolicy, input.BackupTargetID, input.SupportTCP, input.SupportUDP); err != nil {
		return Chain{}, err
	}
	if input.FailurePolicy == "BackupOutbound" {
		backup, err := s.Repository.Outbound(ctx, input.BackupTargetID, false)
		if err != nil || !backup.Enabled || backup.NodeID != input.NodeIDs[0] || input.SupportTCP && !backup.SupportTCP || input.SupportUDP && !backup.SupportUDP {
			return Chain{}, errors.New("chain backup outbound must belong to entry node")
		}
	}
	if input.FailurePolicy == "BackupChain" {
		backup, err := s.Repository.Chain(ctx, input.BackupTargetID)
		if err != nil || backup.ID == chainID || !backup.Enabled || backup.NodeIDs[0] != input.NodeIDs[0] || input.SupportTCP && !backup.SupportTCP || input.SupportUDP && !backup.SupportUDP {
			return Chain{}, errors.New("invalid backup chain")
		}
		seen := map[string]bool{chainID: true}
		for backup.FailurePolicy == "BackupChain" {
			if seen[backup.ID] {
				return Chain{}, errors.New("backup chain cycle")
			}
			seen[backup.ID] = true
			backup, err = s.Repository.Chain(ctx, backup.BackupTargetID)
			if err != nil || !backup.Enabled {
				return Chain{}, errors.New("backup chain unavailable")
			}
		}
		if seen[backup.ID] {
			return Chain{}, errors.New("backup chain cycle")
		}
	}
	return Chain{ID: chainID, Name: input.Name, NodeIDs: input.NodeIDs, OutboundIDs: input.OutboundIDs, Hops: hops, FinalOutboundID: input.FinalOutboundID, SupportTCP: input.SupportTCP, SupportUDP: input.SupportUDP, Enabled: input.Enabled, FailurePolicy: input.FailurePolicy, BackupTargetID: input.BackupTargetID}, nil
}

func (r *Repository) ChainInboundReferences(ctx context.Context, inboundID, exceptChain string) (int, error) {
	var count int
	err := r.Database.QueryRowContext(ctx, "SELECT count(*) FROM proxy_chain_hops WHERE target_inbound_id=? AND chain_id<>?", inboundID, exceptChain).Scan(&count)
	return count, err
}

// ChainSnapshots revalidates the saved topology against current capabilities
// and bindings, then returns immutable inputs in exit-to-entry publication order.
func (s *Service) ChainSnapshots(ctx context.Context, chainID string, users AccessReader) ([]desiredstate.State, error) {
	chain, err := s.Repository.Chain(ctx, chainID)
	if err != nil {
		return nil, err
	}
	if !chain.Enabled {
		return nil, errors.New("chain is disabled")
	}
	if _, err := s.validateChain(ctx, chainID, chainInput(chain)); err != nil {
		return nil, err
	}
	states := make([]desiredstate.State, 0, len(chain.NodeIDs))
	// A node snapshot includes all enabled proxy configuration. Include its
	// entire downstream dependency graph, including alternate chains, so the
	// first publication of two mutually referenced drafts cannot deadlock.
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(nodeID string) error {
		if visiting[nodeID] {
			return errors.New("deployment dependency cycle")
		}
		if done[nodeID] {
			return nil
		}
		if len(done)+len(visiting) >= 64 {
			return errors.New("deployment graph exceeds 64 nodes")
		}
		visiting[nodeID] = true
		outbounds, err := snapshotList(func(page, size int) ([]Outbound, int, error) {
			return s.Repository.Outbounds(ctx, nodeID, page, size, false)
		})
		if err != nil {
			return err
		}
		for _, out := range outbounds {
			if out.Enabled && out.TargetNodeID != "" {
				if err := visit(out.TargetNodeID); err != nil {
					return err
				}
			}
		}
		state, err := s.Snapshot(ctx, nodeID, users)
		if err != nil {
			return err
		}
		states = append(states, state)
		delete(visiting, nodeID)
		done[nodeID] = true
		return nil
	}
	if err := visit(chain.NodeIDs[0]); err != nil {
		return nil, err
	}
	return states, nil
}
func (r *Repository) saveChain(ctx context.Context, chainID string, input ChainInput, hops []ChainHop, create bool) (Chain, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Chain{}, err
	}
	defer tx.Rollback()
	nodes, _ := json.Marshal(input.NodeIDs)
	if create {
		_, err = tx.ExecContext(ctx, "INSERT INTO proxy_chains(id,name,node_ids_json,final_outbound_id,support_tcp,support_udp,enabled,failure_policy,backup_target_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", chainID, input.Name, string(nodes), input.FinalOutboundID, input.SupportTCP, input.SupportUDP, input.Enabled, legacyFailure(input.FailurePolicy, true), input.BackupTargetID, timestamp())
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE proxy_chains SET name=?,node_ids_json=?,final_outbound_id=?,support_tcp=?,support_udp=?,enabled=?,failure_policy=?,backup_target_id=? WHERE id=?", input.Name, string(nodes), input.FinalOutboundID, input.SupportTCP, input.SupportUDP, input.Enabled, legacyFailure(input.FailurePolicy, true), input.BackupTargetID, chainID)
	}
	if err != nil {
		return Chain{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE proxy_chains SET failure_policy_v2=? WHERE id=?", input.FailurePolicy, chainID); err != nil {
		return Chain{}, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM proxy_chain_hops WHERE chain_id=?", chainID); err != nil {
		return Chain{}, err
	}
	for _, hop := range hops {
		if _, err := tx.ExecContext(ctx, "INSERT INTO proxy_chain_hops(chain_id,position,source_node_id,target_node_id,outbound_id,target_inbound_id,credential_id,protocol,failure_policy) VALUES(?,?,?,?,?,?,?,?,?)", chainID, hop.Position, hop.SourceNodeID, hop.TargetNodeID, hop.OutboundID, hop.TargetInboundID, hop.CredentialID, hop.Protocol, hop.FailurePolicy); err != nil {
			return Chain{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Chain{}, err
	}
	return r.Chain(ctx, chainID)
}
func (r *Repository) DeleteChain(ctx context.Context, chainID string) error {
	if _, err := r.Chain(ctx, chainID); err != nil {
		return err
	}
	var count int
	if err := r.Database.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM inbounds WHERE default_outbound=? OR default_outbound=?) +
 (SELECT count(*) FROM proxy_chains WHERE backup_target_id=?) +
 (SELECT count(*) FROM outbound_groups WHERE backup_outbound_id=?) +
 (SELECT count(*) FROM outbounds WHERE backup_outbound_id=?)`, chainID, "chain:"+chainID, chainID, chainID, chainID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return errors.New("chain is referenced by an inbound or as a backup")
	}
	_, err := r.Database.ExecContext(ctx, "DELETE FROM proxy_chains WHERE id=?", chainID)
	return err
}
