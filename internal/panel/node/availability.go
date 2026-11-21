package node

import "time"

// DisplayStatus interprets connection state for user-facing views. It does not
// change heartbeat freshness for metrics or control operations.
func DisplayStatus(n Node, now time.Time) string {
	if n.DeletedAt != "" {
		return "deleted"
	}
	if !n.Enabled || n.Status == "disabled" {
		return "disabled"
	}
	if n.Maintenance {
		return "maintenance"
	}
	if n.Status == "pending" {
		return "pending"
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, n.LastHeartbeat)
	age := now.Sub(heartbeat)
	fresh := err == nil && age >= -5*time.Second && age < 45*time.Second
	if n.Status == "offline" || !fresh && n.Status != "error" {
		if ConfirmedOffline(n.Status, n.LastHeartbeat, n.OfflineAfterSeconds, now) {
			return "offline"
		}
		return "reconnecting"
	}
	if fresh && displayCoreUnhealthy(n) {
		return "core_error"
	}
	return n.Status
}

func displayCoreUnhealthy(n Node) bool {
	if n.Heartbeat != nil && n.Heartbeat.CoreHealthy != nil && !n.Heartbeat.GetCoreHealthy() {
		return true
	}
	if len(n.Capabilities.GetCores()) == 0 {
		for _, status := range n.Heartbeat.GetCores() {
			if CoreNameValid(status.GetName()) && status.GetName() == n.Capabilities.GetCoreName() && !status.GetHealthy() {
				return true
			}
		}
	}
	for _, core := range n.Capabilities.GetCores() {
		if !CoreNameValid(core.GetName()) {
			continue
		}
		healthy := core.Healthy
		for _, status := range n.Heartbeat.GetCores() {
			if status.GetName() == core.GetName() {
				healthy = &status.Healthy
				break
			}
		}
		if healthy != nil && !*healthy {
			return true
		}
	}
	return false
}

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
