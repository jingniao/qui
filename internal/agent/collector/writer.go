package collector

import (
	"net"
	"net/netip"
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
		// written as an invented domain. The Agent's own TCP health checks open
		// and close every inbound from loopback, and Shadowsocks logs each one
		// this way; those are not observations at all.
		if !loopbackSource(string(line)) {
			w.failed()
		}
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

// loopbackSource reports whether an Xray access line starts from a loopback
// client ("from 127.0.0.1:port", "from tcp:[::1]:port"). The address is only
// inspected here and never kept.
func loopbackSource(line string) bool {
	_, rest, found := strings.Cut(line, "from ")
	if !found {
		return false
	}
	source, _, _ := strings.Cut(rest, " ")
	source = strings.TrimPrefix(strings.TrimPrefix(source, "tcp:"), "udp:")
	host, _, err := net.SplitHostPort(source)
	if err != nil {
		return false
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}
