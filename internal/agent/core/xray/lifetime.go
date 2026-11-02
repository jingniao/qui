package xray

import "proxycontrol/internal/agent/core"

// runMarker binds the shared core-run marker material to this adapter: its runtime
// directory, the node identity it starts children for and its own core name. The
// binding is resolved on use because adapters are plain values built by the node
// wiring and by tests, never by a constructor.
func (a *Adapter) runMarker() *core.RunMarker {
	a.marker.Directory, a.marker.NodeID, a.marker.Core = a.Directory, a.NodeID, a.Name()
	return &a.marker
}

// runningChildLocked reports the child this adapter holds as the running core: the
// durable marker must also name the epoch the statistics are attributed to, so a
// start that never settled keeps its child without proving that run.
func (a *Adapter) runningChildLocked() bool {
	return a.process != nil && a.marker.Epoch == a.statsEpoch
}

// lifecycleReadyLocked refuses to open the data plane while a previously recorded
// child core is not proven to have exited.
func (a *Adapter) lifecycleReadyLocked() error { return a.runMarker().Ready(a.runningChildLocked()) }
