package singbox

import (
	"context"
	"proxycontrol/pkg/contracts/traffic"
)

func (a *Adapter) SampleConnections(ctx context.Context) {
	v, err := a.ConnectionSnapshot(ctx)
	if err != nil || !v.DeviceGateSupported {
		// Observation/probe failure revokes the volatile lease immediately.
		// The Xray adapter independently restores and verifies baseline routing.
		_ = a.SetDeviceGate(ctx, []byte(`{"ttl_seconds":180,"entries":[]}`))
	}
	a.connectionObserver.Record(v, err)
}
func (a *Adapter) ObservedConnections(context.Context) (traffic.ConnectionSnapshot, error) {
	return a.connectionObserver.Snapshot()
}
