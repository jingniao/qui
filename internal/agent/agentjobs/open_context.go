package agentjobs

import (
	"context"
	"errors"
	"proxycontrol/internal/platform/filelock"
	"time"
)

// OpenForStartup tolerates a short concurrent receipt read without mistaking
// an otherwise valid Worker for a failed executable. No other error is retried.
func OpenForStartup(parent context.Context, o Options) (*Store, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		store, err := Open(o)
		if !errors.Is(err, filelock.ErrBusy) {
			return store, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
