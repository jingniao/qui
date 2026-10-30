package control

import (
	"context"
	"encoding/json"
	"sort"

	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/traffic"
)

func coreConnectionStatuses(ctx context.Context, source any, revision int64, addresses bool) []*pb.CoreConnectionStatus {
	r, ok := source.(interface {
		CoreConnections(context.Context) map[string]traffic.ConnectionSnapshot
	})
	if !ok {
		return nil
	}
	parts := r.CoreConnections(ctx)
	names := []string{}
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	result := []*pb.CoreConnectionStatus{}
	for _, name := range names {
		s := parts[name]
		if s.Revision != revision {
			continue
		}
		if !addresses {
			s.ObservationContractVersion = 0
			s.DeviceGateSupported = false
			for i := range s.Accounts {
				s.Accounts[i].AddressKeys = nil
				s.Accounts[i].AddressesMeasured = false
			}
		}
		if data, err := json.Marshal(s); err == nil {
			result = append(result, &pb.CoreConnectionStatus{Name: name, SnapshotJson: data})
		}
	}
	return result
}
