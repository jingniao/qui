package core

import (
	"context"
	"errors"
	"math"
	"proxycontrol/pkg/contracts/traffic"
	"time"
)

func NodeCounter(name, epoch string, revision int64, upload, download uint64) (traffic.Counter, error) {
	if upload > math.MaxInt64 || download > math.MaxInt64 {
		return traffic.Counter{}, errors.New("node traffic overflow")
	}
	return traffic.Counter{Kind: "node", Core: name, CoreEpoch: epoch + ".node", Revision: revision, CredentialID: "node", UserID: "node", DeviceID: "node", UploadBytes: int64(upload), DownloadBytes: int64(download), ObservedAt: time.Now().UTC()}, nil
}

// Persist account samples first and the corresponding node total separately.
// Separate immutable batches let old peers drain accounts while retaining node
// totals for a future negotiated session. A failed sink never advances cursors.
func SaveAccountAndNode(ctx context.Context, sink func(context.Context, []traffic.Counter) error, name, epoch string, revision int64, counters []traffic.Counter) error {
	if err := sink(ctx, counters); err != nil {
		return err
	}
	var upload, download uint64
	for _, c := range counters {
		if c.UploadBytes < 0 || c.DownloadBytes < 0 || uint64(c.UploadBytes) > math.MaxInt64-upload || uint64(c.DownloadBytes) > math.MaxInt64-download {
			return errors.New("node traffic overflow")
		}
		upload += uint64(c.UploadBytes)
		download += uint64(c.DownloadBytes)
	}
	node, err := NodeCounter(name, epoch, revision, upload, download)
	if err != nil {
		return err
	}
	return sink(ctx, []traffic.Counter{node})
}
