package node

// IgnoredDeploymentResult is a valid receipt for a target already finalized by
// the panel. The control transport acknowledges it without changing the target.
type IgnoredDeploymentResult struct{ Reason string }

func (e *IgnoredDeploymentResult) Error() string {
	return "deployment target already finalized: " + e.Reason
}
func (e *IgnoredDeploymentResult) IgnoredDeploymentReason() string { return e.Reason }
