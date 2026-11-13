package node

import (
	"errors"
	"sync"
	"time"
)

// rejectionLogInterval bounds "agent batch rejected" warnings to one per node,
// batch kind and reason in this interval: a refused batch is redelivered on
// every reconnect, and the log must name the reason without flooding.
const rejectionLogInterval = time.Minute

type rejectionLog struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (l *rejectionLog) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if at, ok := l.last[key]; ok && now.Sub(at) < rejectionLogInterval {
		return false
	}
	// Dropping expired keys keeps the map to the keys logged in the last
	// interval.
	for other, at := range l.last {
		if now.Sub(at) >= rejectionLogInterval {
			delete(l.last, other)
		}
	}
	if l.last == nil {
		l.last = map[string]time.Time{}
	}
	l.last[key] = now
	return true
}

// logBatchRejection records why the panel refused an access or traffic batch:
// the recorder's fixed reason code and revision, never batch contents. Errors
// without a code (malformed batches) are logged as "invalid".
func (c *Control) logBatchRejection(nodeID, kind, batchID string, err error) {
	if c.Logger == nil {
		return
	}
	reason, revision := "invalid", int64(0)
	var coded interface{ RejectionReason() (string, int64) }
	if errors.As(err, &coded) {
		reason, revision = coded.RejectionReason()
	}
	if !c.rejections.allow(nodeID+"\x00"+kind+"\x00"+reason, time.Now()) {
		return
	}
	attributes := []any{"node_id", nodeID, "kind", kind, "reason", reason}
	if batchID != "" {
		attributes = append(attributes, "batch_id", batchID)
	}
	if revision > 0 {
		attributes = append(attributes, "revision", revision)
	}
	c.Logger.Warn("agent batch rejected", attributes...)
}
