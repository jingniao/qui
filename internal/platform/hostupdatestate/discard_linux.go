package hostupdatestate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func childName(registry, path, prefix string) (string, error) {
	if !filepath.IsAbs(registry) || !filepath.IsAbs(path) || filepath.Dir(path) != filepath.Clean(registry) || filepath.Clean(path) != path {
		return "", fmt.Errorf("not a direct registry child: %s", path)
	}
	name := filepath.Base(path)
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return "", fmt.Errorf("invalid snapshot name: %s", path)
	}
	return name, nil
}

func DiscardSnapshot(registry, snapshot string) error {
	return discardSnapshot(registry, snapshot, fileOperations{})
}
func discardSnapshot(registry, snapshot string, ops fileOperations) (result error) {
	name, err := childName(registry, snapshot, "rollback.")
	if err != nil {
		return err
	}
	dir, err := privateDirectory(registry)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, dir.Close()) }()
	dest := "discard." + strings.TrimPrefix(name, "rollback.")
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return removeDiscardedAt(dir, dest, ops)
	}
	if err != nil {
		return fmt.Errorf("snapshot retained at %s: %w", snapshot, err)
	}
	source := os.NewFile(uintptr(fd), snapshot)
	defer func() { result = errors.Join(result, source.Close()) }()
	var original, current unix.Stat_t
	if err = unix.Fstat(fd, &original); err != nil {
		return err
	}
	if original.Uid != 0 || original.Gid != 0 || original.Mode&07777 != 0700 {
		return fmt.Errorf("snapshot must be root:root 0700: %s", snapshot)
	}
	if ops.pinned != nil {
		if err = ops.pinned(); err != nil {
			return err
		}
	}
	if err = unix.Fstatat(int(dir.Fd()), name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if original.Dev != current.Dev || original.Ino != current.Ino {
		return fmt.Errorf("snapshot changed before discard: %s", snapshot)
	}
	rename := ops.rename
	if rename == nil {
		rename = func(a int, b string, c int, d string) error { return unix.Renameat2(a, b, c, d, unix.RENAME_NOREPLACE) }
	}
	if err = rename(int(dir.Fd()), name, int(dir.Fd()), dest); err != nil {
		return fmt.Errorf("snapshot retained at %s: %w", snapshot, err)
	}
	syncDir := ops.sync
	if syncDir == nil {
		syncDir = func(f *os.File) error { return f.Sync() }
	}
	if err = syncDir(dir); err != nil {
		return fmt.Errorf("discard retained at %s: %w", filepath.Join(registry, dest), err)
	}
	return removeDiscardedAt(dir, dest, ops)
}

func RemoveDiscarded(registry, discarded string) (result error) {
	name, err := childName(registry, discarded, "discard.")
	if err != nil {
		return err
	}
	dir, err := privateDirectory(registry)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, dir.Close()) }()
	return removeDiscardedAt(dir, name, fileOperations{})
}

func removeDiscardedAt(dir *os.File, name string, ops fileOperations) (result error) {
	var st unix.Stat_t
	err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0700 {
		return fmt.Errorf("unrecognized discarded directory retained: %s/%s", dir.Name(), name)
	}
	// OpenRoot pins the registry inode and confines recursive deletion beneath it.
	root, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", dir.Fd()))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, root.Close()) }()
	remove := ops.remove
	if remove == nil {
		remove = func(r *os.Root, name string) error { return r.RemoveAll(name) }
	}
	if err = remove(root, name); err != nil {
		return fmt.Errorf("discard retained at %s/%s: %w", dir.Name(), name, err)
	}
	syncDir := ops.sync
	if syncDir == nil {
		syncDir = func(f *os.File) error { return f.Sync() }
	}
	if err = syncDir(dir); err != nil {
		return fmt.Errorf("discard removed; registry sync failed: %w", err)
	}
	return nil
}
