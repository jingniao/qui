package statistics

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"sync"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/db"
)

const refreshInterval = 30 * time.Second
const freshAge = 35 * time.Second

type View struct {
	Generation  string     `json:"generation"`
	Status      string     `json:"status"`
	Version     uint64     `json:"version"`
	AsOf        *time.Time `json:"asOf,omitempty"`
	GeneratedAt *time.Time `json:"generatedAt,omitempty"`
	AgeSeconds  *float64   `json:"ageSeconds,omitempty"`
	Error       string     `json:"error,omitempty"`
	Data        *Data      `json:"data,omitempty"`
}

type Service struct {
	pool       *sql.DB
	nodes      *node.Repository
	Now        func() time.Time
	generation string

	mu        sync.Mutex
	last      *snapshot
	failed    *configuration
	lastErr   error
	requested *configuration
	wake      chan struct{}
	stopped   bool
}

func New(pool *sql.DB, nodes *node.Repository) *Service {
	return &Service{pool: pool, nodes: nodes, Now: time.Now, generation: rand.Text(), wake: make(chan struct{}, 1)}
}

// Run owns the only producer. Ticks are measured from the start of the pass
// so the computation time is included in the age visible to readers.
func (s *Service) Run(ctx context.Context) {
	ticks := time.NewTicker(refreshInterval)
	defer ticks.Stop()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.last, s.failed, s.requested = nil, nil, nil
		s.stopped = true
	}()
	s.refresh(ctx)
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-ticks.C:
		case <-s.wake:
			s.mu.Lock()
			requested := s.requested != nil
			s.mu.Unlock()
			if !requested {
				continue
			}
		}
		s.refresh(ctx)
	}
}

func (s *Service) refresh(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, refreshInterval)
	defer cancel()
	var c *configuration
	var next *snapshot
	asOf := s.Now().UTC()
	tx, err := db.BeginRead(ctx, s.pool)
	if err == nil {
		c, err = s.configuration(ctx, tx, asOf)
		if err == nil {
			next, err = s.build(ctx, tx, c, asOf)
		}
		tx.Close()
	}
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The complete version is reusable only for the same configuration. Keep
	// both sets of counters and their original timestamp when machine data fails.
	if err == nil && next.Data.MachineError != "" && s.last != nil && s.last.Data.MachineError == "" && c.matches(s.last.config) {
		err = errors.New(next.Data.MachineError)
	}
	s.failed, s.lastErr = c, err
	if c.matches(s.requested) {
		s.requested = nil
	}
	if err == nil {
		generated := s.Now().UTC()
		next.GeneratedAt = &generated
		next.Generation = s.generation
		next.Version = 1
		if s.last != nil {
			next.Version = s.last.Version + 1
		}
		s.last = next
	}
}

// Read checks the small configuration key in a fresh read transaction. It
// never queries historical traffic. A changed key suppresses the old data
// immediately, even when an older producer is just finishing.
func (s *Service) Read(ctx context.Context, id string) (View, error) {
	now := s.Now().UTC()
	tx, err := db.BeginRead(ctx, s.pool)
	if err != nil {
		return View{}, err
	}
	current, err := s.configuration(ctx, tx, now)
	tx.Close()
	if err != nil {
		return View{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return View{}, errors.New("statistics worker stopped")
	}
	if s.last == nil || !current.matches(s.last.config) {
		if s.lastErr != nil && (s.failed == nil || current.matches(s.failed)) {
			return View{Generation: s.generation, Status: "error", Error: s.lastErr.Error()}, nil
		}
		if !current.matches(s.requested) {
			s.requested = current
			select {
			case s.wake <- struct{}{}:
			default:
			}
		}
		return View{Generation: s.generation, Status: "pending"}, nil
	}
	v, err := s.last.forNode(id)
	if err != nil {
		return View{}, err
	}
	age := s.Now().UTC().Sub(*v.AsOf)
	seconds := age.Seconds()
	v.Status = "ready"
	v.AgeSeconds = &seconds
	if age > freshAge {
		v.Status = "stale"
	}
	if s.lastErr != nil && (s.failed == nil || current.matches(s.failed)) {
		v.Status = "stale"
		v.Error = s.lastErr.Error()
	}
	return v, nil
}
