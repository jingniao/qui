package logging

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"proxycontrol/internal/platform/filelock"
)

func TestRotatingLogsBoundedAndReopened(t *testing.T) {
	dir := t.TempDir()
	w, err := openRotating(dir, 16, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.Repeat([]byte("12345678"), 10)
	if n, err := w.Write(input); err != nil || n != len(input) {
		t.Fatalf("write %d: %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	assertLogSize(t, dir, 48)
	w, err = openRotating(dir, 16, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	assertLogSize(t, dir, 35)
	got, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil || string(got) != "new" {
		t.Fatalf("latest %q: %v", got, err)
	}
}

func TestRotatingLogsConcurrentAndExclusive(t *testing.T) {
	dir := t.TempDir()
	w, err := openRotating(dir, 32, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openRotating(dir, 32, 3, nil); !errors.Is(err, filelock.ErrBusy) {
		t.Fatalf("duplicate writer: %v", err)
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 20 {
				if _, err := w.Write([]byte("test")); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	group.Wait()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	assertLogSize(t, dir, 96)
}

func TestRotatingLogsFailureIsReported(t *testing.T) {
	dir := t.TempDir()
	var reported error
	w, err := openRotating(dir, 4, 2, func(err error) { reported = err })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("full")); err != nil {
		t.Fatal(err)
	}
	// A directory cannot be silently removed or replaced as a log archive.
	archive := filepath.Join(dir, "agent.log.1")
	if err := os.Mkdir(archive, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("next")); err == nil {
		t.Fatal("rotation failure hidden")
	}
	if reported == nil {
		t.Fatal("rotation error not reported")
	}
	if err := w.Close(); err == nil {
		t.Fatal("close lost write failure")
	}
	if info, err := os.Stat(archive); err != nil || !info.IsDir() {
		t.Fatalf("unexpected archive deletion: %v", err)
	}
}

func TestRotatingLogsWorkerPipeDrainedAfterFailure(t *testing.T) {
	dir := t.TempDir()
	var reported error
	w, err := openRotating(dir, 4, 2, func(err error) { reported = err })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "agent.log.1"), 0700); err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("worker output\n"), 50)
	written, err := io.Copy(w.WorkerOutput(), bytes.NewReader(data))
	if err != nil || written != int64(len(data)) {
		t.Fatalf("worker pipe stopped: %d %v", written, err)
	}
	if reported == nil {
		t.Fatal("lost logging failure report")
	}
	if err := w.Close(); err == nil {
		t.Fatal("lost persistent write error")
	}
}

func assertLogSize(t *testing.T, dir string, want int64) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "agent.log*"))
	if err != nil {
		t.Fatal(err)
	}
	var size int64
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		size += info.Size()
	}
	if size != want {
		t.Fatalf("log bytes = %d, want %d", size, want)
	}
}
