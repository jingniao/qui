//go:build linux

package core

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// FileTail delivers complete lines to an io.Writer, including SingboxWriter.
// Partial lines stay in the file. Checkpoint pauses polling until Resume/Detach.
type FileTail struct {
	mu                     sync.Mutex
	file                   *os.File
	path                   string
	sink                   io.Writer
	limit, offset          int64
	device, inode          uint64
	attach, paused, closed bool
	err                    error
	stop, done             chan struct{}
	once                   sync.Once
}

// OpenFileTail with nil checkpoint starts a new run; the caller must first
// prove the previous run exited. Attach requires the original safe file and
// never truncates unread data except at the explicitly logged privacy cap.
func OpenFileTail(path string, sink io.Writer, limit int64, checkpoint *LogCheckpoint) (_ *FileTail, resultErr error) {
	if !filepath.IsAbs(path) || sink == nil || limit < 1 {
		return nil, errors.New("file tail requires absolute path, writer and positive privacy cap")
	}
	if err := checkHandoverDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	if checkpoint == nil {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, f.Close())
		}
	}()
	t := &FileTail{file: f, path: path, sink: sink, limit: limit, attach: checkpoint != nil, stop: make(chan struct{}), done: make(chan struct{})}
	info, err := t.checkLocked()
	if err != nil {
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	t.device, t.inode = uint64(stat.Dev), stat.Ino
	if checkpoint != nil {
		if checkpoint.Path != path || checkpoint.Device != t.device || checkpoint.Inode != t.inode || checkpoint.Offset < 0 || checkpoint.Offset > info.Size() {
			return nil, errors.New("file tail checkpoint identity or offset changed")
		}
		t.offset = checkpoint.Offset
		if t.offset > 0 {
			var b [1]byte
			if _, err := f.ReadAt(b[:], t.offset-1); err != nil {
				return nil, err
			}
			if b[0] != '\n' {
				return nil, errors.New("file tail checkpoint is not a complete-line boundary")
			}
		}
	} else if err := f.Truncate(0); err != nil {
		return nil, err
	}
	go t.run()
	return t, nil
}

func (t *FileTail) checkLocked() (os.FileInfo, error) {
	info, err := t.file.Stat()
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(t.path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || !os.SameFile(info, current) || current.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("file tail identity, ownership, permissions or links are unsafe")
	}
	if info.Size() < t.offset {
		return nil, errors.New("file tail shrank below complete-line checkpoint")
	}
	return info, nil
}

func (t *FileTail) recordLocked(err error) error {
	if err != nil && t.err == nil {
		t.err = err
		slog.Error("core file tail failed", "event", "core_file_tail_failed", "error", err)
	}
	return t.err
}

func (t *FileTail) readLocked(bound bool) error {
	info, err := t.checkLocked()
	if err != nil {
		return t.recordLocked(err)
	}
	if t.err == nil {
		reader := bufio.NewReader(io.NewSectionReader(t.file, t.offset, info.Size()-t.offset))
		for {
			line, readErr := reader.ReadBytes('\n')
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				t.recordLocked(readErr)
				break
			}
			n, writeErr := t.sink.Write(line)
			if writeErr == nil && n != len(line) {
				writeErr = io.ErrShortWrite
			}
			if writeErr != nil {
				t.recordLocked(writeErr)
				break
			}
			t.offset += int64(n)
		}
	}
	if bound {
		after, err := t.checkLocked()
		if err != nil {
			return t.recordLocked(err)
		}
		complete := !t.attach && t.err == nil && t.offset > 0 && t.offset == after.Size()
		if complete || after.Size() >= t.limit {
			reason := "consumed"
			if !complete {
				reason = "privacy_cap"
			}
			// Even caught-up truncation races with append; record possible loss.
			slog.Warn("core log privacy truncation", "event", "core_log_discard", "reason", reason, "file_bytes", after.Size(), "unread_bytes", after.Size()-t.offset, "append_race_possible", true)
			if err := t.file.Truncate(0); err != nil {
				return t.recordLocked(err)
			}
			t.offset = 0
		}
	}
	return t.err
}

func (t *FileTail) run() {
	defer close(t.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			t.mu.Lock()
			if !t.paused {
				t.readLocked(true)
			}
			t.mu.Unlock()
		}
	}
}

func (t *FileTail) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		_, err := t.checkLocked()
		t.recordLocked(err)
	}
	return t.err
}

func (t *FileTail) Checkpoint() (LogCheckpoint, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return LogCheckpoint{}, os.ErrClosed
	}
	t.paused = true
	if err := t.readLocked(false); err != nil {
		return LogCheckpoint{}, err
	}
	return LogCheckpoint{Path: t.path, Device: t.device, Inode: t.inode, Offset: t.offset}, nil
}

func (t *FileTail) Resume() { t.mu.Lock(); defer t.mu.Unlock(); t.paused = false }

// CompleteAttach ends adoption's no-truncate phase only after the caller has
// verified the live core and persisted its first cumulative sample. Polling
// remains paused until Resume; normal caught-up privacy cleanup then resumes.
func (t *FileTail) CompleteAttach() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || !t.paused || !t.attach {
		return ErrHandoverConflict
	}
	if _, err := t.checkLocked(); err != nil {
		return t.recordLocked(err)
	}
	if t.err != nil {
		return t.err
	}
	t.attach = false
	return nil
}

// Detach releases only this reader. It neither drains nor truncates the file.
func (t *FileTail) Detach() error { return t.finish(false) }

// CloseAfterExit drains and privacy-clears the file after confirmed core exit.
func (t *FileTail) CloseAfterExit() error { return t.finish(true) }

func (t *FileTail) finish(exited bool) error {
	t.once.Do(func() {
		close(t.stop)
		<-t.done
		t.mu.Lock()
		defer t.mu.Unlock()
		if !exited {
			_, err := t.checkLocked()
			t.recordLocked(err)
		}
		if exited {
			t.readLocked(false)
			if info, err := t.checkLocked(); err != nil {
				t.recordLocked(err)
			} else {
				if info.Size() > t.offset {
					slog.Warn("core log discarded after exit", "event", "core_log_discard", "reason", "exit_partial_or_failed", "unread_bytes", info.Size()-t.offset)
				}
				t.recordLocked(t.file.Truncate(0))
			}
		}
		t.recordLocked(t.file.Close())
		t.closed = true
	})
	return t.Err()
}
