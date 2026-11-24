package hostupdatedb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func accountIDs(account string) (int, int, error) {
	u, err := user.Lookup(account)
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(u.Gid)
	return uid, gid, err
}

type sourceReader struct {
	io.ReadCloser
	context    context.Context
	command    *exec.Cmd
	diagnostic bytes.Buffer
}

func (r *sourceReader) Read(buffer []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.ReadCloser.Read(buffer)
}

func openSource(ctx context.Context, account, path string, staged bool) (*sourceReader, error) {
	r := &sourceReader{context: ctx}
	if staged {
		f, err := os.Open(path)
		r.ReadCloser = f
		return r, err
	}
	r.command = exec.CommandContext(ctx, "runuser", "-u", account, "--", "cat", "--", path)
	r.command.Stderr = &r.diagnostic
	pipe, err := r.command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	r.ReadCloser = pipe
	if err := r.command.Start(); err != nil {
		return nil, errors.Join(err, pipe.Close())
	}
	return r, nil
}
func (r *sourceReader) finish(different bool) error {
	closeErr := r.Close()
	if r.command == nil {
		return closeErr
	}
	err := r.command.Wait()
	var exit *exec.ExitError
	if different && errors.As(err, &exit) {
		status := exit.Sys().(syscall.WaitStatus)
		if status.Signal() == syscall.SIGPIPE || exit.ExitCode() == 141 {
			err = nil
		}
	}
	if err != nil {
		err = fmt.Errorf("read database source: %w: %s", err, r.diagnostic.String())
	}
	return errors.Join(closeErr, err)
}

func equalReaders(ctx context.Context, a, b io.Reader) (bool, error) {
	left, right := make([]byte, 64*1024), make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, ae := io.ReadFull(a, left)
		m, be := io.ReadFull(b, right)
		for _, err := range []error{ae, be} {
			if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return false, err
			}
		}
		if n != m || !bytes.Equal(left[:n], right[:m]) {
			return false, nil
		}
		if ae != nil || be != nil {
			return true, nil
		}
	}
}
func compareSource(ctx context.Context, account, source, backup string, staged bool) (equal bool, result error) {
	input, err := openSource(ctx, account, source, staged)
	if err != nil {
		return false, err
	}
	defer func() { result = errors.Join(result, input.finish(!equal && result == nil)) }()
	other, err := os.Open(backup)
	if err != nil {
		return false, err
	}
	defer func() { result = errors.Join(result, other.Close()) }()
	return equalReaders(ctx, input, other)
}
func compareDatabase(ctx context.Context, account, source, backup string) (bool, error) {
	return compareSource(ctx, account, source, backup, false)
}

// These operations stay injectable inside the package so fsync errors and the
// directory replacement race can be verified without production fault switches.
type copyOperations struct {
	sync   func(*os.File) error
	pinned func() error
}

