//go:build windows

package capability

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

var kernel = windows.NewLazySystemDLL("kernel32.dll")
var systemTimes = kernel.NewProc("GetSystemTimes")
var memoryStatus = kernel.NewProc("GlobalMemoryStatusEx")

type memoryInfo struct {
	Length, Load                                                                                                                 uint32
	TotalPhysical, AvailablePhysical, TotalPageFile, AvailablePageFile, TotalVirtual, AvailableVirtual, AvailableExtendedVirtual uint64
}

func ticks(value windows.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}
func readSystem(directory string) systemMetrics {
	result := systemMetrics{}
	var idle, kernelTime, user windows.Filetime
	if ok, _, _ := systemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernelTime)), uintptr(unsafe.Pointer(&user))); ok != 0 {
		result.cpu = counters{total: ticks(kernelTime) + ticks(user), idle: ticks(idle), valid: true}
	}
	memory := memoryInfo{}
	memory.Length = uint32(unsafe.Sizeof(memory))
	if ok, _, _ := memoryStatus.Call(uintptr(unsafe.Pointer(&memory))); ok != 0 && memory.TotalPhysical > 0 && memory.AvailablePhysical <= memory.TotalPhysical {
		result.memory = percent(float64(memory.TotalPhysical-memory.AvailablePhysical) / float64(memory.TotalPhysical) * 100)
	}
	path, err := windows.UTF16PtrFromString(directory)
	if err == nil {
		var available, total, free uint64
		if err := windows.GetDiskFreeSpaceEx(path, &available, &total, &free); err == nil && total > 0 && available <= total {
			result.disk = percent(float64(total-available) / float64(total) * 100)
		}
	}
	return result
}
