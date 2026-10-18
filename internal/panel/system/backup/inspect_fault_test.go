package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func repackFixture(t *testing.T, archivePath string, manifest Manifest, files map[string][]byte, fault string) {
	t.Helper()
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	sealed, err := newEncryptedWriter(context.Background(), file, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(sealed)
	archive := tar.NewWriter(compressed)
	manifest.Files = []File{}
	names := []string{}
	for name := range files {
		if name != "manifest.json" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		digest := sha256.Sum256(files[name])
		manifest.Files = append(manifest.Files, File{Name: name, Bytes: int64(len(files[name])), SHA256: hex.EncodeToString(digest[:])})
	}
	data, _ := json.Marshal(manifest)
	if err := archive.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(data); err != nil {
		t.Fatal(err)
	}
	for index, name := range names {
		header := &tar.Header{Name: name, Mode: 0600, Size: int64(len(files[name]))}
		if fault == "symlink" && index == 0 {
			header.Typeflag = tar.TypeSymlink
			header.Linkname = "../../outside.key"
			header.Size = 0
		}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeSymlink {
			if _, err := archive.Write(files[name]); err != nil {
				t.Fatal(err)
			}
		}
		if fault == "duplicate" && index == 0 {
			if err := archive.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if _, err := archive.Write(files[name]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fault == "extra" {
		if err := archive.WriteHeader(&tar.Header{Name: "rulesets/unlisted.txt", Mode: 0600, Size: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if fault == "trailing-tar" {
		if _, err := compressed.Write([]byte("unlisted-content")); err != nil {
			t.Fatal(err)
		}
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if fault == "trailing-gzip" {
		if _, err := sealed.Write([]byte("second-stream")); err != nil {
			t.Fatal(err)
		}
	}
	if err := sealed.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatedArchiveRejectsInvalidDatabaseKeysLinksExtraFilesAndTrailingStreams(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	created, err := source.Create(context.Background(), directory, "valid_archive_001", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	original := archivedFiles(t, filepath.Join(directory, created.Manifest.ID+".pcb"))
	for _, fault := range []string{"database", "key", "symlink", "duplicate", "extra", "traversal", "trailing-tar", "trailing-gzip"} {
		t.Run(fault, func(t *testing.T) {
			files := map[string][]byte{}
			for name, data := range original {
				files[name] = append([]byte(nil), data...)
			}
			switch fault {
			case "database":
				files["database/panel.db"][0] ^= 1
			case "key":
				files["identity/configuration.key"][0] ^= 1
			case "traversal":
				files["rulesets/../../outside.key"] = []byte("outside")
			}
			path := filepath.Join(directory, "fault-"+fault+".pcb")
			repackFixture(t, path, created.Manifest, files, fault)
			if _, err := Inspect(context.Background(), directory, path, testPassword); err == nil {
				t.Fatal("authenticated but invalid archive accepted", fault)
			}
			assertNoStage(t, directory)
			if _, err := os.Stat(filepath.Join(filepath.Dir(directory), "outside.key")); !os.IsNotExist(err) {
				t.Fatal("archive escaped protected staging", err)
			}
		})
	}
}

func TestBackupSnapshotContainsOneConsistentTransactionalDatabaseVersion(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	ready := make(chan struct{})
	var commits atomic.Int64
	go func() {
		defer close(done)
		for generation := 0; ctx.Err() == nil; generation++ {
			tx, err := source.Database.BeginTx(ctx, nil)
			if err != nil {
				return
			}
			value := strings.Repeat("v", generation%20+1)
			_, first := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('backup_fixture_a',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", value)
			_, second := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES('backup_fixture_b',?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", value)
			if first != nil || second != nil || tx.Commit() != nil {
				tx.Rollback()
				return
			}
			commits.Add(1)
			if generation == 0 {
				close(ready)
			}
		}
	}()
	select {
	case <-ready:
	case <-done:
		t.Fatal("writer stopped before committing a snapshot fixture")
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("writer never committed fixture")
	}
	// A live writer keeps committing transactions while VACUUM creates its own
	// SQLite snapshot. The exported pair must never span two generations.
	created, err := source.Create(context.Background(), directory, "consistent_backup_001", "owner", testPassword)
	cancel()
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if commits.Load() < 2 {
		t.Fatal("no concurrent committed writer activity during backup")
	}
	files := archivedFiles(t, filepath.Join(directory, created.Manifest.ID+".pcb"))
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(snapshotPath, files["database/panel.db"], 0600); err != nil {
		t.Fatal(err)
	}
	database, err := readonlyDatabase(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var left, right string
	leftErr := database.QueryRow("SELECT value_json FROM system_settings WHERE key='backup_fixture_a'").Scan(&left)
	rightErr := database.QueryRow("SELECT value_json FROM system_settings WHERE key='backup_fixture_b'").Scan(&right)
	if leftErr != nil || rightErr != nil || left != right {
		t.Fatal("snapshot crossed committed database versions", leftErr, rightErr)
	}
}
