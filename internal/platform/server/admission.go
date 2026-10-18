package server

import (
	"net/http"
	"sync"
)

type admission struct {
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func (a *admission) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			http.Error(w, "Service is stopping", 503)
			return
		}
		a.active.Add(1)
		a.mu.Unlock()
		defer a.active.Done()
		next.ServeHTTP(w, r)
	})
}
func (a *admission) stop() { a.mu.Lock(); a.closed = true; a.mu.Unlock() }
func (a *admission) wait() { a.active.Wait() }
