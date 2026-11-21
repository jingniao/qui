package reconcile

import (
	"context"
	"proxycontrol/pkg/contracts/desiredstate"
)

// syncHostNetwork writes the helper's desired file and waits for its answer, so
// the rules on the host always mirror the revision the Agent runs. The
// reachability proof afterwards is what makes the change safe: a rule that breaks
// new outbound connections rolls the revision back instead of taking the node off
// the control plane.
func (r *Reconciler) syncHostNetwork(ctx context.Context, network desiredstate.Network) error {
	return r.syncHostNetworkState(ctx, desiredstate.State{Network: &network})
}
