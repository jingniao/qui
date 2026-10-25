package node

import (
	"context"

	pb "proxycontrol/pkg/contracts/agentpb"
)

// coreUpdateSession is the once-per-connection part of the core update
// bookkeeping.
type coreUpdateSession struct {
	staleClosed bool
}

// coreUpdateCapabilitiesStored closes the node's unconfirmed core update once
// this connection has stored its first Capabilities message. Closing any earlier
// would compare the requested version with the capabilities of the previous
// connection, so a node that had just installed exactly that version would be
// recorded as failed instead of succeeded.
func (c *Control) coreUpdateCapabilitiesStored(ctx context.Context, nodeID string, message *pb.AgentMessage, session *coreUpdateSession) error {
	if session == nil || session.staleClosed || c.CoreUpdates == nil || message.GetCapabilities() == nil {
		return nil
	}
	session.staleClosed = true
	return c.CoreUpdates.CloseStaleCoreUpdates(ctx, nodeID)
}
