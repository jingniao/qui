//go:build !linux

package hostupdatedb

import (
	"context"
	"errors"
)

var errLinux = errors.New("database update operations require Linux")

func copyDatabase(context.Context, CopyOptions) error                       { return errLinux }
func compareDatabase(context.Context, string, string, string) (bool, error) { return false, errLinux }
func preflight(context.Context, PreflightOptions) (CheckResult, error) {
	return CheckResult{}, errLinux
}
