// Package handovercli implements the fixed local root handover commands.
package handovercli

import "context"

type Options struct {
	Operation    string
	ConfigPath   string
	Scope        string
	Transaction  string
	TargetSHA256 string
	UpdaterPID   int
	RequestID    string
	Reason       string
}

// Arm is the root helper entry point for a journalled worker installation.
// Call after MarkStarted and before the first worker Stop. The caller must
// hold its update reservation; this function never grants privilege itself.
func Arm(ctx context.Context, options Options) (string, error) {
	options.Operation = "core-handover-arm"
	return Run(ctx, options)
}
