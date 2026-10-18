//go:build linux

package core

import (
	"io"
	"os"
	"strings"
)

// maxEnvironmentFactBytes bounds every fixed OS fact read. These files are tiny;
// a larger read is an unobservable environment, never a guess.
const maxEnvironmentFactBytes = 4096

// readEnvironmentFact performs a full, bounded read. A single Read could return
// a short (truncated) value that still looks like a valid fact, so the whole
// bounded stream is consumed before the fact is accepted.
func readEnvironmentFact(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxEnvironmentFactBytes+1))
	if err != nil || len(data) > maxEnvironmentFactBytes {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// observeOSEnvironment binds the machine-id (stable per OS install), the kernel
// boot_id (stable per boot) and the PID namespace link (stable per namespace).
// Each is an independent kernel-owned fact; a missing one stays empty rather
// than being fabricated.
func observeOSEnvironment() OSEnvironment {
	namespace := ""
	if target, err := os.Readlink("/proc/self/ns/pid"); err == nil {
		namespace = target
	}
	return OSEnvironment{
		HostID:      readEnvironmentFact("/etc/machine-id"),
		BootID:      readEnvironmentFact("/proc/sys/kernel/random/boot_id"),
		NamespaceID: namespace,
	}
}

func hexDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// validMachineID matches the 32-hex-character systemd/DBus machine-id.
func validMachineID(value string) bool {
	return len(value) == 32 && hexDigits(value)
}

// validBootUUID matches the canonical 8-4-4-4-12 kernel boot_id.
func validBootUUID(value string) bool {
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

// validPIDNamespace matches the /proc/self/ns/pid link form "pid:[<inode>]".
func validPIDNamespace(value string) bool {
	const prefix = "pid:["
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, "]") {
		return false
	}
	digits := value[len(prefix) : len(value)-1]
	if digits == "" {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// completeEnvironment reports whether all three fixed facts are present and
// well formed. A short or malformed fact is unobservable, never an exit.
func completeEnvironment(environment OSEnvironment) bool {
	return validMachineID(environment.HostID) && validBootUUID(environment.BootID) && validPIDNamespace(environment.NamespaceID)
}
