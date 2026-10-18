package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/vault"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxFiles = 1024
const maxManifestBytes = 256 << 10

type File struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	Files     []File    `json:"files"`
}
type Result struct {
	Manifest Manifest
	Bytes    int64
	SHA256   string
}
type Source struct {
	Database           *sql.DB
	Config             config.Panel
	ConfigurationVault *vault.Vault
}
type fileSource struct {
	File
	source string
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(data)
}

func validID(id string) bool {
	if len(id) < 16 || len(id) > 64 {
		return false
	}
	for _, value := range id {
		if !(value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-') {
			return false
		}
	}
	return true
}

func validName(name string) bool {
	if name == "" || len(name) > 512 || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." || part == "" {
			return false
		}
	}
	switch name {
	case "database/panel.db", "configuration/panel.yaml", "identity/ca.crt", "identity/ca.key", "identity/configuration.key", "identity/node-ca-active.enc", "identity/node-ca-retired.enc", "identity/node-ca-round-active.enc", "identity/node-ca-round-retired.enc", "identity/node-ca-recovered-base.enc", "identity/node-ca-recovered-round.enc", "identity/node-ca-recovered-retired.enc", "identity/server.crt", "identity/server.key", "tls/server.crt", "tls/server.key", "tls/client-ca.crt":
		return true
	}
	return strings.HasPrefix(name, "rulesets/") && len(name) > len("rulesets/")
}

func fileLimit(name string) int64 {
	switch name {
	case "database/panel.db":
		return MaxUnpackedBytes
	case "configuration/panel.yaml":
		return 16384
	case "identity/configuration.key":
		return 32
	case "identity/node-ca-recovered-retired.enc":
		return 32768
	case "identity/node-ca-recovered-base.enc":
		return 1 << 20
	}
	if strings.HasPrefix(name, "rulesets/") {
		return MaxUnpackedBytes
	}
	return 1 << 20
}

func newWork(directory string) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("backup directory must be a regular directory")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(directory, ".work-")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(work, ".panel-backup-stage"), []byte("proxy-control-backup-stage-v1\n"), 0600); err != nil {
		removeWork(directory, work)
		return "", err
	}
	return work, nil
}

// CleanupStages requires the caller to own the backup directory process lock.
// Only marked staging directories made by this package are removed.
func CleanupStages(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".work-") {
			continue
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("invalid backup staging entry")
		}
		work := filepath.Join(directory, entry.Name())
		marker := filepath.Join(work, ".panel-backup-stage")
		info, err := os.Lstat(marker)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 128 {
			return errors.New("unrecognized backup staging directory")
		}
		data, err := os.ReadFile(marker)
		if err != nil || string(data) != "proxy-control-backup-stage-v1\n" {
			return errors.New("unrecognized backup staging directory")
		}
		if err := removeWork(directory, work); err != nil {
			return err
		}
	}
	return nil
}

func removeWork(directory, work string) error {
	root, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(work)
	if err != nil {
		return err
	}
	if filepath.Dir(target) != root || !strings.HasPrefix(filepath.Base(target), ".work-") {
		return errors.New("invalid backup staging directory")
	}
	return os.RemoveAll(target)
}

