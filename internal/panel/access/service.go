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
		return "", errors.New("access batch node identity mismatch")
	}
	accepted, err := s.Repository.Receipt(ctx, batch)
	if err != nil {
		return "", err
	}
	if accepted {
		return batch.ID, nil
	}
	if !allowNew {
		return "", errors.New("stale sequence cannot create an access batch")
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
			return "", errors.New("access time is in the future")
		}
		key := attribution{record.Revision, record.UserID, record.DeviceID, record.InboundProtocol}
		state, known := snapshots[key]
		if !known {
			state, err = s.Snapshots.AppliedState(ctx, nodeID, record.Revision)
			if err != nil {
				return "", errors.New("access revision was not applied by authenticated node")
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
			return "", errors.New("access inbound protocol not present in applied snapshot")
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
				return "", errors.New("access user or device not authorized on this revision")
			}
		}
		if record.RuleID != "" {
			found := false
			for _, rule := range state.Policies {
				if rule.ID == record.RuleID {
					if rule.Logging != nil && (!rule.Logging.LogDomain && record.Domain != "" || !rule.Logging.LogTraffic && record.TrafficKnown) {
						return "", errors.New("access logging disabled by applied rule")
					}
					found = true
				}
			}
			if !found {
				return "", errors.New("access rule not present in applied snapshot")
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
				return "", errors.New("access outbound not present in applied snapshot")
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
				return "", errors.New("access chain not present in applied snapshot")
			}
		}
		if record.EgressKnown {
			// Topology describes a plan, not a data-plane exit observation.
			// The current telemetry format can prove only local direct routing;
			// remote proxy/chain exits require a future correlated observation.
			if record.Action != "Direct" || record.OutboundProtocol != "direct" || record.FinalEgressNodeID != nodeID || record.ChainID != "" {
				return "", errors.New("access final exit requires observed local direct routing")
			}
			for _, outbound := range state.Outbounds {
				if outbound.ID == record.OutboundID && (!outbound.Enabled || outbound.Protocol != "direct") {
					return "", errors.New("access direct exit disagrees with applied outbound")
				}
			}
		}
	}
	if err := s.Repository.Ingest(ctx, batch, now); err != nil {
		return "", err
	}
	return batch.ID, nil
}
