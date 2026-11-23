package core

import (
	"errors"
	"sort"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// CounterSources retains only public native source names for one live process.
// Adapters call Observe under their existing process mutex. It is neither a
// durable usage cursor nor evidence that a never-observed account used zero.
type CounterSources struct {
	epoch       string
	seen        map[string]bool
	lastSuccess time.Time
	failed      bool
}

// CounterSourceSnapshot contains source presence, never usage cursors. Epoch
// binding prevents restoring a different process's source set.
type CounterSourceSnapshot struct {
	Epoch       string
	Seen        []string
	LastSuccess time.Time
}

func (s *CounterSources) Snapshot(epoch string) (CounterSourceSnapshot, error) {
	if epoch == "" || s.epoch != epoch || s.failed || s.lastSuccess.IsZero() {
		return CounterSourceSnapshot{}, errors.New("counter source snapshot requires a successful sample of this epoch")
	}
	result := CounterSourceSnapshot{Epoch: epoch, LastSuccess: s.lastSuccess}
	for name := range s.seen {
		result.Seen = append(result.Seen, name)
	}
	sort.Strings(result.Seen)
	return result, nil
}

func (s *CounterSources) Restore(epoch string, snapshot CounterSourceSnapshot) error {
	if epoch == "" || snapshot.Epoch != epoch || snapshot.LastSuccess.IsZero() {
		return errors.New("counter source snapshot epoch or observation is invalid")
	}
	seen := make(map[string]bool, len(snapshot.Seen))
	for _, name := range snapshot.Seen {
		if name == "" || seen[name] {
			return errors.New("invalid or duplicate counter source in snapshot")
		}
		seen[name] = true
	}
	*s = CounterSources{epoch: epoch, seen: seen, lastSuccess: snapshot.LastSuccess}
	return nil
}

func (s *CounterSources) Observe(epoch string, directions map[string]uint8) error {
	if epoch == "" {
		return errors.New("native counter sources require a process epoch")
	}
	for _, pair := range directions {
		if pair != 3 {
			return errors.New("native counter source lacks a complete direction pair")
		}
	}
	if s.epoch == epoch {
		for name := range s.seen {
			if directions[name] != 3 {
				return errors.New("previously observed native counter source disappeared")
			}
		}
	} else {
		s.epoch = epoch
		s.seen = map[string]bool{}
		s.lastSuccess = time.Time{}
	}
	for name := range directions {
		s.seen[name] = true
	}
	s.lastSuccess = time.Now().UTC()
	s.failed = false
	return nil
}

func (s *CounterSources) Failure(epoch string) {
	if epoch == "" {
		return
	}
	if s.epoch != epoch {
		s.epoch = epoch
		s.seen = map[string]bool{}
		s.lastSuccess = time.Time{}
	}
	s.failed = true
}

// Counts describe sources ever observed in this process, not accounts, bytes,
// or confirmation that all traffic was sampled. Never-observed meters may be lazy.
func (s *CounterSources) Status(epoch string, revision int64, configured int, running bool) *pb.TrafficSourceStatus {
	result := &pb.TrafficSourceStatus{Status: "unobserved", CoreEpoch: epoch, Revision: revision, ConfiguredSources: uint64(configured), CoreRunning: running, ObservedAt: timestamppb.Now()}
	if s.epoch == epoch {
		result.ObservedSources = uint64(len(s.seen))
		if !s.lastSuccess.IsZero() {
			result.LastSuccessAt = timestamppb.New(s.lastSuccess)
			result.Status = "partial"
			if result.ObservedSources == result.ConfiguredSources {
				result.Status = "covered"
			}
		}
		if s.failed {
			result.Status = "sampling_failed"
		}
	}
	if !running && result.Status != "sampling_failed" {
		result.Status = "not_running"
	}
	return result
}
