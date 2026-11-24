package node

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LivenessFact is display evidence, never a substitute for a committed heartbeat.
// Its lease bounds how long a browser may use a connected in-flight snapshot.
type LivenessFact struct {
	EvaluatedAt       time.Time `json:"evaluatedAt"`
	ValidUntil        time.Time `json:"validUntil"`
	Connected         bool      `json:"connected"`
	SessionID         uint64    `json:"sessionId"`
	LastReceivedAt    time.Time `json:"lastReceivedAt"`
	InFlightCount     int       `json:"inFlightCount"`
	OldestInFlightAt  time.Time `json:"oldestInFlightAt"`
	ProcessingStage   string    `json:"processingStage"`
	ProcessingDelayed bool      `json:"processingDelayed"`
	DisconnectedAt    time.Time `json:"disconnectedAt"`
}

type liveSession struct {
	id, fingerprint string
	token           uint64
}
type pendingReceipt struct {
	received time.Time
	stage    string
}
type livenessEntry struct {
	session                        liveSession
	established, connected         bool
	received, disconnected, logged time.Time
	pending                        map[uint64]pendingReceipt
}

// One store belongs to the server process. No database work or callback runs
// while its lock is held. A nil store denotes a database-only reader.
type Liveness struct {
	mu      sync.Mutex
	next    uint64
	items   map[string]*livenessEntry
	changed func(string)
}

func NewLiveness(changed func(string)) *Liveness {
	return &Liveness{items: map[string]*livenessEntry{}, changed: changed}
}
func (l *Liveness) notify(id string) {
	if l.changed != nil {
		l.changed(id)
	}
}
func (l *Liveness) Connect(id, fingerprint string, now time.Time) liveSession {
	if l == nil {
		return liveSession{}
	}
	l.mu.Lock()
	l.next++
	s := liveSession{id, fingerprint, l.next}
	l.items[id] = &livenessEntry{session: s, connected: true, received: now, pending: map[uint64]pendingReceipt{}}
	l.mu.Unlock()
	l.notify(id)
	return s
}
func (l *Liveness) mutate(s liveSession, change func(*livenessEntry)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	entry := l.items[s.id]
	ok := entry != nil && entry.session == s
	if ok {
		change(entry)
	}
	l.mu.Unlock()
	if ok {
		l.notify(s.id)
	}
}
func (l *Liveness) Establish(s liveSession) {
	l.mutate(s, func(e *livenessEntry) {
		if e.connected {
			e.established = true
		}
	})
}
func (l *Liveness) Receive(s liveSession, now time.Time) uint64 {
	var receipt uint64
	l.mutate(s, func(e *livenessEntry) {
		if !e.connected {
			return
		}
		l.next++
		receipt = l.next
		e.received = now
		e.pending[receipt] = pendingReceipt{now, "waiting_db"}
	})
	return receipt
}
func (l *Liveness) SendingAck(s liveSession, receipt uint64) {
	l.mutate(s, func(e *livenessEntry) {
		if p, ok := e.pending[receipt]; ok {
			p.stage = "sending_ack"
			e.pending[receipt] = p
		}
	})
}
func (l *Liveness) Complete(s liveSession, receipt uint64) {
	l.mutate(s, func(e *livenessEntry) {
		delete(e.pending, receipt)
		if len(e.pending) == 0 {
			e.logged = time.Time{}
		}
	})
}
func (l *Liveness) Disconnect(s liveSession, now time.Time) {
	l.mutate(s, func(e *livenessEntry) {
		if e.connected {
			e.connected = false
			e.disconnected = now
			clear(e.pending)
		}
	})
}
func (l *Liveness) Invalidate(id string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.items, id)
	l.mu.Unlock()
	l.notify(id)
}
func (l *Liveness) Snapshot(now time.Time) map[string]LivenessFact {
	result := map[string]LivenessFact{}
	if l == nil {
		return result
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, e := range l.items {
		result[id] = e.fact(now)
	}
	return result
}

// Readers supply the configured grace and durable facts. Keep explicit
// disconnects while they still change interpretation (including error states).
func (l *Liveness) forgetExpired(id, status, heartbeat string, seconds int, now time.Time) bool {
	if l == nil {
		return false
	}
	window := max(HeartbeatFreshness, time.Duration(seconds)*time.Second)
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.items[id]
	if e != nil && !e.connected && now.Sub(e.received) >= window && ConfirmedOffline(status, heartbeat, seconds, now) {
		delete(l.items, id)
		return true
	}
	return false
}
func (e *livenessEntry) fact(now time.Time) LivenessFact {
	f := LivenessFact{EvaluatedAt: now, ValidUntil: now.Add(HeartbeatFreshness), SessionID: e.session.token, Connected: e.connected && e.established, LastReceivedAt: e.received, DisconnectedAt: e.disconnected}
	// Hello remains visible as processing work, but does not confer online status.
	f.InFlightCount = len(e.pending)
	for _, p := range e.pending {
		if f.OldestInFlightAt.IsZero() || p.received.Before(f.OldestInFlightAt) {
			f.OldestInFlightAt = p.received
			f.ProcessingStage = p.stage
		}
	}
	f.ProcessingDelayed = f.InFlightCount > 0 && now.Sub(f.OldestInFlightAt) >= 10*time.Second
	if !e.established {
		f.LastReceivedAt = time.Time{}
	}
	return f
}
func (l *Liveness) Run(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			type warning struct {
				id   string
				fact LivenessFact
			}
			var warnings []warning
			l.mu.Lock()
			for id, e := range l.items {
				fact := e.fact(now)
				if fact.ProcessingDelayed && (e.logged.IsZero() || now.Sub(e.logged) >= time.Minute) {
					e.logged = now
					warnings = append(warnings, warning{id, fact})
				}
			}
			l.mu.Unlock()
			for _, w := range warnings {
				logger.WarnContext(ctx, "node message processing delayed", "node", w.id, "session", w.fact.SessionID, "stage", w.fact.ProcessingStage, "age", now.Sub(w.fact.OldestInFlightAt), "in_flight", w.fact.InFlightCount)
				l.notify(w.id)
			}
		}
	}
}

