package singbox

import (
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func (a *Adapter) TrafficSourceStatus() *pb.TrafficSourceStatus {
	// Official sing-box builds cannot attribute traffic to a user (plan D5), so
	// the per-user source is reported as unavailable, never as a fabricated zero.
	// Node totals are read by node_traffic.go and reported separately.
	return &pb.TrafficSourceStatus{Status: "unavailable", ObservedAt: timestamppb.Now()}
}
