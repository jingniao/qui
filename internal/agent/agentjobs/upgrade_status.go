package agentjobs

import "errors"

type UpgradeStatus struct {
	Pending  *Job
	Recovery *Job
}

// InspectUpgradeStatus describes current reservations, not historic failures.
// It authenticates the original journal without promoting interrupted tails.
func InspectUpgradeStatus(options Options) (status UpgradeStatus, resultErr error) {
	directory, checkpoint, err := paths(options)
	if err != nil {
		return status, err
	}
	s, err := handles(options, directory, checkpoint)
	if err != nil {
		return status, err
	}
	defer func() { resultErr = errors.Join(resultErr, s.Close()) }()
	s.inspectionOnly = true
	if err := s.load(); err != nil {
		return status, err
	}
	if err := s.diskHead(); err != nil {
		return status, err
	}
	status.Pending = s.Pending()
	for id := range s.unresolvedRecoveries() {
		if status.Recovery != nil {
			return status, ErrState
		}
		job, found := s.Get(id)
		if !found {
			return status, ErrState
		}
		status.Recovery = &job
	}
	return status, nil
}
