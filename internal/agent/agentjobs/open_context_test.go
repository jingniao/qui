package agentjobs

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestStartupOpenWaitsOnlyForLockAndHonorsCancellation(t *testing.T) {
	o, _ := fixture(t)
	held := openTest(t, o)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	if s, err := OpenForStartup(ctx, o); !errors.Is(err, context.DeadlineExceeded) || s != nil {
		t.Fatal("lock wait ignored deadline", err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { time.Sleep(50 * time.Millisecond); done <- held.Close() }()
	s, err := OpenForStartup(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.Checkpoint, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if s, err := OpenForStartup(ctx, o); err == nil || s != nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("corruption was retried as contention", err)
	}
}
