package singbox

import (
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func (a *Adapter) TrafficSourceStatus() *pb.TrafficSourceStatus {
	if !a.mu.TryLock() {
		return &pb.TrafficSourceStatus{Status: "busy", ObservedAt: timestamppb.Now()}
	}
	defer a.mu.Unlock()
	if a.current.UserStatsEndpoint != "" && a.statsEpoch != "" {
		return a.counterSources.Status(a.statsEpoch, a.current.Revision, len(a.current.StatsAccounts), a.process != nil)
	}
	// Official sing-box builds cannot attribute traffic to a user (plan D5), so
	// the per-user source is reported as unavailable, never as a fabricated zero.
	// Node totals are read by node_traffic.go and reported separately.
	return &pb.TrafficSourceStatus{Status: "unavailable", ObservedAt: timestamppb.Now()}
}
