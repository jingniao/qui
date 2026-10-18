package xray

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The core writes its access log into the Agent's own runtime directory. The
// Agent tails that file instead of a pipe so the core never blocks on a slow
// reader. A plain-HTTP proxy line carries the whole URL, path and query
// included, so raw lines must not stay on disk: the tail empties the file as
// soon as it has read every byte of it, which is at most one poll interval
// (~100 ms), and again when the core stops. The cap below stays as the backstop
// for a burst the reader cannot keep up with.
//
// Accepted trade-off: a line the core appends in the microseconds between the
// size check and the truncate is lost, so the access record undercounts
// slightly; it is never left on disk.
const (
	accessLogName         = "access.log"
	accessLogLimit        = 20 << 20
	accessLogPollInterval = 100 * time.Millisecond
	accessLogChunk        = 64 << 10
)

// accessLogTail follows the core's access log file and hands every byte it
// reads to the line-oriented collector writer. Xray opens the access log with
// O_APPEND, so the Agent can truncate the file while the core keeps writing:
// the next core write lands at the new end of the file. The tail empties the
// file after every complete read, so a raw line, URL path and query included,
// survives on disk for at most one poll interval.
type accessLogTail struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	sink    io.Writer
	limit   int64
	offset  int64
	broken  bool
	closing bool
	closed  bool
	stop    chan struct{}
	done    chan struct{}
}

// accessLogPath returns the Agent-owned access log file of this adapter. The
// path is stable for the adapter's lifetime, so a rollback restart tails the
// same bounded file.
func (a *Adapter) accessLogPath() string {
	return filepath.Join(a.Directory, accessLogName)
}

// closeAccessLogLocked drains and releases the access log tail of the core that
// just exited. The core must be gone: everything it wrote is then in the file
// and is handed to the collector writer before the file is emptied and closed,
// so no raw line is left behind on disk.
func (a *Adapter) closeAccessLogLocked() {
	if a.accessTail == nil {
		return
	}
	_ = a.accessTail.close()
	a.accessTail = nil
}

// newAccessLogTail truncates whatever the previous core left behind, opens the
// file and starts following it. The caller must close it after the core exited.
func newAccessLogTail(path string, sink io.Writer, limit int64) (*accessLogTail, error) {
	if path == "" || sink == nil || limit < 1 {
		return nil, errors.New("access log tail requires a path, a sink and a cap")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	// A new core process starts with an empty access log: the lines of the
	// previous revision were drained when that process stopped.
	if err := file.Truncate(0); err != nil {
		file.Close()
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	tail := &accessLogTail{file: file, path: path, sink: sink, limit: limit, stop: make(chan struct{}), done: make(chan struct{})}
	go tail.run()
	return tail, nil
}

func (t *accessLogTail) run() {
	defer close(t.done)
	ticker := time.NewTicker(accessLogPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.mu.Lock()
			if !t.closed {
				_ = t.readLocked()
				_ = t.boundLocked()
			}
			t.mu.Unlock()
		}
	}
}

// readLocked hands every byte the core has written so far to the collector
// writer. A sink failure disables the sink for this revision but never stops
// the tail: the file still has to stay bounded.
func (t *accessLogTail) readLocked() error {
	buffer := make([]byte, accessLogChunk)
	for {
		count, err := t.file.Read(buffer)
		if count > 0 {
			t.offset += int64(count)
			if !t.broken {
				if _, writeErr := t.sink.Write(buffer[:count]); writeErr != nil {
					t.broken = true
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// boundLocked removes what has been read from the file: once every byte of the
// file has been handed to the parser the file is emptied, so a raw line, URL
// path and query included, does not stay on disk. Reading happens first, so
// only bytes already handed to the parser are discarded. The cap stays as the
// backstop for a burst the reader cannot keep up with; a line the core appends
// between the size check and the truncate is lost, which slightly undercounts
// the access record and is never a leak.
func (t *accessLogTail) boundLocked() error {
	info, err := t.file.Stat()
	if err != nil {
		return err
	}
	// Three cases empty the file and continue at its start: everything in it
	// has been read, it reached the cap, or it shrank below the read position
	// because an operator emptied it. A truncate followed by a rewrite of
	// exactly the same size is not detected, and the Agent never writes the
	// file itself.
	complete := t.offset > 0 && info.Size() == t.offset
	shrunk := info.Size() < t.offset
	if !complete && !shrunk && info.Size() < t.limit {
		return nil
	}
	if err := t.file.Truncate(0); err != nil {
		return err
	}
	t.offset = 0
	_, err = t.file.Seek(0, io.SeekStart)
	return err
}

// close drains the lines the core wrote before it exited, empties the file so
// no raw line is left on disk, then releases it. The caller must only call it
// after the core process is gone.
func (t *accessLogTail) close() error {
	t.mu.Lock()
	if t.closing {
		t.mu.Unlock()
		<-t.done
		return nil
	}
	t.closing = true
	t.mu.Unlock()
	close(t.stop)
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	_ = t.readLocked()
	truncateErr := t.file.Truncate(0)
	t.offset = 0
	t.closed = true
	if err := t.file.Close(); err != nil {
		return err
	}
	return truncateErr
}
