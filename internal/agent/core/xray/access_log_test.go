package xray

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureWriter is a concurrency-safe sink for the tail's bytes.
type captureWriter struct {
	mu    sync.Mutex
	value strings.Builder
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.value.Write(data)
}

func (w *captureWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.value.String()
}

func waitForTail(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("access log tail did not follow the core file")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForEmptyAccessLog polls until the tail emptied the file it read. Raw
// access lines, and with them the URL path and query of a plain-HTTP request,
// must not stay on disk after the poll that read them.
func waitForEmptyAccessLog(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("access log unreadable: %v", err)
		}
		if info.Size() == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("raw access lines stayed on disk: %d bytes", info.Size())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAccessLogTailFollowsTheCoreFile proves the Agent reads what the core
// appends, including a partial line, that a stop drains the last lines, and
// that the file is emptied once it has been read and again on the stop, so no
// raw line stays on disk.
func TestAccessLogTailFollowsTheCoreFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), accessLogName)
	sink := &captureWriter{}
	tail, err := newAccessLogTail(path, sink, accessLogLimit)
	if err != nil {
		t.Fatal(err)
	}
	// The tail owns an empty file, so the core always has something to append
	// to and a previous run's lines can never be replayed.
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatalf("access log not created empty: %v %v", info, err)
	}
	appendAccessLog(t, path, "2026/10/17 11:33:29.727545 from tcp:127.0.0.1:63160 accepted tcp:localhost:63157 [entry -> direct]\n")
	waitForTail(t, func() bool { return strings.Contains(sink.String(), "tcp:localhost:63157") })
	// The bytes reached the parser, so the raw line goes: a plain-HTTP line
	// would carry the URL path and query.
	waitForEmptyAccessLog(t, path)
	// A line the core is still writing arrives as bytes; the line-oriented
	// collector writer holds the remainder until its newline, and the raw bytes
	// are gone from the file as soon as they were handed over.
	appendAccessLog(t, path, "2026/10/17 11:33:31.135462 from tcp:127.0.0.1:63165 accepted tcp:[::1]:63157")
	waitForTail(t, func() bool { return strings.Contains(sink.String(), "tcp:[::1]:63157") })
	waitForEmptyAccessLog(t, path)
	// Everything written before the stop is drained even if no poll ran, and
	// the stop leaves nothing behind either.
	appendAccessLog(t, path, " [entry -> direct]\n")
	if err := tail.close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.String(), "[entry -> direct]") {
		t.Fatalf("stop did not drain the core file: %q", sink.String())
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("stopping the tail left raw access lines on disk: %v %v", info, err)
	}
}

// TestAccessLogTailBoundsAndRecoversTheFile proves the file stays bounded: it is
// emptied after every complete read, the cap backstops a burst the reader cannot
// keep up with, and a file an operator emptied is picked up again instead of
// stalling the tail.
func TestAccessLogTailBoundsAndRecoversTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), accessLogName)
	sink := &captureWriter{}
	tail, err := newAccessLogTail(path, sink, 512)
	if err != nil {
		t.Fatal(err)
	}
	defer tail.close()
	for index := 0; index < 8; index++ {
		appendAccessLog(t, path, strings.Repeat("x", 128)+"\n")
	}
	waitForTail(t, func() bool { return strings.Count(sink.String(), "x") > 512 })
	waitForEmptyAccessLog(t, path)
	// The tail keeps working after the rotation.
	appendAccessLog(t, path, strings.Repeat("y", 400)+"\n")
	waitForTail(t, func() bool { return strings.Count(sink.String(), "y") >= 400 })
	waitForEmptyAccessLog(t, path)
	// An operator emptying the file leaves the tail reading the new content
	// from its start.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	appendAccessLog(t, path, "2026/10/17 11:33:34.000000 from 127.0.0.1:63190 accepted tcp:example.test:443 [entry -> direct]\n")
	waitForTail(t, func() bool { return strings.Contains(sink.String(), "tcp:example.test:443") })
	waitForEmptyAccessLog(t, path)
}

func appendAccessLog(t *testing.T, path, data string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(data); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
