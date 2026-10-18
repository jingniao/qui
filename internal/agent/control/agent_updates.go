package control

import (
	"context"
	"encoding/json"
	"proxycontrol/internal/agent/agenthost"
	"proxycontrol/internal/platform/config"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/release"
)

type agentUpdates struct {
	cfg      config.Agent
	nodeID   string
	recovery bool
}

func newAgentUpdates(ctx context.Context, c config.Agent, nodeID string, recovery bool) (*agentUpdates, error) {
	if err := agenthost.RemoteUpdatesAvailable(ctx, c, nodeID); err != nil {
		return nil, err
	}
	return &agentUpdates{cfg: c, nodeID: nodeID, recovery: recovery}, nil
}
func (w *agentUpdates) submit(command *pb.AgentUpdateCommand, recovery bool) ([]*pb.AgentUpdateResult, error) {
	if command == nil {
		return nil, release.ErrUpdate
	}
	r, err := release.DecodeAgentUpdateRequest(command.RequestJson)
	if err != nil {
		return nil, err
	}
	job, err := agenthost.QueueRemoteUpdate(w.cfg, w.nodeID, r, recovery && w.recovery)
	if err != nil {
		return nil, err
	}
	results := []*pb.AgentUpdateResult{}
	if r.Operation == "recover" {
		original, err := agenthost.OriginalRemoteResult(w.cfg, w.nodeID, r)
		if err != nil {
			return nil, err
		}
		if original != nil {
			data, err := json.Marshal(original)
			if err != nil {
				return nil, err
			}
			results = append(results, &pb.AgentUpdateResult{ResultJson: data})
		}
	}
	if job.Result != nil {
		if err := release.ValidateAgentResult(r, *job.Result); err != nil {
			return nil, err
		}
		data, err := json.Marshal(job.Result)
		if err != nil {
			return nil, err
		}
		results = append(results, &pb.AgentUpdateResult{ResultJson: data})
	}
	return results, nil
}
