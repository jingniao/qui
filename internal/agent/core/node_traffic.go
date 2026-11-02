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

// NodeTotal sums the account counters and any node-only bytes, such as Xray
// forward inbound counters that belong to no account, into one cumulative node
// total for a process epoch.
func NodeTotal(name, epoch string, revision int64, counters []traffic.Counter, extra ...StatsBatch) (traffic.Counter, error) {
	var upload, download uint64
	for _, c := range counters {
		if c.UploadBytes < 0 || c.DownloadBytes < 0 || uint64(c.UploadBytes) > math.MaxInt64-upload || uint64(c.DownloadBytes) > math.MaxInt64-download {
			return traffic.Counter{}, errors.New("node traffic overflow")
		}
		upload += uint64(c.UploadBytes)
		download += uint64(c.DownloadBytes)
	}
	for _, batch := range extra {
		if batch.Upload > math.MaxInt64-upload || batch.Download > math.MaxInt64-download {
			return traffic.Counter{}, errors.New("node traffic overflow")
		}
		upload += batch.Upload
		download += batch.Download
	}
	return NodeCounter(name, epoch, revision, upload, download)
}

// Persist account samples first and the corresponding node total separately.
// Separate immutable batches let old peers drain accounts while retaining node
// totals for a future negotiated session. A failed sink never advances cursors.
// Node-only bytes (extra) never enter the account batch.
func SaveAccountAndNode(ctx context.Context, sink func(context.Context, []traffic.Counter) error, name, epoch string, revision int64, counters []traffic.Counter, extra ...StatsBatch) error {
	node, err := NodeTotal(name, epoch, revision, counters, extra...)
	if err != nil {
		return err
	}
	if err := sink(ctx, counters); err != nil {
		return err
	}
	return sink(ctx, []traffic.Counter{node})
}
