//go:build windows

package core

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// windowsProcessNamespace declares the process scope Windows actually supports.
// Windows process ids are system-wide within a boot (there is no Linux-style
// per-process PID namespace that would make a PID probe observe another process
// table), so every recorded marker shares this fixed system scope. The host and
// boot facts below remain the real isolation checks: a marker copied to another
// machine or another boot is still never resolved by probing a PID here.
const windowsProcessNamespace = "system"

// systemBootEnvironmentInformation mirrors the native
// SYSTEM_BOOT_ENVIRONMENT_INFORMATION layout returned for
// SystemBootEnvironmentInformation.
type systemBootEnvironmentInformation struct {
	BootIdentifier windows.GUID
	FirmwareType   uint32
	BootFlags      uint64
}

// readMachineGuid reads the fixed per-OS-install MachineGuid from the registry.
// It is a bounded registry value read (no command, no shell); a missing or
// oversized value is unobservable rather than fabricated.
func readMachineGuid() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue("MachineGuid")
	if err != nil || value == "" || len(value) > 64 {
		return ""
	}
	return value
}

// readBootIdentifier reads the fixed kernel boot GUID for this boot. A query
// failure or a short buffer is unobservable, never an empty-environment exit.
func readBootIdentifier() string {
	var info systemBootEnvironmentInformation
	var returned uint32
	size := uint32(unsafe.Sizeof(info))
	if err := windows.NtQuerySystemInformation(windows.SystemBootEnvironmentInformation, unsafe.Pointer(&info), size, &returned); err != nil || returned < size {
		return ""
	}
	return info.BootIdentifier.String()
}

// observeOSEnvironment binds the per-install MachineGuid, the per-boot kernel
// boot GUID and the fixed Windows process scope. Each is a fixed local fact; a
// missing one stays empty rather than being fabricated, so a marker without a
// complete environment is never read as a proven exit.
func observeOSEnvironment() OSEnvironment {
	return OSEnvironment{
		HostID:      readMachineGuid(),
		BootID:      readBootIdentifier(),
		NamespaceID: windowsProcessNamespace,
	}
}

// validWindowsGUID accepts the canonical 8-4-4-4-12 GUID with optional braces.
func validWindowsGUID(value string) bool {
	value = strings.TrimPrefix(value, "{")
	value = strings.TrimSuffix(value, "}")
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		switch i {
		case 8, 13, 18, 23:
			if value[i] != '-' {
				return false
			}
		default:
			c := value[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// completeEnvironment requires the host and boot GUIDs and the fixed system
// process scope. A missing or malformed fact is unresolved, not an exit.
func completeEnvironment(environment OSEnvironment) bool {
	return validWindowsGUID(environment.HostID) && validWindowsGUID(environment.BootID) && environment.NamespaceID == windowsProcessNamespace
}
