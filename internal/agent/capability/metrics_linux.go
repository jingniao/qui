//go:build linux

package capability

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func readSystem(directory string) systemMetrics {
	result := systemMetrics{}
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		fields := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
		if len(fields) >= 9 && fields[0] == "cpu" {
			valid := true
			for index := 1; index <= 8; index++ {
				value, err := strconv.ParseUint(fields[index], 10, 64)
				if err != nil {
					valid = false
					break
				}
				result.cpu.total += value
				if index == 4 || index == 5 {
					result.cpu.idle += value
				}
			}
			result.cpu.valid = valid
		}
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, available uint64
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				total = value
			case "MemAvailable:":
				available = value
			}
		}
		if total > 0 && available <= total {
			result.memory = percent(float64(total-available) / float64(total) * 100)
		}
	}
	var disk syscall.Statfs_t
	if err := syscall.Statfs(directory, &disk); err == nil && disk.Blocks > 0 && disk.Bavail <= disk.Blocks {
		result.disk = percent(float64(disk.Blocks-disk.Bavail) / float64(disk.Blocks) * 100)
	}
	return result
}
