package node

import (
	"context"
	"errors"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// A stored report or accepted-envelope replay cannot authorize a new session.
// The Agent creates its current capability envelope after receiving Welcome.
type settingsContractSession struct{ capabilitiesSeen bool }

func (s *settingsContractSession) record(message *pb.AgentMessage, fresh bool) {
	if fresh && message.GetCapabilities() != nil {
		s.capabilitiesSeen = true
	}
}

func (c *Control) canDispatchApply(ctx context.Context, nodeID string, command *pb.ApplyCommand, session *settingsContractSession) (bool, error) {
	state, err := desiredstate.Decode(command.DesiredStateJson)
	if err != nil {
		return false, err
	}
	return c.canDispatchSettings(ctx, nodeID, state, session)
}

func (c *Control) canDispatchRollback(ctx context.Context, nodeID string, command *pb.RollbackCommand, session *settingsContractSession) (bool, error) {
	// Revision zero clears the runtime and has no state/settings to transmit.
	// It remains available to legacy peers without a capability report.
	if command.Revision == 0 {
		return true, nil
	}
	state, err := c.Deployment.State(ctx, command.Revision)
	if err != nil {
		return false, err
	}
	return c.canDispatchSettings(ctx, nodeID, state, session)
}

func (c *Control) canDispatchSettings(ctx context.Context, nodeID string, state desiredstate.State, session *settingsContractSession) (bool, error) {
	if state.NodeID != nodeID {
		return false, errors.New("command belongs to another node")
	}
	for _, inbound := range state.Inbounds {
		if !inbound.Settings.HasServerSettings() {
			continue
		}
		if session == nil || !session.capabilitiesSeen {
			return false, nil
		}
		current, err := c.Repository.Get(ctx, nodeID)
		if err != nil {
			return false, err
		}
		core := SelectedCore(current.Capabilities, inbound.Core)
		if core == nil || desiredstate.RequireInboundServerSettings(core.Name, SettingsContractCapability(core), inbound.Protocol, inbound.Settings) != nil {
			// Withhold the queued command without breaking an otherwise healthy
			// stream; a later fresh capability report can make it eligible again.
			return false, nil
		}
	}
	return true, nil
}
