package collector

import (
	"context"
	"io"
	"log/slog"
	"proxycontrol/internal/agent/spool"
	"proxycontrol/pkg/contracts/access"
	"testing"
	"time"
)

func TestRuntimeShutdownFlushAggregatesAndPersists(t *testing.T) {
	ctx := context.Background()
	queue, err := spool.Open(ctx, t.TempDir(), "node", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	runtime, err := NewRuntime(queue, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	record := access.Rollup{Bucket: time.Now().UTC(), NodeID: "node", Revision: 1, InboundProtocol: "socks5", Domain: "example.test", TargetPort: 443, Network: "tcp", Action: "Direct", Allowed: true, Connections: 1}
	for index := 0; index < 50; index++ {
		if err := runtime.Submit(record); err != nil {
			t.Fatal(err)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	go runtime.Run(runCtx)
	cancel()
	runtime.Wait()
	batch, err := queue.Pending(ctx)
	if err != nil || batch == nil || len(batch.Records) != 1 || batch.Records[0].Connections != 50 {
		t.Fatal("shutdown lost pending access aggregation", batch, err)
	}
}
