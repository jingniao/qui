package xray

import (
	"context"
	"log/slog"

	"proxycontrol/pkg/contracts/traffic"
)

func (a *Adapter) SampleConnections(ctx context.Context) {
	v, err := a.ConnectionSnapshot(ctx)
	if err != nil || !v.DeviceGateSupported {
		// Observation/probe failure revokes the volatile lease immediately.
		// The Xray adapter independently restores and verifies baseline routing.
		// A failed clear can ask for a core restart, so it is reported once per
		// failure instead of on every sample.
		if clearErr := a.SetDeviceGate(ctx, []byte(`{"ttl_seconds":180,"entries":[]}`)); clearErr != nil {
			if a.gateClearFailed.CompareAndSwap(false, true) {
				slog.Warn("device gate clear failed", "error", clearErr)
			}
		} else {
			a.gateClearFailed.Store(false)
		}
	}
	a.connectionObserver.Record(v, err)
}
func (a *Adapter) ObservedConnections(context.Context) (traffic.ConnectionSnapshot, error) {
	return a.connectionObserver.Snapshot()
}
