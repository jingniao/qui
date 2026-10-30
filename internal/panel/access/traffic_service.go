package access

import (
	"context"
	"errors"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"proxycontrol/pkg/contracts/traffic"
	"strings"
	"time"
)

func (s *Service) RecordTraffic(ctx context.Context, nodeID string, message *pb.TrafficBatch, allowNew bool) (string, error) {
	if message == nil {
		return "", ErrInvalid
	}
	batch, err := traffic.Decode(message.BatchJson)
	if err != nil {
		return "", err
	}
	if batch.NodeID != nodeID {
		return "", errors.New("traffic batch node mismatch")
	}
	if err := s.recordDecodedTraffic(ctx, batch, allowNew); err != nil {
		return "", err
	}
	return batch.ID, nil
}

func (s *Service) recordDecodedTraffic(ctx context.Context, batch traffic.Batch, allowNew bool) error {
	nodeID := batch.NodeID
	accepted, err := s.Repository.TrafficReceipt(ctx, batch)
	if err != nil {
		return trafficStageError("statistics_pending", err)
	}
	if !accepted && !allowNew {
		return errors.New("stale sequence cannot create traffic batch")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	states := map[int64]desiredstate.State{}
	if !accepted {
		for _, counter := range batch.Counters {
			if counter.ObservedAt.After(now.Add(2 * time.Minute)) {
				return errors.New("traffic sample time is in the future")
			}
			state, err := s.trafficState(ctx, nodeID, counter)
			if err != nil {
				return trafficStageError("source_unavailable", err)
			}
			states[counter.Revision] = state
			if counter.Kind == "node" {
				if counter.UploadBytes > 1<<50 || counter.DownloadBytes > 1<<50 || !strings.HasSuffix(counter.CoreEpoch, ".node") {
					return ErrInvalid
				}
				continue
			}
			matched := false
			for _, credential := range state.Credentials {
				if credential.ID == counter.CredentialID && !credential.Internal && credential.UserID == counter.UserID && credential.DeviceID == counter.DeviceID && credential.Protocol == counter.Protocol {
					for _, inbound := range state.Inbounds {
						if inbound.Enabled && inbound.Protocol == counter.Protocol {
							for _, id := range inbound.CredentialIDs {
								if id == counter.CredentialID {
									matched = true
								}
							}
						}
					}
				}
			}
			if !matched {
				return trafficStageError("identity_conflict", ErrInvalid)
			}
		}
		if s.Repository.Vault != nil {
			if err := s.Repository.SavePendingTraffic(ctx, batch, now); err != nil {
				return trafficStageError("journal_unavailable", err)
			}
		}
		if err := s.Repository.IngestTraffic(ctx, batch, now); err != nil {
			return trafficStageError("statistics_pending", err)
		}
	}
	hasAccounts := false
	for _, c := range batch.Counters {
		if c.Kind != "node" {
			hasAccounts = true
		}
	}
	if !hasAccounts {
		if s.Repository.Vault != nil {
			return s.Repository.CompletePendingTraffic(ctx, batch)
		}
		return nil
	}
	if s.Users == nil {
		return errors.New("user traffic recorder unavailable")
	}
	if recorder, ok := s.Users.(interface {
		ApplyTrafficWithStates(context.Context, traffic.Batch, map[int64]desiredstate.State) error
	}); ok {
		if accepted {
			for _, counter := range batch.Counters {
				state, err := s.trafficState(ctx, nodeID, counter)
				if err != nil {
					return trafficStageError("source_unavailable", err)
				}
				states[counter.Revision] = state
			}
		}
		if accepted && s.Repository.Vault != nil {
			if err := s.Repository.SavePendingTraffic(ctx, batch, now); err != nil {
				return trafficStageError("journal_unavailable", err)
			}
		}
		if err := recorder.ApplyTrafficWithStates(ctx, batch, states); err != nil {
			return s.trafficAccountError(err)
		}
	} else {
		if accepted && s.Repository.Vault != nil {
			if err := s.Repository.SavePendingTraffic(ctx, batch, now); err != nil {
				return trafficStageError("journal_unavailable", err)
			}
		}
		if err := s.Users.ApplyTraffic(ctx, batch); err != nil {
			return s.trafficAccountError(err)
		}
	}
	if s.Repository.Vault != nil {
		if err := s.Repository.CompletePendingTraffic(ctx, batch); err != nil {
			return trafficStageError("journal_unavailable", err)
		}
	}
	return nil
}

func (s *Service) trafficState(ctx context.Context, nodeID string, counter traffic.Counter) (desiredstate.State, error) {
	if counter.Kind == "node" {
		return s.Snapshots.AppliedState(ctx, nodeID, counter.Revision)
	}
	if source, ok := s.Snapshots.(interface {
		TrafficState(context.Context, string, traffic.Counter) (desiredstate.State, error)
	}); ok {
		return source.TrafficState(ctx, nodeID, counter)
	}
	return s.Snapshots.AppliedState(ctx, nodeID, counter.Revision)
}