func digestFile(ctx context.Context, filePath string, limit int64) (File, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return File{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit {
		return File{}, errors.New("invalid backup source size or file type")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(contextReader{ctx, file}, limit+1))
	if err != nil {
		return File{}, err
	}
	if n != info.Size() || n > limit {
		return File{}, errors.New("backup source changed during inspection")
	}
	return File{Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s Source) snapshot(ctx context.Context, work string) ([]fileSource, error) {
	if s.Database == nil {
		return nil, errors.New("backup source database unavailable")
	}
	if err := s.Database.PingContext(ctx); err != nil {
		return nil, errors.New("backup source database unavailable")
	}
	if err := s.checkConfigurationKey(); err != nil {
		return nil, err
	}
	databasePath := filepath.Join(work, "panel.db")
	// A separate read-only SQLite connection preserves live WAL visibility
	// without holding the panel's single database/sql connection during copying.
	absolute, err := filepath.Abs(s.Config.Database)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	uri.RawQuery = url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)"}}.Encode()
	snapshotConnection, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	snapshotConnection.SetMaxOpenConns(1)
	defer snapshotConnection.Close()
	if _, err := snapshotConnection.ExecContext(ctx, "VACUUM INTO ?", databasePath); err != nil {
		return nil, errors.Join(errors.New("consistent database snapshot failed"), ctx.Err())
	}
	if err := os.Chmod(databasePath, 0600); err != nil {
		return nil, err
	}
	configuration, err := yaml.Marshal(s.Config)
	if err != nil {
		return nil, err
	}
	configurationPath := filepath.Join(work, "panel.yaml")
	if err := os.WriteFile(configurationPath, configuration, 0600); err != nil {
		return nil, err
	}
	sources := []fileSource{}
	add := func(name, source string, optional bool, limit int64) error {
		if source == "" {
			if optional {
				return nil
			}
			return errors.New("required backup source missing")
		}
		file, err := digestFile(ctx, source, limit)
		if optional && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		file.Name = name
		sources = append(sources, fileSource{File: file, source: source})
		return nil
	}
	if err := add("database/panel.db", databasePath, false, MaxUnpackedBytes); err != nil {
		return nil, err
	}
	if err := add("configuration/panel.yaml", configurationPath, false, 16384); err != nil {
		return nil, err
	}
	for _, name := range []string{"ca.crt", "ca.key", "configuration.key", "node-ca-active.enc", "node-ca-retired.enc", "node-ca-round-active.enc", "node-ca-round-retired.enc", "node-ca-recovered-base.enc", "node-ca-recovered-round.enc", "node-ca-recovered-retired.enc", "server.crt", "server.key"} {
		if err := add("identity/"+name, filepath.Join(s.Config.NodeIdentityDirectory, name), strings.HasPrefix(name, "server.") || name == "node-ca-active.enc" || name == "node-ca-retired.enc" || name == "node-ca-round-active.enc" || name == "node-ca-round-retired.enc" || name == "node-ca-recovered-base.enc" || name == "node-ca-recovered-round.enc" || name == "node-ca-recovered-retired.enc", fileLimit("identity/"+name)); err != nil {
			return nil, err
		}
	}
	if !s.Config.Development {
		for name, source := range map[string]string{"tls/server.crt": s.Config.TLS.Certificate, "tls/server.key": s.Config.TLS.Key, "tls/client-ca.crt": s.Config.TLS.ClientCA} {
			if err := add(name, source, false, 1<<20); err != nil {
				return nil, err
			}
		}
	}
	rulesDirectory := filepath.Join(filepath.Dir(s.Config.Database), "rulesets")
	info, err := os.Lstat(rulesDirectory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("invalid ruleset directory")
		}
		err = filepath.WalkDir(rulesDirectory, func(source string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("ruleset symlinks cannot be archived")
			}
			if entry.IsDir() {
				return nil
			}
			if len(sources) >= maxFiles {
				return errors.New("backup file count exceeded")
			}
			relative, err := filepath.Rel(rulesDirectory, source)
			if err != nil {
				return err
			}
			name := "rulesets/" + filepath.ToSlash(relative)
			if !validName(name) {
				return errors.New("invalid ruleset archive name")
			}
			return add(name, source, false, MaxUnpackedBytes)
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	var total int64
	for _, source := range sources {
		total += source.Bytes
		if total > MaxUnpackedBytes {
			return nil, errors.New("backup contents exceed size limit")
		}
	}
	if err := s.checkConfigurationKey(); err != nil {
		return nil, err
	}
	return sources, nil
}

func (s Source) CheckConfigurationKey() error { return s.checkConfigurationKey() }

func (s Source) checkConfigurationKey() error {
	if s.ConfigurationVault == nil {
		return errors.New("active configuration key unavailable")
	}
	keyPath := filepath.Join(s.Config.NodeIdentityDirectory, "configuration.key")
	info, err := os.Stat(keyPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != 32 {
		return errors.New("original configuration key unavailable")
	}
	keyFile, err := os.Open(keyPath)
	if err != nil {
		return errors.New("original configuration key unavailable")
	}
	defer keyFile.Close()
	key, err := io.ReadAll(io.LimitReader(keyFile, 33))
	if err != nil || len(key) != 32 {
		return errors.New("original configuration key unavailable")
	}
	fileVault, err := vault.FromKey(key)
	clear(key)
	if err != nil {
		return err
	}
	probe := []byte("proxy-control-backup-key-check")
	sealed, err := fileVault.Seal(probe)
	if err != nil {
		return err
	}
	opened, err := s.ConfigurationVault.Open(sealed)
	if err != nil || string(opened) != string(probe) {
		return errors.New("configuration key differs from active key")
	}
	return nil
}

// Create snapshots SQLite before archiving and revalidates the completed
// portable file before returning it. Only the caller publishes its catalog row.
func (s Source) Create(ctx context.Context, directory, id, actor, password string) (result Result, resultErr error) {
	if !validID(id) || actor == "" || len(actor) > 128 {
		return result, ErrArchive
	}
	if err := ValidatePassword(password); err != nil {
		return result, err
	}
	work, err := newWork(directory)
	if err != nil {
		return result, err
	}
	archivePath := filepath.Join(directory, id+".pcb")
	published := false
	defer func() {
		resultErr = errors.Join(resultErr, removeWork(directory, work))
		if resultErr != nil {
			resultErr = errors.Join(resultErr, ctx.Err())
		}
		if published && resultErr != nil {
			resultErr = errors.Join(resultErr, os.Remove(archivePath))
		}
	}()
	sources, err := s.snapshot(ctx, work)
	if err != nil {
		return result, err
	}
	result.Manifest = Manifest{Version: 1, ID: id, CreatedAt: time.Now().UTC(), CreatedBy: actor, Files: []File{}}
	for _, source := range sources {
		result.Manifest.Files = append(result.Manifest.Files, source.File)
	}
	manifest, err := json.Marshal(result.Manifest)
	if err != nil || len(manifest) > maxManifestBytes {
		return result, ErrArchive
	}
	partialPath := filepath.Join(work, "archive.pcb")
	file, err := os.OpenFile(partialPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	defer file.Close()
	sealed, err := newEncryptedWriter(ctx, file, password)
	if err != nil {
		return result, err
	}
	compressed := gzip.NewWriter(sealed)
	archive := tar.NewWriter(compressed)
	if err := archive.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(manifest))}); err != nil {
		return result, err
	}
	if _, err := archive.Write(manifest); err != nil {
		return result, err
	}
	for _, source := range sources {
		if err := archive.WriteHeader(&tar.Header{Name: source.Name, Mode: 0600, Size: source.Bytes}); err != nil {
			return result, err
		}
		input, err := os.Open(source.source)
		if err != nil {
			return result, err
		}
		hash := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(contextReader{ctx, input}, source.Bytes+1))
		closeErr := input.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return result, err
		}
		if n != source.Bytes || hex.EncodeToString(hash.Sum(nil)) != source.SHA256 {
			return result, errors.New("backup source changed during archive creation")
		}
	}
	for _, close := range []func() error{archive.Close, compressed.Close, sealed.Close, file.Sync, file.Close} {
		if err := close(); err != nil {
			return result, err
		}
	}
	inspection, err := Inspect(ctx, directory, partialPath, password)
	if err != nil || inspection.Manifest.ID != id {
		return result, errors.Join(err, ErrArchive)
	}
	// Exclusive publication prevents exposing a partial archive or replacing
	// an existing backup with the same identifier.
	if err := os.Link(partialPath, archivePath); err != nil {
		return result, err
	}
	published = true
	return inspection, nil
}
