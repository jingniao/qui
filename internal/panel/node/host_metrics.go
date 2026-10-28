package node

import (
	"errors"
	"math"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
)

func validateHostMetrics(h *pb.Heartbeat) error {
	valid := func(v *float64) bool {
		return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 1e15)
	}
	if !valid(h.HostUptimeSeconds) || len(h.Interfaces) > 1024 {
		return errors.New("invalid host metrics")
	}
	if h.CoreStartedAt != nil && h.CoreStartedAt.CheckValid() != nil {
		return errors.New("invalid core start time")
	}
	for _, v := range []*string{h.TcpCongestionControl, h.DefaultQdisc} {
		if v != nil && (len(*v) == 0 || len(*v) > 64 || len(strings.Fields(*v)) != 1) {
			return errors.New("invalid sysctl observation")
		}
	}
	seen := map[string]bool{}
	for _, nic := range h.Interfaces {
		if nic == nil || nic.Name == "" || len(nic.Name) > 64 || seen[nic.Name] || !valid(nic.ReceiveBytesPerSecond) || !valid(nic.TransmitBytesPerSecond) {
			return errors.New("invalid interface observation")
		}
		seen[nic.Name] = true
	}
	return nil
}
