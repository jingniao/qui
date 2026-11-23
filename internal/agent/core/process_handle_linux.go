//go:build linux

package core

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// OpenProcessHandle never Waits on an external process. Both sides of pidfd_open
// are checked, including the target PID namespace, before granting control.
func OpenProcessHandle(run CoreRun) (ProcessHandle, error) {
	if err := validateCoreRun(&run); err != nil {
		return nil, errors.Join(ErrHandoverInvalid, err)
	}
	check := func() error {
		current := ObserveOSEnvironment()
		if ResolveCoreRun(&run, current) == ProcessExited {
			return ErrCoreExitedDuringHandover
		}
		if run.Version != CoreRunMarkerVersion || !completeEnvironment(run.Environment) || run.Environment != current {
			return ErrHandoverConflict
		}
		if run.Process.PID <= 0 {
			return ErrHandoverInvalid
		}
		token, state, err := observeLinuxProcess(run.Process.PID)
		if err != nil {
			if errors.Is(err, errProcessGone) {
				return errors.Join(ErrCoreExitedDuringHandover, err)
			}
			return err
		}
		if token != run.Process.Token || state == "Z" || state == "X" {
			return errors.Join(ErrCoreExitedDuringHandover, errProcessGone)
		}
		ns, err := os.Readlink("/proc/" + strconv.Itoa(run.Process.PID) + "/ns/pid")
		if err != nil {
			return err
		}
		if ns != run.Environment.NamespaceID {
			return ErrHandoverConflict
		}
		return nil
	}
	if err := check(); err != nil {
		return nil, err
	}
	fd, err := unix.PidfdOpen(run.Process.PID, 0)
	if err != nil {
		return nil, pidfdError(err)
	}
	if err := check(); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	// Signal 0 probes pidfd_send_signal support and permission without signaling.
	if err := unix.PidfdSendSignal(fd, 0, nil, 0); err != nil {
		return nil, errors.Join(pidfdError(err), unix.Close(fd))
	}
	h := &processHandle{identity: run.Process, environment: run.Environment, done: make(chan struct{}), observed: make(chan struct{})}
	h.stop = func() error {
		err := unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
		if errors.Is(err, unix.ESRCH) {
			return nil
		}
		if err != nil {
			return pidfdError(err)
		}
		return nil
	}
	// Poll and close share h.mu: a reused file descriptor can never be polled or
	// signaled by this observer. Poll's timeout only bounds release latency.
	h.release = func() error { return unix.Close(fd) }
	go func() {
		for {
			h.mu.Lock()
			if h.detached {
				h.mu.Unlock()
				return
			}
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			_, err := unix.Poll(fds, 100)
			if errors.Is(err, unix.EINTR) {
				h.mu.Unlock()
				continue
			}
			if err != nil {
				h.exit.Err = err
				close(h.observed)
				h.mu.Unlock()
				return
			}
			if fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
				h.exit.Err = fmt.Errorf("pidfd poll events: %d", fds[0].Revents)
				close(h.observed)
				h.mu.Unlock()
				return
			}
			if fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0 {
				h.exit = ExitObservation{Exited: true}
				close(h.done)
				close(h.observed)
				h.mu.Unlock()
				return
			}
			h.mu.Unlock()
		}
	}()
	return h, nil
}

func pidfdError(err error) error {
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENODEV) {
		return errors.Join(ErrHandoverUnsupported, err)
	}
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
		return errors.Join(ErrHandoverPermission, err)
	}
	return err
}
