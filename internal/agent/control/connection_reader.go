package control

import (
	"context"
	"proxycontrol/pkg/contracts/traffic"
)

func connectionReader(source any) func(context.Context) (traffic.ConnectionSnapshot, error) {
	if cached, ok := source.(interface {
		ObservedConnections(context.Context) (traffic.ConnectionSnapshot, error)
	}); ok {
		return cached.ObservedConnections
	}
	// Optional runtimes and older test adapters can still provide count-only
	// snapshots; their omitted observation version never opens the gate path.
	if legacy, ok := source.(interface {
		ConnectionSnapshot(context.Context) (traffic.ConnectionSnapshot, error)
	}); ok {
		return legacy.ConnectionSnapshot
	}
	return nil
}
