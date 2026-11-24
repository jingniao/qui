// Package hostupdatedb performs the offline file operations used by qui-update.
package hostupdatedb

import "context"

type CopyOptions struct {
	Registry, Account, Source, Destination string
	Restore, OwnerStaged                   bool
}
type PreflightOptions struct{ CheckDirectory, Backup, Checker string }
type CheckResult struct {
	ExitCode int
	Output   []byte
}

func Copy(ctx context.Context, options CopyOptions) error { return copyDatabase(ctx, options) }
func Compare(ctx context.Context, account, source, backup string) (bool, error) {
	return compareDatabase(ctx, account, source, backup)
}
func Preflight(ctx context.Context, options PreflightOptions) (CheckResult, error) {
	return preflight(ctx, options)
}
