package capability

import (
	"math"
	"os"
	"path/filepath"
	pb "proxycontrol/pkg/contracts/agentpb"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// readHost uses only readable proc files. Missing files are unknown, never zero.
func readHost(root string, h *pb.Heartbeat) {
	if raw, err := os.ReadFile(filepath.Join(root, "uptime")); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 0 {
			v, e := strconv.ParseFloat(fields[0], 64)
			if e == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 {
				h.HostUptimeSeconds = &v
			}
		}
	}
	for _, item := range []struct {
		path   string
		target **string
	}{{"sys/net/ipv4/tcp_congestion_control", &h.TcpCongestionControl}, {"sys/net/core/default_qdisc", &h.DefaultQdisc}} {
		if raw, err := os.ReadFile(filepath.Join(root, item.path)); err == nil {
			v := strings.TrimSpace(string(raw))
			if v != "" && len(v) <= 64 && len(strings.Fields(v)) == 1 {
				*item.target = &v
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "net/dev"))
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		name, values, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "lo" || name == "" {
			continue
		}
		fields := strings.Fields(values)
		if len(fields) != 16 {
			continue
		}
		rx, e1 := strconv.ParseUint(fields[0], 10, 64)
		tx, e2 := strconv.ParseUint(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		h.Interfaces = append(h.Interfaces, &pb.InterfaceCounters{Name: name, ReceiveBytes: rx, TransmitBytes: tx})
	}
}

func (s *Sampler) hostHeartbeat(h *pb.Heartbeat, now time.Time) {
	if runtime.GOOS != "linux" {
		return
	}
	readHost("/proc", h)
	readExtraHost("/proc", h)
	_, h.DiskUsedBytes = diskBytes(s.directory)
	s.interfaceRates(h, now)
}

func (s *Sampler) interfaceRates(h *pb.Heartbeat, now time.Time) {
	elapsed := now.Sub(s.sampled).Seconds()
	for _, nic := range h.Interfaces {
		if old, ok := s.interfaces[nic.Name]; ok && elapsed >= 1 && nic.ReceiveBytes >= old[0] && nic.TransmitBytes >= old[1] {
			rx, tx := float64(nic.ReceiveBytes-old[0])/elapsed, float64(nic.TransmitBytes-old[1])/elapsed
			nic.ReceiveBytesPerSecond = &rx
			nic.TransmitBytesPerSecond = &tx
		}
	}
	s.interfaces = map[string][2]uint64{}
	for _, nic := range h.Interfaces {
		s.interfaces[nic.Name] = [2]uint64{nic.ReceiveBytes, nic.TransmitBytes}
	}
}
