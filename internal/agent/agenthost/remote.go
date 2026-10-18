package agenthost

import (
	"context"
	"path/filepath"
	"proxycontrol/internal/agent/agentjobs"
	"proxycontrol/internal/platform/config"
	"proxycontrol/pkg/contracts/release"
)

func nodeJobs(c config.Agent, nodeID string, startup ...context.Context) (*agentjobs.Store, error) {
	key, err := readOriginal(filepath.Join(c.DataDirectory, "configuration.key"), 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	r, err := readRecord(filepath.Join(c.DataDirectory, "identity", markerName), key, nodeID)
	if err != nil || !r.Jobs {
		return nil, ErrState
	}
	if len(startup) > 0 {
		return agentjobs.OpenForStartup(startup[0], jobsOptions(c.DataDirectory, nodeID, key))
	}
	return agentjobs.Open(jobsOptions(c.DataDirectory, nodeID, key))
}

func RemoteUpdatesAvailable(ctx context.Context, c config.Agent, nodeID string) error {
	s, err := nodeJobs(c, nodeID, ctx)
	if err != nil {
		return err
	}
	return s.Close()
}

// OriginalRemoteResult replays only the immutable receipt of the installation
// explicitly bound by a recovery command, never an unrelated historic job.
func OriginalRemoteResult(c config.Agent, nodeID string, request release.AgentUpdateRequest) (*release.UpdateResult, error) {
	if request.Validate() != nil || request.NodeID != nodeID || request.Operation != "recover" {
		return nil, release.ErrUpdate
	}
	s, err := nodeJobs(c, nodeID)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	original, found := s.Get(request.OriginalID)
	if !found || original.Request.Operation != "install" || original.Request.NodeID != nodeID || original.Request.Revision != request.Revision || original.Request.Release != request.Release {
		return nil, ErrState
	}
	if original.Result == nil {
		return nil, nil
	}
	if release.ValidateAgentResult(original.Request, *original.Result) != nil {
		return nil, ErrState
	}
	result := *original.Result
	return &result, nil
}

// QueueRemoteUpdate accepts only fixed authenticated-control requests. Local
// signing trust is independently enforced by the parent, including rejection
// results when panel and node pins differ. No executable path is accepted.
func QueueRemoteUpdate(c config.Agent, nodeID string, r release.AgentUpdateRequest, recovery ...bool) (agentjobs.Job, error) {
	if r.Validate() != nil || r.NodeID != nodeID || r.Operation != "install" && !(r.Operation == "recover" && len(recovery) > 0 && recovery[0]) {
		return agentjobs.Job{}, release.ErrUpdate
	}
	s, err := nodeJobs(c, nodeID)
	if err != nil {
		return agentjobs.Job{}, err
	}
	defer s.Close()
	if err := s.Queue(r); err != nil {
		return agentjobs.Job{}, err
	}
	job, ok := s.Get(r.ID)
	if !ok {
		return agentjobs.Job{}, ErrState
	}
	return job, nil
}
