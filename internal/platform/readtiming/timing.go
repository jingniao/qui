// Package readtiming collects opt-in timings without request data or SQL text.
package readtiming

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type contextKey struct{}
type measurement struct {
	Milliseconds float64 `json:"ms"`
	Calls        int     `json:"calls"`
}
type collector struct {
	mu     sync.Mutex
	stages map[string]measurement
}

var sequence atomic.Uint64

// Start is a no-op unless the request passed through Middleware. Names must be
// constants, never SQL, URLs, identifiers or values supplied by the caller.
func Start(ctx context.Context, name string) func() {
	c, _ := ctx.Value(contextKey{}).(*collector)
	if c == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		elapsed := time.Since(start)
		c.mu.Lock()
		defer c.mu.Unlock()
		m := c.stages[name]
		m.Milliseconds += float64(elapsed) / float64(time.Millisecond)
		m.Calls++
		c.stages[name] = m
	}
}

type writer struct {
	http.ResponseWriter
	ctx    context.Context
	status int
}

func (w *writer) TimingContext() context.Context { return w.ctx }
func (w *writer) Unwrap() http.ResponseWriter    { return w.ResponseWriter }
func (w *writer) WriteHeader(status int) {
	if w.status == 0 && status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *writer) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Middleware samples finite GET API responses only. SSE and subscriptions are
// excluded; logs contain the matched route template, never the requested URL.
func Middleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/v1/") || strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Path == "/api/v1/events" {
			next.ServeHTTP(w, r)
			return
		}
		c := &collector{stages: map[string]measurement{}}
		r = r.WithContext(context.WithValue(r.Context(), contextKey{}, c))
		response := &writer{ResponseWriter: w, ctx: r.Context()}
		id := sequence.Add(1)
		start := time.Now()
		next.ServeHTTP(response, r)
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		logger.Info("read timing", "request_id", id, "route", r.Pattern, "status", status, "total_ms", float64(time.Since(start))/float64(time.Millisecond), "stages", c.stages)
	})
}
