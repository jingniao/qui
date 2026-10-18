package xray

import (
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func (a *Adapter) TrafficSourceStatus() *pb.TrafficSourceStatus {
	if !a.mu.TryLock() {
		return &pb.TrafficSourceStatus{Status: "busy", ObservedAt: timestamppb.Now()}
	}
	defer a.mu.Unlock()
	if a.current.StatsEndpoint == "" || a.statsEpoch == "" {
		return &pb.TrafficSourceStatus{Status: "unavailable", ObservedAt: timestamppb.Now()}
	}
	return a.counterSources.Status(a.statsEpoch, a.current.Revision, len(a.current.StatsAccounts), a.process != nil)
}
