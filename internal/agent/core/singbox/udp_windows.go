//go:build windows

package singbox

import (
	"encoding/binary"
	"golang.org/x/sys/windows"
	"net"
	"strconv"
	"unsafe"
)

var getExtendedUDPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedUdpTable")

// Windows can permit an overlapping UDP wildcard bind. Query actual PID
// ownership instead of inferring health from a competing bind's error code.
func udpEndpointHealthy(endpoint string, pid int) bool {
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		return false
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	family, rowSize, portOffset, pidOffset := uint32(windows.AF_INET), 12, 4, 8
	if ip.To4() == nil {
		family = windows.AF_INET6
		rowSize = 28
		portOffset = 20
		pidOffset = 24
	}
	var size uint32
	status, _, _ := getExtendedUDPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), 1, 0)
	if status != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size < 4 || size > 1<<20 {
		return false
	}
	data := make([]byte, size)
	status, _, _ = getExtendedUDPTable.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(family), 1, 0)
	if status != 0 || size > uint32(len(data)) || size < 4 {
		return false
	}
	count := binary.LittleEndian.Uint32(data[:4])
	if count > uint32((int(size)-4)/rowSize) {
		return false
	}
	for index := 0; index < int(count); index++ {
		row := data[4+index*rowSize : 4+(index+1)*rowSize]
		if binary.LittleEndian.Uint32(row[pidOffset:pidOffset+4]) != uint32(pid) || int(binary.BigEndian.Uint16(row[portOffset:portOffset+2])) != port {
			continue
		}
		var local net.IP
		if family == windows.AF_INET {
			local = net.IP(row[:4])
		} else {
			local = net.IP(row[:16])
		}
		if local.Equal(ip) || local.IsUnspecified() && !ip.IsUnspecified() {
			return true
		}
	}
	return false
}
