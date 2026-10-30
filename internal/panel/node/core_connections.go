package node

import (
	"errors"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

func (c *Control) recordCoreConnections(nodeID string, status *pb.ConnectionStatus) error {
	cores := map[string]traffic.ConnectionSnapshot{}
	for _, core := range status.Cores {
		if core == nil || !CoreNameValid(core.Name) {
			return errors.New("invalid core snapshot")
		}
		if _, ok := cores[core.Name]; ok {
			return errors.New("duplicate core snapshot")
		}
		// An empty per-core payload carries no measurement; a partially
		// measurable node must not lose the cores that did report.
		if len(core.SnapshotJson) == 0 {
			continue
		}
		snapshot, err := traffic.DecodeConnections(core.SnapshotJson)
		if err != nil {
			return err
		}
		cores[core.Name] = snapshot
	}
	if len(cores) == 0 {
		return errors.New("no core snapshot")
	}
	return c.Connections.RecordCores(nodeID, cores)
}
