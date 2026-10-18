package node

import (
	"errors"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"time"
)

func validateTrafficSourceStatus(s *pb.TrafficSourceStatus) error {
	if s == nil {
		return nil
	}
	if s.ObservedAt == nil || s.ObservedAt.CheckValid() != nil || s.ObservedAt.AsTime().After(time.Now().UTC().Add(2*time.Minute)) || s.ConfiguredSources > 1000000 || s.ObservedSources > s.ConfiguredSources {
		return errors.New("invalid traffic source observation")
	}
	if s.Status == "busy" || s.Status == "unavailable" {
		if s.CoreEpoch != "" || s.Revision != 0 || s.ConfiguredSources != 0 || s.ObservedSources != 0 || s.CoreRunning || s.LastSuccessAt != nil {
			return errors.New("unavailable traffic sources cannot declare coverage")
		}
		return nil
	}
	if s.CoreEpoch == "" || len(s.CoreEpoch) > 128 || s.CoreEpoch == "." || s.CoreEpoch == ".." || s.Revision < 1 {
		return errors.New("traffic source observation requires a process identity")
	}
	for _, r := range s.CoreEpoch {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.", r) {
			return errors.New("invalid traffic source epoch")
		}
	}
	if s.LastSuccessAt != nil && (s.LastSuccessAt.CheckValid() != nil || s.LastSuccessAt.AsTime().After(s.ObservedAt.AsTime().Add(time.Second))) {
		return errors.New("invalid traffic source sample time")
	}
	if s.LastSuccessAt == nil && s.ObservedSources != 0 {
		return errors.New("unobserved sources cannot declare counts")
	}
	switch s.Status {
	case "unobserved":
		if !s.CoreRunning || s.LastSuccessAt != nil {
			return errors.New("invalid unobserved traffic sources")
		}
	case "partial":
		if !s.CoreRunning || s.LastSuccessAt == nil || s.ObservedSources >= s.ConfiguredSources {
			return errors.New("invalid partial traffic sources")
		}
	case "covered":
		if !s.CoreRunning || s.LastSuccessAt == nil || s.ObservedSources != s.ConfiguredSources {
			return errors.New("invalid covered traffic sources")
		}
	case "not_running":
		if s.CoreRunning {
			return errors.New("stopped traffic sources cannot declare running")
		}
	case "sampling_failed":
	default:
		return errors.New("invalid traffic source status")
	}
	return nil
}
