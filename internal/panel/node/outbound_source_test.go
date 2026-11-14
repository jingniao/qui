package node

import (
	pb "proxycontrol/pkg/contracts/agentpb"
	"testing"
)

func TestOutboundSourceCapability(t *testing.T) {
	for _, supported := range []bool{false, true} {
		cores := EnabledCores(&pb.Capabilities{CoreName: "xray", OutboundSourceSupported: supported})
		if len(cores) != 1 || cores[0].OutboundSourceSupported != supported || SettingsContractCapability(cores[0]).OutboundSourceSupported != supported {
			t.Fatalf("outbound source report lost: %v", cores)
		}
	}
}
