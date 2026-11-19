package updater

import "errors"

// InspectAgentInstallation is for a stopped supervisor. It takes the ordinary
// history locks but never repairs a checkpoint or promotes an interrupted tail.
func InspectAgentInstallation(options HistoryOptions) (state Installation, resultErr error) {
	directory, checkpoint, err := historyPaths(options)
	if err != nil {
		return state, err
	}
	h, err := historyHandles(options, directory, checkpoint)
	if err != nil {
		return state, err
	}
	defer func() { resultErr = errors.Join(resultErr, h.Close()) }()
	h.inspectionOnly = true
	if err := h.load(); err != nil {
		return state, err
	}
	state, found, err := h.Installation("proxy-agent")
	if err != nil || !found {
		return state, errors.Join(ErrHistory, err)
	}
	return state, nil
}