func copyDatabase(ctx context.Context, o CopyOptions) error {
	return copyWithOperations(ctx, o, copyOperations{sync: func(f *os.File) error { return f.Sync() }})
}
func copyWithOperations(ctx context.Context, o CopyOptions, ops copyOperations) (result error) {
	uid, gid, err := accountIDs(o.Account)
	if err != nil {
		return err
	}
	registry, err := os.Open(o.Registry)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, registry.Close()) }()
	var reg unix.Stat_t
	if err := unix.Fstat(int(registry.Fd()), &reg); err != nil {
		return err
	}
	if reg.Mode&unix.S_IFMT != unix.S_IFDIR || reg.Uid != 0 || reg.Mode&0777 != 0700 {
		return fmt.Errorf("registry must be a root-owned 0700 directory: %s", o.Registry)
	}
	destination, err := os.Open(filepath.Dir(o.Destination))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, destination.Close()) }()
	var dest unix.Stat_t
	if err := unix.Fstat(int(destination.Fd()), &dest); err != nil {
		return err
	}
	if dest.Mode&unix.S_IFMT != unix.S_IFDIR || int(dest.Uid) != uid {
		return fmt.Errorf("database target directory does not belong to %s", o.Account)
	}
	if dest.Dev != reg.Dev {
		return errors.New("database target and registry must share a filesystem")
	}
	if ops.pinned != nil {
		if err := ops.pinned(); err != nil {
			return err
		}
	}
	var partials []string
	defer func() {
		for _, p := range partials {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				result = errors.Join(result, err)
			}
		}
	}()
	var prepared []string
	suffixes := []string{""}
	wal, err := os.Lstat(o.Source + "-wal")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && wal.Mode().IsRegular() {
		suffixes = append(suffixes, "-wal")
	}
	for _, suffix := range suffixes {
		file, err := os.CreateTemp(o.Registry, "database.*.partial")
		if err != nil {
			return err
		}
		partials = append(partials, file.Name())
		input, err := openSource(ctx, o.Account, o.Source+suffix, o.OwnerStaged)
		if err != nil {
			return errors.Join(err, file.Close())
		}
		_, copyErr := io.Copy(file, input)
		err = errors.Join(copyErr, input.finish(false))
		if err == nil {
			var equal bool
			equal, err = compareSource(ctx, o.Account, o.Source+suffix, file.Name(), o.OwnerStaged)
			if err == nil && !equal {
				err = fmt.Errorf("database source changed during copy: %s", o.Source+suffix)
			}
		}
		if err == nil {
			err = file.Chown(uid, gid)
		}
		if err == nil {
			err = file.Chmod(0600)
		}
		if err == nil {
			err = ops.sync(file)
		}
		if err = errors.Join(err, file.Close()); err != nil {
			return fmt.Errorf("copy %s: %w", o.Source+suffix, err)
		}
		prepared = append(prepared, file.Name())
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name := filepath.Base(o.Destination)
	if o.Restore {
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := unix.Unlinkat(int(destination.Fd()), name+suffix, 0); err != nil && !errors.Is(err, unix.ENOENT) {
				return err
			}
		}
	}
	// Both input files have been verified before any final name is published.
	for i := len(prepared) - 1; i >= 0; i-- {
		if err := unix.Renameat(int(registry.Fd()), filepath.Base(prepared[i]), int(destination.Fd()), name+suffixes[i]); err != nil {
			return err
		}
	}
	return ops.sync(destination)
}

func preflight(ctx context.Context, o PreflightOptions) (result CheckResult, outcome error) {
	return preflightWithCleanup(ctx, o, os.RemoveAll)
}

func preflightWithCleanup(ctx context.Context, o PreflightOptions, cleanup func(string) error) (result CheckResult, outcome error) {
	info, err := os.Lstat(o.CheckDirectory)
	if err != nil {
		return result, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || stat.Uid != 0 {
		return result, errors.New("check directory must be root-owned 0700")
	}
	entries, err := os.ReadDir(o.CheckDirectory)
	if err != nil {
		return result, err
	}
	if len(entries) != 0 {
		return result, errors.New("check directory must be empty")
	}
	defer func() { outcome = errors.Join(outcome, cleanup(o.CheckDirectory)) }()
	for _, suffix := range []string{"", "-wal"} {
		target := filepath.Join(o.CheckDirectory, "panel.db"+suffix)
		err := os.Link(o.Backup+suffix, target)
		if suffix != "" && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, err
		}
		info, err := os.Lstat(target)
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() {
			return result, fmt.Errorf("backup input is not a regular file: %s", o.Backup+suffix)
		}
	}
	command := exec.CommandContext(ctx, o.Checker, "--check-database", filepath.Join(o.CheckDirectory, "panel.db"))
	result.Output, err = command.CombinedOutput()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		result.ExitCode = exit.ExitCode()
		if result.ExitCode < 0 {
			result.ExitCode = 128 + int(exit.Sys().(syscall.WaitStatus).Signal())
		}
	case errors.Is(err, os.ErrNotExist):
		result.ExitCode = 127
		result.Output = []byte(err.Error())
	case errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOEXEC):
		result.ExitCode = 126
		result.Output = []byte(err.Error())
	default:
		return result, err
	}
	return result, nil
}
