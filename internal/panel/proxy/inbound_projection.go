package proxy

import (
	"context"
	"errors"

	"proxycontrol/pkg/contracts/desiredstate"
)

// InboundDraft returns the owner-enabled listeners as they would be rendered,
// before credentials are attached. Only chain continuations can override their
// routing. This display read never allocates WireGuard addresses or reads users.
func (s *Service) InboundDraft(ctx context.Context, nodeID string) (desiredstate.State, []string, error) {
	var state desiredstate.State
	n, err := s.Nodes.GetForCoreSelection(ctx, nodeID)
	if err != nil {
		return state, nil, err
	}
	if !n.Enabled {
		return state, nil, errors.New("node unavailable")
	}
	rows, err := s.Repository.Database.QueryContext(ctx, `SELECT id,name,core,protocol,listen,port,enabled,default_outbound,subscription_visible,internal_relay,tls_source,coalesce(certificate_id,''),encrypted_settings
		FROM inbounds WHERE node_id=? ORDER BY created_at,id`, nodeID)
	if err != nil {
		return state, nil, err
	}
	var records []Inbound
	for rows.Next() {
		var record Inbound
		var encoded []byte
		if err := rows.Scan(&record.ID, &record.Name, &record.Core, &record.Protocol, &record.Listen, &record.Port, &record.Enabled,
			&record.DefaultOutbound, &record.SubscriptionVisible, &record.InternalRelay, &record.TLSSource, &record.CertificateID, &encoded); err != nil {
			rows.Close()
			return state, nil, err
		}
		record.Settings, err = s.Repository.decode(encoded)
		if err != nil {
			rows.Close()
			return state, nil, err
		}
		records = append(records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return state, nil, err
	}
	enabled := []string{}
	for _, record := range records {
		if err := s.Repository.injectCertificate(ctx, &record); err != nil {
			return state, nil, err
		}
		inbound, err := renderInboundBase(n, record)
		if err != nil {
			return state, nil, err
		}
		state.Inbounds = append(state.Inbounds, inbound)
		if record.Enabled {
			enabled = append(enabled, record.ID)
		}
	}
	if len(records) == 0 {
		return state, enabled, nil
	}
	chains, err := snapshotList(func(page, size int) ([]Chain, int, error) {
		return s.Repository.Chains(ctx, nodeID, page, size)
	})
	if err != nil || len(chains) == 0 {
		return state, enabled, err
	}
	// The continuation only needs each enabled outbound's identity and protocol.
	rows, err = s.Repository.Database.QueryContext(ctx, "SELECT id,protocol FROM outbounds WHERE node_id=? AND enabled=1", nodeID)
	if err != nil {
		return state, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var out desiredstate.Outbound
		if err := rows.Scan(&out.ID, &out.Protocol); err != nil {
			return state, nil, err
		}
		state.Outbounds = append(state.Outbounds, out)
	}
	if err := rows.Err(); err != nil {
		return state, nil, err
	}
	for _, chain := range chains {
		if chain.Enabled {
			applyChainContinuation(&state, nodeID, chain)
		}
	}
	state.Outbounds = nil
	return state, enabled, nil
}
