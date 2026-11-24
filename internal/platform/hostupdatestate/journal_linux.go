package hostupdatestate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// The seams are package-private: no production failure switches.
type fileOperations struct {
	sync   func(*os.File) error
	pinned func() error
	rename func(int, string, int, string) error
	remove func(*os.Root, string) error
}

func privateDirectory(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	if err == nil && (st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0700) {
		err = fmt.Errorf("must be a root:root 0700 directory: %s", path)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

func ReadJournal(file, backups string) (j Journal, result error) {
	dir, err := privateDirectory(filepath.Dir(file))
	if err != nil {
		return j, err
	}
	defer func() { result = errors.Join(result, dir.Close()) }()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(file), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return j, err
	}
	f := os.NewFile(uintptr(fd), file)
	defer func() { result = errors.Join(result, f.Close()) }()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return j, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0600 {
		return j, fmt.Errorf("journal must be a root:root 0600 regular file: %s", file)
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return j, err
	}
	j, err = parseJournal(b)
	if err != nil {
		return j, err
	}
	if j.Backup != "" {
		if !strings.HasPrefix(j.Backup, strings.TrimRight(backups, "/")+"/") {
			return j, errors.New("backup is outside backup directory")
		}
		info, err := os.Lstat(j.Backup)
		if err != nil {
			return j, err
		}
		if !info.Mode().IsRegular() {
			return j, errors.New("backup must be a regular non-symlink file")
		}
	}
	return j, nil
}

func WriteJournal(snapshot string, j Journal) error {
	return writeJournal(snapshot, j, fileOperations{})
}
func writeJournal(snapshot string, j Journal, ops fileOperations) (result error) {
	b, err := journalBytes(j)
	if err != nil {
		return err
	}
	dir, err := privateDirectory(snapshot)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, dir.Close()) }()
	if ops.pinned != nil {
		if err = ops.pinned(); err != nil {
			return err
		}
	}
	// A killed writer can leave this single known temporary name. Unlink it
	// relative to the pinned directory, without opening or following it.
	if err = unix.Unlinkat(int(dir.Fd()), ".journal.tmp", 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	fd, err := unix.Openat(int(dir.Fd()), ".journal.tmp", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	pending := true
	defer func() {
		if pending {
			result = errors.Join(result, unix.Unlinkat(int(dir.Fd()), ".journal.tmp", 0))
		}
	}()
	f := os.NewFile(uintptr(fd), ".journal.tmp")
	syncFile := ops.sync
	if syncFile == nil {
		syncFile = func(f *os.File) error { return f.Sync() }
	}
	err = f.Chmod(0600)
	if err == nil {
		err = f.Chown(0, 0)
	}
	if err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = syncFile(f)
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	rename := ops.rename
	if rename == nil {
		rename = unix.Renameat
	}
	if err = rename(int(dir.Fd()), ".journal.tmp", int(dir.Fd()), "journal"); err != nil {
		return err
	}
	pending = false
	if err = syncFile(dir); err != nil {
		return fmt.Errorf("journal renamed; snapshot directory sync failed: %w", err)
	}
	return nil
}
