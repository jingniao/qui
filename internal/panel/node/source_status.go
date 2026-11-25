package node

import (
	"errors"
	"net"

	pb "proxycontrol/pkg/contracts/agentpb"
)

// validateSourceAddressStatus checks the Agent's report of configured outbound
// sources missing on its host. Old Agents omit it; the panel only displays it.
func validateSourceAddressStatus(status *pb.SourceAddressStatus) error {
	if status == nil {
		return nil
	}
	if status.Revision < 0 {
		return errors.New("invalid source status revision")
	}
	if status.Error != "" {
		if status.Error != "source_check_failed" || len(status.Missing) > 0 {
			return errors.New("invalid source status error")
		}
		return nil
	}
	// One entry per configured binding; generous so a large node never has its
	// whole heartbeat refused.
	if len(status.Missing) > 1024 {
		return errors.New("too many missing sources")
	}
	for _, issue := range status.Missing {
		if issue.Address != "" && net.ParseIP(issue.Address) == nil || len(issue.Interface) > 64 || len(issue.OwnerId) > 128 {
			return errors.New("invalid missing source")
		}
		owned := issue.OwnerId != ""
		switch issue.Kind {
		case "host_source":
			if owned || issue.Address == "" || issue.Interface != "" {
				return errors.New("invalid missing host source")
			}
		case "inbound_egress":
			if !owned || issue.Address == "" || issue.Interface != "" {
				return errors.New("invalid missing inbound egress")
			}
		case "core_source":
			if owned || issue.Address == "" && issue.Interface == "" {
				return errors.New("invalid missing core source")
			}
		case "outbound_source":
			if !owned || issue.Address == "" && issue.Interface == "" {
				return errors.New("invalid missing outbound source")
			}
		default:
			return errors.New("unknown missing source kind")
		}
	}
	return nil
}
