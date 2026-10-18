package collector

import (
	"errors"
	"proxycontrol/pkg/contracts/access"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var ErrCapacity = errors.New("access aggregation capacity reached")

// Aggregator is bounded. Callers must flush or spool before retrying a record
// rejected at capacity; it never silently discards a domain or merges identities.
type Aggregator struct {
	mu      sync.Mutex
	records map[string]access.Rollup
	limit   int
	count   atomic.Int64
}

func NewAggregator(limit int) (*Aggregator, error) {
	if limit < 1 || limit > 100000 {
		return nil, errors.New("invalid aggregation capacity")
	}
	return &Aggregator{records: map[string]access.Rollup{}, limit: limit}, nil
}

func (a *Aggregator) Add(record access.Rollup) error {
	if err := record.Normalize(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := record.Key()
	if previous, exists := a.records[key]; exists {
		merged, err := access.Merge(previous, record)
		if err != nil {
			return err
		}
		a.records[key] = merged
		return nil
	}
	if len(a.records) >= a.limit {
		return ErrCapacity
	}
	a.records[key] = record
	a.count.Add(1)
	return nil
}

// Flush calls persistence while holding the aggregation lock and removes rows
// only after it succeeds. A failed disk write leaves the original data intact.
func (a *Aggregator) Flush(before time.Time, persist func([]access.Rollup) error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := []string{}
	for key, record := range a.records {
		if record.Bucket.Before(before.UTC()) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return nil
	}
	records := make([]access.Rollup, 0, len(keys))
	for _, key := range keys {
		records = append(records, a.records[key])
	}
	if err := persist(records); err != nil {
		return err
	}
	for _, key := range keys {
		delete(a.records, key)
	}
	a.count.Add(-int64(len(keys)))
	return nil
}

func (a *Aggregator) Len() int { return int(a.count.Load()) }
