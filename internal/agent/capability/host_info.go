package capability

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"proxycontrol/internal/agent/tuning"
	pb "proxycontrol/pkg/contracts/agentpb"
)

func memValues(root string) map[string]uint64 {
	values := map[string]uint64{}
	raw, err := os.ReadFile(filepath.Join(root, "meminfo"))
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[2] != "kB" {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err == nil && n <= math.MaxUint64/1024 {
			values[strings.TrimSuffix(f[0], ":")] = n * 1024
		}
	}
	return values
}

func readHostInfo(proc, release string) *pb.HostInfo {
	h := &pb.HostInfo{Architecture: runtime.GOARCH}
	if raw, err := os.ReadFile(filepath.Join(proc, "cpuinfo")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k == "processor" {
				h.CpuCores++
			}
			if h.CpuModel == "" && (k == "model name" || k == "Hardware" || k == "Processor") {
				h.CpuModel = v
			}
		}
	}
	if raw, err := os.ReadFile(filepath.Join(proc, "sys/kernel/osrelease")); err == nil {
		h.KernelVersion = strings.TrimSpace(string(raw))
	}
	mem := memValues(proc)
	h.MemoryTotalBytes = mem["MemTotal"]
	h.SwapTotalBytes = mem["SwapTotal"]
	if raw, err := os.ReadFile(release); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				v = strings.TrimSpace(v)
				if strings.HasPrefix(v, `"`) {
					if s, err := strconv.Unquote(v); err == nil {
						h.OsPrettyName = s
					}
				} else {
					h.OsPrettyName = strings.Trim(v, "'")
				}
			}
		}
	}
	return h
}

func (s *Sampler) HostInfo() *pb.HostInfo {
	if runtime.GOOS != "linux" {
		return nil
	}
	h := readHostInfo("/proc", "/etc/os-release")
	total, _ := diskBytes(s.directory)
	if total != nil {
		h.DiskTotalBytes = *total
	}
	return h
}

func socketCount(root, name string) *uint64 {
	var total uint64
	// Both families must be readable; a partial count would misrepresent the host.
	for _, suffix := range []string{"", "6"} {
		f, err := os.Open(filepath.Join(root, "net", name+suffix))
		if err != nil {
			return nil
		}
		scanner := bufio.NewScanner(f)
		if !scanner.Scan() || !strings.Contains(scanner.Text(), "local_address") {
			f.Close()
			return nil
		}
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) == 0 {
				continue
			}
			if len(fields) < 10 || !strings.HasSuffix(fields[0], ":") {
				f.Close()
				return nil
			}
			total++
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return nil
		}
	}
	return &total
}

func readExtraHost(root string, h *pb.Heartbeat) {
	if raw, err := os.ReadFile(filepath.Join(root, "loadavg")); err == nil {
		f := strings.Fields(string(raw))
		if len(f) >= 3 {
			for i, p := range []**float64{&h.Load1, &h.Load5, &h.Load15} {
				v, err := strconv.ParseFloat(f[i], 64)
				if err == nil && v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
					*p = &v
				}
			}
		}
	}
	// /proc/stat's "processes" counts forks since boot, not current processes.
	if entries, err := os.ReadDir(root); err == nil {
		var count uint64
		for _, entry := range entries {
			if entry.IsDir() {
				if id, err := strconv.ParseUint(entry.Name(), 10, 64); err == nil && id > 0 {
					count++
				}
			}
		}
		h.ProcessCount = &count
	}
	h.TcpConnections = socketCount(root, "tcp")
	h.UdpSockets = socketCount(root, "udp")
	m := memValues(root)
	for _, item := range []struct {
		total, free string
		target      **uint64
	}{{"MemTotal", "MemAvailable", &h.MemoryUsedBytes}, {"SwapTotal", "SwapFree", &h.SwapUsedBytes}} {
		total, a := m[item.total]
		free, b := m[item.free]
		if a && b && free <= total {
			v := total - free
			*item.target = &v
		}
	}
	h.TuningCurrent = tuning.Current(filepath.Join(root, "sys"))
}
