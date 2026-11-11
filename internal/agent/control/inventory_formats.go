package control

import (
	"proxycontrol/internal/agent/storage"
	"proxycontrol/pkg/contracts/agentstorage"
	"proxycontrol/pkg/contracts/desiredstate"
)

// activateInventoryFormats raises the multi-core storage formats for a desired
// state that names an explicit core. The floor is re-read first: once it already
// writes the multi-core formats nothing is checked or written, so publishing such
// a revision never needs the release history. Only an unraised floor runs
// check, which proves it is safe to raise.
func activateInventoryFormats(directory, nodeID string, contract agentstorage.Contract, desired desiredstate.State, check func() error) error {
	explicit := false
	for _, in := range desired.Inbounds {
		explicit = explicit || in.Core != ""
	}
	if !explicit {
		return nil
	}
	floor, _, err := storage.Check(directory, nodeID, contract)
	if err != nil || floor.Formats.Configuration.Write >= 4 {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	_, err = storage.ActivateCoreInventoryFormats(directory, nodeID, contract)
	return err
}
