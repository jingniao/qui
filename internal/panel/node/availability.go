package node

import "time"

// ConfirmedOffline is the shared display/subscription interpretation. Control
// connection admission still uses its own heartbeat freshness requirement.
func ConfirmedOffline(status, lastHeartbeat string, offlineAfterSeconds int, now time.Time) bool {
	if status == "pending" {
		return false
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, lastHeartbeat)
	age := now.Sub(heartbeat)
	valid := err == nil && age >= -5*time.Second
	fresh := valid && age < 45*time.Second
	if status != "offline" && (fresh || status == "error") {
		return false
	}
	return !valid || age >= time.Duration(offlineAfterSeconds)*time.Second
}
