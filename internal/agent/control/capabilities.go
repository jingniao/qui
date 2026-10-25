package control

import (
	"sync"

	"google.golang.org/protobuf/proto"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// capabilityState holds the node's latest detected core capabilities. Every
// session reports it on connect, and the core update worker replaces it once the
// running binary changed, so a reconnect after a successful core update never
// republishes the capabilities detected at process start.
type capabilityState struct {
	mu    sync.Mutex
	value *pb.Capabilities
}

func newCapabilityState(detected *pb.Capabilities) *capabilityState {
	state := &capabilityState{}
	state.Store(detected)
	return state
}

// Store keeps its own copy: neither the detector nor a sender may observe the
// other's message mutating.
func (c *capabilityState) Store(capabilities *pb.Capabilities) {
	if c == nil {
		return
	}
	if capabilities == nil {
		capabilities = &pb.Capabilities{}
	}
	cloned := proto.Clone(capabilities).(*pb.Capabilities)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = cloned
}

// Snapshot returns a copy, or empty capabilities before anything was detected.
func (c *capabilityState) Snapshot() *pb.Capabilities {
	if c == nil {
		return &pb.Capabilities{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.value == nil {
		return &pb.Capabilities{}
	}
	return proto.Clone(c.value).(*pb.Capabilities)
}
