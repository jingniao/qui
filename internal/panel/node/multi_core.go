package node

import (
	"errors"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "proxycontrol/pkg/contracts/agentpb"
)

func CoreNameValid(name string) bool { return name == "xray" || name == "sing-box" }

// EnabledCores models legacy reports without inventing observations.
func EnabledCores(c *pb.Capabilities) []*pb.CoreCapability {
	if c == nil {
		return nil
	}
	if c.MultiCoreSupported {
		return c.Cores
	}
	if c.CoreName == "" {
		return nil
	}
	return []*pb.CoreCapability{{Name: c.CoreName, Version: c.CoreVersion, Protocols: c.Protocols, SingboxUserStats: c.SingboxUserStats, ForwardInboundSupported: c.ForwardInboundSupported, DeviceGateSupported: c.DeviceGateSupported, ObservationContractVersion: c.ObservationContractVersion}}
}
func SelectedCore(c *pb.Capabilities, name string) *pb.CoreCapability {
	if c == nil {
		return nil
	}
	if name == "" {
		name = c.CoreName
	}
	for _, core := range EnabledCores(c) {
		if core != nil && core.Name == name {
			return core
		}
	}
	return nil
}

func AnyGateSupported(c *pb.Capabilities) bool {
	for _, core := range EnabledCores(c) {
		if core.DeviceGateSupported {
			return true
		}
	}
	return false
}

// A report may carry one named core without the multi-core flag: that is the
// shape an Agent produces when it only decorates its scalar capabilities, so it
// stays a one-core node and must not be rejected. Two cores or a contradictory
// primary are refused.
func validateMultiCapabilities(c *pb.Capabilities) error {
	if len(c.Cores) > 2 || len(c.Cores) > 1 && !c.MultiCoreSupported {
		return errors.New("invalid multi-core capabilities")
	}
	seen := map[string]bool{}
	for _, core := range c.Cores {
		if core == nil || !CoreNameValid(core.Name) || seen[core.Name] || len(core.Version) > 64 || len(core.Protocols) > 16 {
			return errors.New("invalid core capability")
		}
		seen[core.Name] = true
		legacy := &pb.Capabilities{CoreName: core.Name, CoreVersion: core.Version, Protocols: core.Protocols}
		if err := validateMessage("node", &pb.AgentMessage{NodeId: "node", Sequence: 1, MessageId: "validate", Timestamp: timestamppb.Now(), Payload: &pb.AgentMessage_Capabilities{Capabilities: legacy}}); err != nil {
			return err
		}
	}
	if c.MultiCoreSupported && (!seen[c.CoreName] || len(c.Cores) == 0) {
		return errors.New("primary core missing")
	}
	if !c.MultiCoreSupported && len(c.Cores) == 1 && c.CoreName != "" && c.Cores[0].Name != c.CoreName {
		return errors.New("core list contradicts the primary")
	}
	return nil
}

func validateCoreStatuses(cores []*pb.CoreStatus) error {
	if len(cores) > 2 {
		return errors.New("too many core statuses")
	}
	seen := map[string]bool{}
	for _, c := range cores {
		if c == nil || !CoreNameValid(c.Name) || seen[c.Name] {
			return errors.New("invalid core status")
		}
		seen[c.Name] = true
		if c.Error != "" && c.Error != "core_unhealthy" && c.Error != "accounting_interrupted" {
			return errors.New("invalid core error")
		}
		if c.Error != "" && c.Healthy {
			return errors.New("inconsistent core health")
		}
		if c.StartedAt != nil && c.StartedAt.CheckValid() != nil {
			return errors.New("invalid core start")
		}
		if err := validateTrafficSourceStatus(c.TrafficSourceStatus); err != nil {
			return err
		}
	}
	return nil
}
func validateRemoveResult(r *pb.RemoveCoreResult) error {
	if r.Id == "" || len(r.Id) > 128 || !CoreNameValid(r.Component) || r.Revision < 1 {
		return errors.New("invalid remove result")
	}
	if len(r.Code) > 128 || r.Code == "" || r.Success != (r.Code == "success") {
		return errors.New("invalid remove result code")
	}
	return nil
}

// DeviceGateUDPUnsupported reports whether a gated user's UDP on an inbound of
// this protocol, served by this core, escapes the device limit. Measured with
// the real cores (docs/v1.1-progress.md, L8): Xray SOCKS5 UDP carries no user
// identity; sing-box SOCKS5 UDP cannot be tied to the user, and sing-box
// Shadowsocks, Hysteria2 and TUIC recreate a closed UDP session for the same
// user on the next datagram. TCP on these inbounds is still limited.
func DeviceGateUDPUnsupported(core, protocol string) bool {
	switch core {
	case "xray":
		return protocol == "socks5"
	case "sing-box":
		return protocol == "socks5" || protocol == "shadowsocks" || protocol == "hysteria2" || protocol == "tuic"
	}
	return false
}

// DeviceGateCoverage is how far the device limit reaches on an inbound: "full",
// "tcp_only" (UDP is not limited, see DeviceGateUDPUnsupported) or "none"
// (the core cannot gate, or the inbound has no user identity).
func DeviceGateCoverage(core *pb.CoreCapability, protocol string) string {
	switch {
	case core == nil || !core.DeviceGateSupported || protocol == "wireguard" || protocol == "forward":
		return "none"
	case DeviceGateUDPUnsupported(core.Name, protocol):
		return "tcp_only"
	}
	return "full"
}
