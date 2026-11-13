package access

import (
	"context"
	"errors"
	contract "proxycontrol/pkg/contracts/access"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

type AppliedSnapshots interface {
	AppliedState(context.Context, string, int64) (desiredstate.State, error)
}
type Service struct {
	Repository *Repository
	Snapshots  AppliedSnapshots
	Now        func() time.Time
	Users      interface {
		ApplyTraffic(context.Context, traffic.Batch) error
	}
}

func (s *Service) RecordAccess(ctx context.Context, nodeID string, message *pb.AccessBatch, allowNew bool) (string, error) {
	if message == nil {
		return "", ErrInvalid
	}
	batch, err := contract.DecodeBatch(message.BatchJson)
	if err != nil {
		return "", err
	}
	if batch.NodeID != nodeID {
		return "", reject("node_mismatch", 0)
	}
	accepted, err := s.Repository.Receipt(ctx, batch)
	if errors.Is(err, ErrConflict) {
		return "", &batchRejection{reason: "batch_conflict", cause: err}
	}
	if err != nil {
		return "", rejectStore("receipt_unavailable", err)
	}
	if accepted {
		return batch.ID, nil
	}
	if !allowNew {
		return "", reject("stale_sequence", 0)
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	type attribution struct {
		revision               int64
		user, device, protocol string
	}
	snapshots := map[attribution]desiredstate.State{}
	for _, record := range batch.Records {
		if record.Bucket.After(now.Add(2 * time.Minute)) {
			return "", reject("future_time", record.Revision)
		}
		key := attribution{record.Revision, record.UserID, record.DeviceID, record.InboundProtocol}
		state, known := snapshots[key]
		if !known {
			state, err = s.Snapshots.AppliedState(ctx, nodeID, record.Revision)
			if err != nil {
				return "", reject("revision_not_applied", record.Revision)
			}
			snapshots[key] = state
		}
		inboundFound := false
		for _, inbound := range state.Inbounds {
			if inbound.Enabled && inbound.Protocol == record.InboundProtocol {
				inboundFound = true
			}
		}
		if !inboundFound {
			return "", reject("inbound_unknown", record.Revision)
		}
		if record.UserID != "" {
			matched := false
			for _, credential := range state.Credentials {
				if !credential.Internal && credential.Protocol == record.InboundProtocol && credential.UserID == record.UserID && (record.DeviceID == "" || credential.DeviceID == record.DeviceID) {
					for _, inbound := range state.Inbounds {
						if !inbound.Enabled || inbound.Protocol != record.InboundProtocol {
							continue
						}
						for _, id := range inbound.CredentialIDs {
							if id == credential.ID {
								matched = true
							}
						}
					}
				}
			}
			if !matched {
				return "", reject("identity_conflict", record.Revision)
			}
		}
		if record.RuleID != "" {
			found := false
			for _, rule := range state.Policies {
				if rule.ID == record.RuleID {
					if rule.Logging != nil && (!rule.Logging.LogDomain && record.Domain != "" || !rule.Logging.LogTraffic && record.TrafficKnown) {
						return "", reject("logging_disabled", record.Revision)
					}
					found = true
				}
			}
			if !found {
				return "", reject("rule_unknown", record.Revision)
			}
		}
		if record.OutboundID != "" {
			found := false
			for _, outbound := range state.Outbounds {
				if outbound.ID == record.OutboundID {
					found = true
				}
			}
			if !found {
				return "", reject("outbound_unknown", record.Revision)
			}
		}
		if record.ChainID != "" {
			found := false
			for _, chain := range state.ProxyChains {
				if chain.ID == record.ChainID {
					found = true
				}
			}
			if !found {
				return "", reject("chain_unknown", record.Revision)
			}
		}
		if record.EgressKnown {
			// Topology describes a plan, not a data-plane exit observation.
			// The current telemetry format can prove only local direct routing;
			// remote proxy/chain exits require a future correlated observation.
			if record.Action != "Direct" || record.OutboundProtocol != "direct" || record.FinalEgressNodeID != nodeID || record.ChainID != "" {
				return "", reject("egress_unobserved", record.Revision)
			}
			for _, outbound := range state.Outbounds {
				if outbound.ID == record.OutboundID && (!outbound.Enabled || outbound.Protocol != "direct") {
					return "", reject("egress_conflict", record.Revision)
				}
			}
		}
	}
	if err := s.Repository.Ingest(ctx, batch, now); err != nil {
		return "", rejectStore("ingest_failed", err)
	}
	return batch.ID, nil
}
