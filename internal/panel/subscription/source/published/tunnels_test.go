package published

import (
	"context"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
	"testing"
)

type forwardSubscriptionFixture struct{ state desiredstate.State }

func (f forwardSubscriptionFixture) Credentials(context.Context, string, bool) ([]user.Credential, error) {
	return []user.Credential{{ID: "credential", Protocol: "forward", Status: "active"}}, nil
}
func (f forwardSubscriptionFixture) ExpandedNodes(context.Context, []string) ([]node.Node, error) {
	return []node.Node{{ID: "entry", PublicIP: "203.0.113.1", Capabilities: &pb.Capabilities{CoreName: "gost", ForwardInboundSupported: true, Protocols: []*pb.ProtocolCapability{{Protocol: "forward", Inbound: true}}}}}, nil
}
func (f forwardSubscriptionFixture) UserInboundGroups(context.Context, string, string) ([]string, error) {
	return []string{"group"}, nil
}
func (f forwardSubscriptionFixture) Published(context.Context, string) (desiredstate.State, error) {
	return f.state, nil
}

func TestGeneratedTunnelForwardsAndRelaysNeverEnterSubscriptions(t *testing.T) {
	// Even an accidental visible flag and user binding cannot turn forwards or
	// relay listeners into subscription endpoints. N1 emits linked entries from
	// the destination inbound instead of these generated listeners.
	fixture := forwardSubscriptionFixture{state: desiredstate.State{NodeID: "entry", Inbounds: []desiredstate.Inbound{
		{ID: "rule", Protocol: "forward", Listen: "0.0.0.0", Port: 30001, Enabled: true, SubscriptionVisible: true, CredentialIDs: []string{"credential"}, Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Host: "example.com", Port: 443, Network: "tcp"}}},
		{ID: "relay", Protocol: "forward", Listen: "0.0.0.0", Port: 30002, Enabled: true, SubscriptionVisible: true, Settings: desiredstate.ProtocolSettings{Forward: &desiredstate.ForwardSettings{Mode: "relay"}}},
	}}}
	s := &Source{Users: fixture, Nodes: fixture, Published: fixture}
	endpoints, _, err := s.Proxies(context.Background(), source.Viewer{UserID: "user", DeviceID: "identity", NodeGroupIDs: []string{"group"}, AllowedProtocols: []string{"forward"}})
	if err != nil || len(endpoints) != 0 {
		t.Fatal(endpoints, err)
	}
}
