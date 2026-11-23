//go:build !linux

package handovercli

import (
	"context"
	"proxycontrol/internal/agent/core"
)

func Run(context.Context, Options) (string, error) { return "", core.ErrHandoverUnsupported }
