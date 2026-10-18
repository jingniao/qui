package collector

import (
	"proxycontrol/pkg/contracts/access"
	"strings"
	"sync"
	"time"
)

// Writer never stores raw logs. It bounds a transient partial line and reports
// rejected events numerically; neither parse errors nor log content are exposed.
type Writer struct {
	mu         sync.Mutex
	context    *Context
	submit     func(access.Rollup) error
	reject     func()
	buffer     []byte
	discarding bool
	now        func() time.Time
}

func NewWriter(context *Context, submit func(access.Rollup) error, reject func()) *Writer {
	return &Writer{context: context, submit: submit, reject: reject, now: time.Now}
}

func (w *Writer) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, value := range data {
		if value == '\n' {
			if !w.discarding {
				w.line()
			}
			w.buffer = w.buffer[:0]
			w.discarding = false
			continue
		}
		if w.discarding {
			continue
		}
		if len(w.buffer) >= 8192 {
			w.buffer = w.buffer[:0]
			w.discarding = true
			w.failed()
			continue
		}
		w.buffer = append(w.buffer, value)
	}
	return len(data), nil
}

func (w *Writer) line() {
	line := w.buffer
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	event, err := ParseXrayAccessLine(string(line))
	if err != nil {
		if strings.Contains(string(line), " accepted ") || strings.Contains(string(line), " rejected ") {
			w.failed()
		}
		return
	}
	if event.Domain == "" && event.TargetIP == "" {
		// Upstream recorded a rejection that has no destination, for example a
		// failed inbound handshake. There is no domain or address to attribute,
		// so the observation is counted as unattributed instead of being
		// written as an invented domain.
		w.failed()
		return
	}
	record, include, err := w.context.Record(event, w.now())
	if err != nil {
		w.failed()
		return
	}
	if !include {
		return
	}
	if err := w.submit(record); err != nil {
		w.failed()
	}
}
func (w *Writer) failed() {
	if w.reject != nil {
		w.reject()
	}
}
