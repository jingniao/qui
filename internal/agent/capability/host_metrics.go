package capability

import (
	"encoding/hex"
	"math"
	"net"
	"os"
	"path/filepath"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/netaddr"
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
	// Boot identity and the default route describe the host, not the interface
	// list, so they are read before the /proc/net/dev early return.
	if raw, err := os.ReadFile(filepath.Join(root, "sys/kernel/random/boot_id")); err == nil {
		if v := strings.TrimSpace(string(raw)); canonicalBootID(v) {
			h.BootId = &v
		}
	}
	if raw, err := os.ReadFile(filepath.Join(root, "net/route")); err == nil {
		if v, ok := defaultRouteInterface(string(raw)); ok {
			h.DefaultRouteInterface = &v
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

// canonicalBootID accepts the kernel's canonical UUID text, 8-4-4-4-12 in
// hexadecimal groups. Anything else is a failed read: the panel must not invent
// a boot identity from uptime, a process id, the hostname or the Agent start.
func canonicalBootID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}

// defaultRouteInterface applies an IPv4 routing-table heuristic to
// /proc/net/route: destination and mask zero, RTF_UP set and RTF_REJECT clear.
// The smallest metric wins, then the smallest name. A gateway may be zero, and
// an IPv6-only host reports nothing. This is not `ip route get`.
func defaultRouteInterface(raw string) (string, bool) {
	const (
		rtfUp     = 0x0001
		rtfReject = 0x0200
	)
	best, bestMetric := "", uint64(0)
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[0] == "Iface" {
			continue
		}
		destination, destinationErr := strconv.ParseUint(fields[1], 16, 32)
		flags, flagsErr := strconv.ParseUint(fields[3], 16, 32)
		metric, metricErr := strconv.ParseUint(fields[6], 10, 32)
		mask, maskErr := strconv.ParseUint(fields[7], 16, 32)
		name := fields[0]
		if destinationErr != nil || flagsErr != nil || metricErr != nil || maskErr != nil || name == "" || name == "lo" {
			continue
		}
		if destination != 0 || mask != 0 || flags&rtfUp == 0 || flags&rtfReject != 0 {
			continue
		}
		if best == "" || metric < bestMetric || metric == bestMetric && name < best {
			best, bestMetric = name, metric
		}
	}
	return best, best != ""
}

func (s *Sampler) hostHeartbeat(h *pb.Heartbeat, now time.Time) {
	if runtime.GOOS != "linux" {
		return
	}
	readHost("/proc", h)
	readExtraHost("/proc", h)
	h.PublicIp = publicIP(h.GetDefaultRouteInterface())
	_, h.DiskUsedBytes = diskBytes(s.directory)
	s.interfaceRates(h, now)
}

// publicIP reports the address a client on the Internet could dial, read from
// the interface that carries the default route. Only global unicast addresses
// that are not private, CGNAT, loopback, link-local or multicast qualify, so a
// node behind NAT reports nothing instead of an address no client can reach. The
// panel needs this because a subscription endpoint without an address is
// unusable; a wrong one is worse. Nothing here leaves the host.
func publicIP(iface string) string {
	if iface == "" {
		return ""
	}
	device, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, err := device.Addrs()
	if err != nil {
		return ""
	}
	return netaddr.Select(addrs)
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
