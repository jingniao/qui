package xray

import (
	"context"
	"proxycontrol/internal/agent/core"
	"proxycontrol/pkg/contracts/desiredstate"
	"time"
)

func Render(ctx context.Context, state desiredstate.State) (core.RenderedConfig, error) {
	return RenderAt(ctx, state, time.Now().UTC())
}

func RenderAt(ctx context.Context, state desiredstate.State, at time.Time) (core.RenderedConfig, error) {
	return renderWithOptions(ctx, state, at, RenderOptions{})
}