func (f LivenessFact) valid(now time.Time) bool {
	return !f.EvaluatedAt.IsZero() && !now.Before(f.EvaluatedAt.Add(-5*time.Second)) && now.Before(f.ValidUntil)
}
func (f LivenessFact) pending(now time.Time) bool {
	return f.valid(now) && f.Connected && f.InFlightCount > 0
}
func livenessHeartbeat(last string, f LivenessFact, now time.Time) string {
	if !f.valid(now) || f.LastReceivedAt.IsZero() {
		return last
	}
	previous, err := time.Parse(time.RFC3339Nano, last)
	if err != nil || f.LastReceivedAt.After(previous) {
		return f.LastReceivedAt.Format(time.RFC3339Nano)
	}
	return last
}
func ConfirmedOfflineWithLiveness(status, last string, seconds int, now time.Time, f LivenessFact) bool {
	if status == "pending" {
		return false
	}
	if f.pending(now) || f.valid(now) && f.Connected && ConnectionAlive(status, last, now, f) {
		return false
	}
	if f.valid(now) && !f.DisconnectedAt.IsZero() {
		status = "offline"
	}
	return ConfirmedOffline(status, livenessHeartbeat(last, f, now), seconds, now)
}

// ConnectionAlive is used for recovery notices and deletion previews. Grace
// alone must not close an offline event after a disconnected session.
func ConnectionAlive(status, last string, now time.Time, f LivenessFact) bool {
	if f.pending(now) {
		return true
	}
	if f.valid(now) && !f.DisconnectedAt.IsZero() {
		return false
	}
	if status == "pending" || status == "disabled" {
		return false
	}
	when, err := time.Parse(time.RFC3339Nano, livenessHeartbeat(last, f, now))
	age := now.Sub(when)
	return err == nil && age >= -5*time.Second && age < HeartbeatFreshness && (status == "online" || status == "degraded" || f.Connected)
}
func ConnectionRecoveryText(f LivenessFact) string {
	if f.ProcessingDelayed {
		return "节点连接已恢复，面板处理仍有延迟；数据库心跳尚未追平"
	}
	return "节点连接已恢复"
}
