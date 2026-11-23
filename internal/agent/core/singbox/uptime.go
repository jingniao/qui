package singbox

import "time"

// CoreStartedAt is the actual child launch time, never the Agent session time.
func (a *Adapter) CoreStartedAt() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.runningChildLocked() || a.stopPending {
		return time.Time{}
	}
	return a.startedAt
}
