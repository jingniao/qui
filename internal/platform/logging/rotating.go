package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"proxycontrol/internal/platform/filelock"
)

const AgentLogLimit = 100 << 20

// RotatingWriter has one process owner. A supervisor forwards worker stderr to
// the same writer so all Agent runtime logs share the capacity limit.
type RotatingWriter struct {
	mu          sync.Mutex
	directory   string
	limit, size int64
	count       int
	file        *os.File
	owner       *filelock.Lock
	failure     error
	report      func(error)
}

func OpenAgentLogs(directory string, report func(error)) (*RotatingWriter, error) {
	return openRotating(directory, AgentLogLimit/10, 10, report)
}

// WorkerOutput keeps the supervisor's stderr pipe drained after a reported
// storage failure. Returning a write error to os/exec would close its pipe and
// terminate the worker with SIGPIPE. The original error remains on Close;
// report announces that subsequent logs are discarded until restart.
func (w *RotatingWriter) WorkerOutput() io.Writer { return workerOutput{w} }

type workerOutput struct{ writer *RotatingWriter }

func (output workerOutput) Write(data []byte) (int, error) {
	_, _ = output.writer.Write(data)
	return len(data), nil
}

func openRotating(directory string, limit int64, count int, report func(error)) (_ *RotatingWriter, resultErr error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("Agent log directory must not be a link")
	}
	owner, err := filelock.Try(filepath.Join(directory, ".writer.lock"))
	if err != nil {
		return nil, err
	}
	w := &RotatingWriter{directory: directory, limit: limit, count: count, owner: owner, report: report}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, owner.Close())
		}
	}()
	for index := range count {
		if err := w.checkFile(index); err != nil {
			return nil, err
		}
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *RotatingWriter) path(index int) string {
	name := "agent.log"
	if index > 0 {
		name += "." + strconv.Itoa(index)
	}
	return filepath.Join(w.directory, name)
}

func (w *RotatingWriter) checkFile(index int) error {
	path := w.path(index)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("Agent log must be a regular file: %s", path)
	}
	if info.Size() > w.limit {
		return fmt.Errorf("Agent log exceeds file capacity: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(filelock.CheckSingleLink(f), f.Close())
}

func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.path(0), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return errors.Join(err, f.Close())
	}
	w.file, w.size = f, info.Size()
	return nil
}

func (w *RotatingWriter) rotate() error {
	for index := range w.count {
		if err := w.checkFile(index); err != nil {
			return err
		}
	}
	if err := w.file.Close(); err != nil {
		w.file = nil
		return err
	}
	w.file = nil
	if err := os.Remove(w.path(w.count - 1)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for index := w.count - 2; index >= 0; index-- {
		if err := os.Rename(w.path(index), w.path(index+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return w.open()
}

func (w *RotatingWriter) Write(data []byte) (n int, resultErr error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure != nil {
		return 0, w.failure
	}
	if w.file == nil {
		return 0, os.ErrClosed
	}
	defer func() {
		if resultErr != nil {
			w.failure = fmt.Errorf("Agent log writing stopped: %w", resultErr)
			if w.report != nil {
				w.report(w.failure)
			}
		}
	}()
	for len(data) > 0 {
		if w.size == w.limit {
			if err := w.rotate(); err != nil {
				return n, err
			}
		}
		length := min(int64(len(data)), w.limit-w.size)
		written, err := w.file.Write(data[:length])
		n += written
		w.size += int64(written)
		data = data[written:]
		if err != nil {
			return n, err
		}
		if int64(written) != length {
			return n, io.ErrShortWrite
		}
	}
	return n, nil
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.failure
	if w.file != nil {
		err = errors.Join(err, w.file.Close())
		w.file = nil
	}
	if w.owner != nil {
		err = errors.Join(err, w.owner.Close())
		w.owner = nil
	}
	return err
}
