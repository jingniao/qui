package singbox

import (
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func (a *Adapter) TrafficSourceStatus() *pb.TrafficSourceStatus {
	// Official sing-box builds cannot attribute traffic to a user (plan D5), and
	// the node-total reader is step 6.3. Until it exists the source is reported
	// as unavailable, never as a fabricated zero.
	return &pb.TrafficSourceStatus{Status: "unavailable", ObservedAt: timestamppb.Now()}
}
