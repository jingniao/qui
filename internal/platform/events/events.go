package events

import (
	"encoding/json"
	"fmt"
	"net/http"
	"proxycontrol/internal/panel/auth"
	"sync"
	"time"
)

type Event struct {
	ID   uint64
	Type string
	Data any
}
type Bus struct {
	mu          sync.Mutex
	sequence    uint64
	subscribers map[chan Event]bool
}

func (b *Bus) Publish(kind string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sequence++
	for subscriber := range b.subscribers {
		select {
		case subscriber <- Event{ID: b.sequence, Type: kind, Data: data}:
		default:
		}
	}
}

func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 8
	}
	ch := make(chan Event, buffer)
	b.mu.Lock()
	if b.subscribers == nil {
		b.subscribers = map[chan Event]bool{}
	}
	b.subscribers[ch] = true
	b.mu.Unlock()

	unsubscribe := func() {
		b.mu.Lock()
		delete(b.subscribers, ch)
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

func (b *Bus) Handler(authentication *auth.Handler) http.Handler {
	return authentication.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unavailable", 500)
			return
		}
		channel := make(chan Event, 8)
		b.mu.Lock()
		if b.subscribers == nil {
			b.subscribers = map[chan Event]bool{}
		}
		b.subscribers[channel] = true
		b.mu.Unlock()
		defer func() { b.mu.Lock(); delete(b.subscribers, channel); b.mu.Unlock() }()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()
		cookieName := "proxy_session"
		if authentication.Secure {
			cookieName = "__Host-proxy_session"
		}
		cookie, _ := r.Cookie(cookieName)
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-timer.C:
				if cookie == nil {
					return
				}
				session, err := authentication.Service.Session(r.Context(), cookie.Value)
				if err != nil || !session.IsAuthenticated {
					return
				}
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			case event := <-channel:
				data, err := json.Marshal(event.Data)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.ID, event.Type, data)
				flusher.Flush()
			}
		}
	}), false)
}
