package xray

import "proxycontrol/internal/agent/core"

func (a *Adapter) SupportsAccountTelemetry() bool { return true }

func (a *Adapter) ConfigureRuntime(hooks core.RuntimeHooks) {
	a.TrafficSink = hooks.TrafficSink
	a.AccessSink = hooks.AccessSink
	a.AccessRejected = hooks.AccessRejected
	a.EnableStats = hooks.EnableStats
}
