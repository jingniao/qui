package agentjobs

import (
	"errors"
	"os"
	"proxycontrol/pkg/contracts/release"
	"sort"
)

func InspectRecoveryJobs(options Options) (result release.RecoveryUpdateHistory, resultErr error) {
	return inspectRecoveryJobs(options)
}

func inspectRecoveryJobs(options Options) (result release.RecoveryUpdateHistory, resultErr error) {
	result = release.RecoveryUpdateHistory{Kind: "agent_jobs", Installations: []release.RecoveryInstallation{}}
	dir, checkpoint, err := paths(options)
	if err != nil {
		return result, err
	}
	_, dirErr := os.Lstat(dir)
	_, headErr := os.Lstat(checkpoint)
	if errors.Is(dirErr, os.ErrNotExist) && errors.Is(headErr, os.ErrNotExist) {
		return result, nil
	}
	if dirErr != nil || headErr != nil {
		return result, ErrState
	}
	s, err := handles(options, dir, checkpoint)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, s.Close()) }()
	s.inspectionOnly = true
	if err := s.load(); err != nil {
		return result, err
	}
	if err := s.diskHead(); err != nil {
		return result, err
	}
	result.Present = true
	result.EventCount = int64(s.head.Number)
	result.HeadDigest = s.head.Digest
	result.NeedsRecovery = s.NeedsRecovery()
	if len(s.jobs) > 4096 {
		return result, ErrState
	}
	for _, job := range s.jobs {
		requestDigest, err := release.RecoveryRequestDigest(release.UpdateRequest(job.Request))
		if err != nil {
			return result, err
		}
		phase := "queued"
		if job.Started {
			phase = "started"
		}
		var finished *release.UpdateResult
		if job.Result != nil {
			copy := *job.Result
			finished = &copy
			phase = copy.Status
		}
		result.Attempts = append(result.Attempts, release.RecoveryAttempt{RequestDigest: requestDigest, Kind: "job", ID: job.Request.ID, OriginalID: job.Request.OriginalID, Component: "proxy-agent", Revision: job.Request.Revision, Phase: phase, Operation: job.Request.Operation, TargetDigest: job.Request.Release.Manifest.SHA256, Started: job.Started, Result: finished})
		if job.Result == nil {
			result.PendingJobs++
			if job.Started {
				result.StartedJobs++
				result.NeedsRecovery = true
			}
		} else if job.Result.Status == "failed_closed" {
			result.NeedsRecovery = true
		}
	}
	sort.Slice(result.Attempts, func(i, j int) bool { return result.Attempts[i].ID < result.Attempts[j].ID })
	return result, nil
}
