package hostupdatedb

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/iotest"
	"time"
)

func TestCopyAndCompare(t *testing.T) {
	dir := t.TempDir()
	registry := filepath.Join(dir, "registry")
	destination := filepath.Join(dir, "backups")
	for _, p := range []string{registry, destination} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte(strings.Repeat("database", 16384)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source+"-wal", []byte("committed WAL"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "copy.db")
	if err := Copy(context.Background(), CopyOptions{Registry: registry, Account: "root", Source: source, Destination: target}); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal"} {
		a, _ := os.ReadFile(source + suffix)
		b, err := os.ReadFile(target + suffix)
		if err != nil || string(a) != string(b) {
			t.Fatalf("copy %q: %v", suffix, err)
		}
		x, _ := os.Stat(source + suffix)
		y, _ := os.Stat(target + suffix)
		if os.SameFile(x, y) || y.Mode().Perm() != 0600 {
			t.Fatal("backup must be independent and private")
		}
	}
	equal, err := Compare(context.Background(), "root", source, target)
	if err != nil || !equal {
		t.Fatalf("equal=%v err=%v", equal, err)
	}
	if err := os.WriteFile(target, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	equal, err = Compare(context.Background(), "root", source, target)
	if err != nil || equal {
		t.Fatalf("different: equal=%v err=%v", equal, err)
	}
	_, err = Compare(context.Background(), "root", source+".missing", target)
	if err == nil {
		t.Fatal("missing source must fail")
	}
	files, _ := filepath.Glob(filepath.Join(registry, "*.partial"))
	if len(files) != 0 {
		t.Fatal(files)
	}
}

func TestCopyPinnedDestinationAndSync(t *testing.T) {
	dir := t.TempDir()
	registry, target := filepath.Join(dir, "registry"), filepath.Join(dir, "target")
	for _, p := range []string{registry, target} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("database"), 0600); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(dir, "private")
	if err := os.Mkdir(private, 0700); err != nil {
		t.Fatal(err)
	}
	var synced []string
	err := copyWithOperations(context.Background(), CopyOptions{Registry: registry, Account: "root", Source: source, Destination: filepath.Join(target, "panel.db")}, copyOperations{
		pinned: func() error {
			if err := os.Rename(target, target+".saved"); err != nil {
				return err
			}
			return os.Symlink(private, target)
		},
		sync: func(f *os.File) error { synced = append(synced, f.Name()); return f.Sync() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(target+".saved", "panel.db")); err != nil || string(data) != "database" {
		t.Fatalf("pinned copy: %q %v", data, err)
	}
	if entries, err := os.ReadDir(private); err != nil || len(entries) != 0 {
		t.Fatalf("private target changed: %v %v", entries, err)
	}
	if len(synced) != 2 || !strings.HasSuffix(synced[0], ".partial") || synced[1] != target {
		t.Fatalf("sync order: %v", synced)
	}
}

func TestCopySyncFailuresAndRestore(t *testing.T) {
	for _, failDirectory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[failDirectory], func(t *testing.T) {
			dir := t.TempDir()
			registry := filepath.Join(dir, "registry")
			target := filepath.Join(dir, "target")
			for _, p := range []string{registry, target} {
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			source := filepath.Join(dir, "source")
			destination := filepath.Join(target, "panel.db")
			if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("injected fsync failure")
			err := copyWithOperations(context.Background(), CopyOptions{Registry: registry, Account: "root", Source: source, Destination: destination}, copyOperations{sync: func(f *os.File) error {
				info, err := f.Stat()
				if err != nil {
					return err
				}
				if info.IsDir() == failDirectory {
					return injected
				}
				return f.Sync()
			}})
			if !errors.Is(err, injected) {
				t.Fatalf("sync error lost: %v", err)
			}
			_, err = os.Stat(destination)
			if !failDirectory && !os.IsNotExist(err) {
				t.Fatalf("published before sync: %v", err)
			}
			for _, suffix := range []string{"-wal", "-shm"} {
				if err := os.WriteFile(destination+suffix, []byte("stale"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := Copy(context.Background(), CopyOptions{Registry: registry, Account: "root", Source: source, Destination: destination, Restore: true}); err != nil {
				t.Fatal(err)
			}
			for _, suffix := range []string{"-wal", "-shm"} {
				if _, err := os.Stat(destination + suffix); !os.IsNotExist(err) {
					t.Fatalf("stale sidecar: %s %v", suffix, err)
				}
			}
			entries, err := os.ReadDir(registry)
			if err != nil || len(entries) != 0 {
				t.Fatalf("partials: %v %v", entries, err)
			}
		})
	}
}

func TestPreflightCancellation(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup")
	check := filepath.Join(dir, "check")
	checker := filepath.Join(dir, "checker")
	if err := os.WriteFile(backup, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(check, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checker, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := Preflight(ctx, PreflightOptions{CheckDirectory: check, Backup: backup, Checker: checker})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel lost: %v", err)
	}
	if _, err := os.Stat(check); !os.IsNotExist(err) {
		t.Fatalf("check not cleaned: %v", err)
	}
}

func TestCopyUnreadableWAL(t *testing.T) {
	t.Setenv("LC_ALL", "C")
	dir, err := os.MkdirTemp("/tmp", "qui-db-permission-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(dir, "registry")
	targetDir := filepath.Join(dir, "backups")
	for _, p := range []string{registry, targetDir} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	uid, gid, err := accountIDs("daemon")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(targetDir, uid, gid); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source.db")
	if err := os.WriteFile(source, []byte("db"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source+"-wal", []byte("root private"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "copy.db")
	err = Copy(context.Background(), CopyOptions{Registry: registry, Account: "daemon", Source: source, Destination: target})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
		t.Fatalf("permission error missing: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("published incomplete copy: %v", err)
	}
	if err := Copy(context.Background(), CopyOptions{Registry: registry, Account: "daemon", Source: source, Destination: target, OwnerStaged: true}); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal"} {
		info, err := os.Stat(target + suffix)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if int(stat.Uid) != uid || int(stat.Gid) != gid || info.Mode().Perm() != 0600 {
			t.Fatalf("staged copy ownership: %+v", info)
		}
	}
}

func TestCompareLateErrorAndCancellation(t *testing.T) {
	failure := errors.New("late source read error")
	prefix := strings.Repeat("a", 64*1024)
	equal, err := equalReaders(context.Background(), io.MultiReader(strings.NewReader(prefix), iotest.ErrReader(failure)), strings.NewReader(prefix+"tail"))
	if equal || !errors.Is(err, failure) {
		t.Fatalf("read failure mistaken for difference: %v %v", equal, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	equal, err = equalReaders(ctx, strings.NewReader(prefix), strings.NewReader(prefix))
	if equal || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation hidden: %v %v", equal, err)
	}
}

func TestSourceCancellationStopsReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &sourceReader{ReadCloser: io.NopCloser(strings.NewReader("private staged input")), context: ctx}
	if n, err := reader.Read(make([]byte, 64)); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("read after cancellation: %d %v", n, err)
	}
}

func TestPreflightCleanupFailurePreservesReport(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup")
	checker := filepath.Join(dir, "checker")
	check := filepath.Join(dir, "check")
	if err := os.WriteFile(backup, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checker, []byte("#!/bin/sh\nprintf 'database damaged\\n'\nexit 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(check, 0700); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("cannot remove check directory")
	result, err := preflightWithCleanup(context.Background(), PreflightOptions{CheckDirectory: check, Backup: backup, Checker: checker}, func(string) error { return failure })
	if !errors.Is(err, failure) || result.ExitCode != 5 || string(result.Output) != "database damaged\n" {
		t.Fatalf("report or cleanup failure lost: %+v %v", result, err)
	}
}

func TestPreflightReportAndCleanup(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup.db")
	check := filepath.Join(dir, "check.test")
	if err := os.WriteFile(backup, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(check, 0700); err != nil {
		t.Fatal(err)
	}
	checker := filepath.Join(dir, "checker")
	if err := os.WriteFile(checker, []byte("#!/bin/sh\ntest \"$1\" = --check-database || exit 90\ntest -f \"$2\" || exit 91\nprintf 'database: newer\\n'\nexit 3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err := Preflight(context.Background(), PreflightOptions{CheckDirectory: check, Backup: backup, Checker: checker})
	if err != nil || result.ExitCode != 3 || string(result.Output) != "database: newer\n" {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(check); !os.IsNotExist(err) {
		t.Fatalf("check directory remains: %v", err)
	}
	bytes, _ := os.ReadFile(backup)
	if string(bytes) != "db" {
		t.Fatal("backup changed")
	}
}
