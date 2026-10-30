package node

import (
	"errors"
	"math"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func validateK2bMetrics(h *pb.Heartbeat) error {
	for _, v := range []*float64{h.Load1, h.Load5, h.Load15} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return errors.New("invalid host load")
		}
	}
	for _, v := range []*uint64{h.ProcessCount, h.TcpConnections, h.UdpSockets, h.MemoryUsedBytes, h.SwapUsedBytes, h.DiskUsedBytes} {
		if v != nil && *v > math.MaxInt64 {
			return errors.New("host count exceeds storage range")
		}
	}
	if h.GetActiveConnectionsMeasured() && h.ActiveConnections > math.MaxInt64 {
		return errors.New("invalid active connections")
	}
	if len(h.Interfaces) > 1024 {
		return errors.New("too many host interfaces")
	}
	for _, nic := range h.Interfaces {
		if nic == nil {
			return errors.New("invalid interface")
		}
		for _, v := range []*float64{nic.ReceiveBytesPerSecond, nic.TransmitBytesPerSecond} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > 1e18) {
				return errors.New("invalid interface rate")
			}
		}
	}
	if len(h.TuningCurrent) > len(tuningRules) {
		return errors.New("too many tuning observations")
	}
	for key, value := range h.TuningCurrent {
		if _, ok := tuningRules[key]; !ok || len(value) > 256 {
			return errors.New("invalid tuning observation")
		}
	}
	return nil
}
