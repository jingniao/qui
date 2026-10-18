package recovery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	archive "proxycontrol/internal/panel/system/backup"
	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/id"
	"proxycontrol/internal/platform/panelstore"
	"strconv"
	"strings"
)

const journalName = ".panel-restore.json"
const transactionMarker = "proxy-control-restore-transaction-v1\n"

var ErrFileRecovery = errors.New("restore file transaction requires intact authenticated recovery materials")

type fileEntry struct {
	Destination string        `json:"destination"`
	Old         *archive.File `json:"old,omitempty"`
	New         *archive.File `json:"new,omitempty"`
}
type filePlan struct {
	Version           int         `json:"version"`
	ID                string      `json:"id"`
	Database          string      `json:"database"`
	Identity          string      `json:"identity"`
	Entries           []fileEntry `json:"entries"`
	ReadOnlyPreflight bool        `json:"readOnlyPreflight,omitempty"`
}
type authenticatedPlan struct {
	Plan json.RawMessage `json:"plan"`
	MAC  string          `json:"mac"`
}

// InstallFiles replaces a fully prepared file set while retaining originals.
// Callers must close every database/service handle and validate evidence
// freshness first. Validation must not run proxy control or accept writes.
// A failure or process exit before commitment restores the original file set.
func installFiles(ctx context.Context, owner *panelstore.Ownership, files []archive.RestoreFile, sources map[string]string, validate func() error, checkpoint func(string)) (resultErr error) {
	return installFilesWithCleanup(ctx, owner, files, sources, validate, checkpoint, nil)
}

func installFilesWithCleanup(ctx context.Context, owner *panelstore.Ownership, files []archive.RestoreFile, sources map[string]string, validate func() error, checkpoint func(string), beforeRollback func() error) (resultErr error) {
	if err := owner.AssertHeld(owner.Database, owner.IdentityDirectory); err != nil {
		return err
	}
	if err := RecoverFiles(ctx, owner); err != nil {
		return err
	}
	key, err := journalKey(owner)
	if err != nil {
		return err
	}
	defer clear(key)
	transactionID, err := id.New()
	if err != nil {
		return err
	}
	plan := filePlan{Version: 1, ID: transactionID, Database: owner.Database, Identity: owner.IdentityDirectory}
	plan.ReadOnlyPreflight = beforeRollback != nil
	root := filepath.Dir(owner.Database)
	work := filepath.Join(root, ".restore-txn-"+transactionID)
	if err := os.Mkdir(work, 0700); err != nil {
		return err
	}
	published := false
	defer func() {
		if resultErr != nil && published {
			if beforeRollback != nil {
				resultErr = errors.Join(resultErr, beforeRollback())
			}
			cleanup, cancel := context.WithCancel(context.Background())
			defer cancel()
			resultErr = errors.Join(resultErr, RecoverFiles(cleanup, owner))
		} else if !published {
			resultErr = errors.Join(resultErr, os.RemoveAll(work))
		}
	}()
	if err := writeExclusive(ctx, filepath.Join(work, ".panel-restore-transaction"), []byte(transactionMarker)); err != nil {
		return err
	}
	seen := map[string]bool{}
	var total int64
	for _, item := range files {
		if item.Destination == "" {
			continue
		}
		index := len(plan.Entries)
		destination, err := canonicalFileDestination(item.Destination)
		if err != nil {
			return err
		}
		if seen[destination] || reservedTransactionTarget(destination) || destination == filepath.Join(owner.IdentityDirectory, "configuration.key") && sources[item.Name] == "" {
			return ErrFileRecovery
		}
		seen[destination] = true
		entry := fileEntry{Destination: destination}
		if info, err := os.Lstat(destination); err == nil {
			if !info.Mode().IsRegular() {
				return ErrFileRecovery
			}
			old, err := digest(ctx, destination, archive.MaxUnpackedBytes)
			if err != nil {
				return err
			}
			old.Name = "old/" + strconv.Itoa(index)
			if err := copyNamed(ctx, destination, filepath.Join(work, filepath.FromSlash(old.Name)), old); err != nil {
				return err
			}
			entry.Old = &old
			total += old.Bytes
		} else if !os.IsNotExist(err) {
			return err
		}
		if source := sources[item.Name]; source != "" {
			newFile := item.File
			newFile.Name = "new/" + strconv.Itoa(index)
			if err := copyNamed(ctx, source, filepath.Join(work, filepath.FromSlash(newFile.Name)), newFile); err != nil {
				return err
			}
			entry.New = &newFile
			total += newFile.Bytes
		}
		if entry.Old == nil && entry.New == nil {
			return ErrFileRecovery
		}
		if destination == filepath.Join(owner.IdentityDirectory, "configuration.key") && (entry.Old == nil || entry.New == nil || entry.Old.Bytes != entry.New.Bytes || entry.Old.SHA256 != entry.New.SHA256) {
			return ErrFileRecovery
		}
		if total > 2*archive.MaxUnpackedBytes+1<<20 {
			return ErrFileRecovery
		}
		plan.Entries = append(plan.Entries, entry)
	}
	if len(plan.Entries) == 0 || len(plan.Entries) > 2048 {
		return ErrFileRecovery
	}
	data, err := sealPlan(plan, key)
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return ErrFileRecovery
	}
	if err := writeExclusive(ctx, filepath.Join(work, "journal.json"), data); err != nil {
		return err
	}
	// An exclusive hardlink publishes a complete journal without overwriting
	// an earlier recovery operation. Its private source is retained until cleanup.
	if err := os.Link(filepath.Join(work, "journal.json"), filepath.Join(root, journalName)); err != nil {
		return err
	}
	published = true
	if checkpoint != nil {
		checkpoint("published")
	}
	if err := verifyTransaction(ctx, owner, plan, work); err != nil {
		return err
	}
	for _, entry := range plan.Entries {
		if err := checkDestination(ctx, entry); err != nil {
			return err
		}
	}
	for index, entry := range plan.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := replaceEntry(ctx, work, plan.ID, index, entry, false); err != nil {
			return err
		}
		if checkpoint != nil {
			checkpoint(fmt.Sprintf("installed-%d", index))
		}
	}
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	if checkpoint != nil {
		checkpoint("validated")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	commit := commitMAC(data, key)
	if err := writeExclusive(ctx, filepath.Join(work, "commit.tmp"), []byte(commit)); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(work, "commit.tmp"), filepath.Join(work, "committed")); err != nil {
		return err
	}
	if checkpoint != nil {
		checkpoint("committed")
	}
	return RecoverFiles(context.Background(), owner)
}

// RecoverFiles runs before the panel opens SQLite or validates TLS. Signed
// uncommitted transactions roll back; committed transactions only clean up,
// preserving any later legitimate database writes.
func RecoverFiles(ctx context.Context, owner *panelstore.Ownership) error {
	if err := owner.AssertHeld(owner.Database, owner.IdentityDirectory); err != nil {
		return err
	}
	root := filepath.Dir(owner.Database)
	path := filepath.Join(root, journalName)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
		return ErrFileRecovery
	}
	data, err := readPublishedInstallJournal(root)
	if err != nil {
		return err
	}
	key, err := journalKey(owner)
	if err != nil {
		return err
	}
	defer clear(key)
	plan, err := openPlan(data, key)
	if err != nil {
		return err
	}
	work := filepath.Join(root, ".restore-txn-"+plan.ID)
	if err := verifyTransaction(ctx, owner, plan, work); err != nil {
		return err
	}
	if err := verifyPublishedInstallJournal(root, work, data); err != nil {
		return err
	}
	commit, err := readLimited(work, "committed", 128)
	committed := err == nil && string(commit) == commitMAC(data, key)
	if err == nil && !committed {
		return ErrFileRecovery
	}
	if err != nil {
		if _, statErr := os.Lstat(filepath.Join(work, "committed")); !os.IsNotExist(statErr) {
			return ErrFileRecovery
		}
		// Preflight every destination before restoring anything; foreign changes
		// must never be overwritten by an old journal.
		for _, entry := range plan.Entries {
			if err := checkDestination(ctx, entry); err != nil {
				return err
			}
		}
		if plan.ReadOnlyPreflight {
			if err := cleanupPreparedSidecars(owner, plan); err != nil {
				return err
			}
		}
		for index := len(plan.Entries) - 1; index >= 0; index-- {
			if err := replaceEntry(ctx, work, plan.ID, index, plan.Entries[index], true); err != nil {
				return err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.RemoveAll(work)
}

func journalKey(owner *panelstore.Ownership) ([]byte, error) {
	key, err := readLimited(owner.IdentityDirectory, "configuration.key", 32)
	if err != nil || len(key) != 32 {
		return nil, ErrFileRecovery
	}
	return key, nil
}
func sealPlan(plan filePlan, key []byte) ([]byte, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("proxy-control-restore-files-v1\x00"))
	mac.Write(data)
	return json.Marshal(authenticatedPlan{Plan: data, MAC: hex.EncodeToString(mac.Sum(nil))})
}
func openPlan(data, key []byte) (filePlan, error) {
	var envelope authenticatedPlan
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return filePlan{}, ErrFileRecovery
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("proxy-control-restore-files-v1\x00"))
	mac.Write(envelope.Plan)
	provided, err := hex.DecodeString(envelope.MAC)
	if err != nil || !hmac.Equal(provided, mac.Sum(nil)) {
		return filePlan{}, ErrFileRecovery
	}
	var plan filePlan
	decoder = json.NewDecoder(bytes.NewReader(envelope.Plan))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&plan) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return filePlan{}, ErrFileRecovery
	}
	return plan, nil
}
func commitMAC(data, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("proxy-control-restore-commit-v1\x00"))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyTransaction(ctx context.Context, owner *panelstore.Ownership, plan filePlan, work string) error {
	if plan.Version != 1 || !validRecoveryID(plan.ID) || plan.Database != owner.Database || plan.Identity != owner.IdentityDirectory || len(plan.Entries) == 0 || len(plan.Entries) > 2048 {
		return ErrFileRecovery
	}
	info, err := os.Lstat(work)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrFileRecovery
	}
	marker, err := readLimited(work, ".panel-restore-transaction", 128)
	if err != nil || string(marker) != transactionMarker {
		return ErrFileRecovery
	}
	seen := map[string]bool{}
	allowed := map[string]bool{".panel-restore-transaction": true, "journal.json": true, "committed": true, "commit.tmp": true}
	var total int64
	for index, entry := range plan.Entries {
		destination, err := canonicalFileDestination(entry.Destination)
		if err != nil || destination != entry.Destination || seen[destination] || reservedTransactionTarget(destination) {
			return ErrFileRecovery
		}
		seen[destination] = true
		if destination == filepath.Join(owner.IdentityDirectory, "configuration.key") && (entry.Old == nil || entry.New == nil || entry.Old.Bytes != entry.New.Bytes || entry.Old.SHA256 != entry.New.SHA256) {
			return ErrFileRecovery
		}
		if entry.Old == nil && entry.New == nil {
			return ErrFileRecovery
		}
		for _, item := range []struct {
			file   *archive.File
			prefix string
		}{{entry.Old, "old"}, {entry.New, "new"}} {
			if item.file == nil {
				continue
			}
			file := item.file
			allowed[file.Name] = true
			if file.Name != item.prefix+"/"+strconv.Itoa(index) || file.Bytes < 0 || file.Bytes > archive.MaxUnpackedBytes || !validHash(file.SHA256) {
				return ErrFileRecovery
			}
			actual, err := digest(ctx, filepath.Join(work, filepath.FromSlash(file.Name)), file.Bytes)
			if err != nil || actual.Bytes != file.Bytes || actual.SHA256 != file.SHA256 {
				return ErrFileRecovery
			}
			total += file.Bytes
		}
	}
	if total > 2*archive.MaxUnpackedBytes+1<<20 {
		return ErrFileRecovery
	}
	if plan.ReadOnlyPreflight {
		found := false
		for _, entry := range plan.Entries {
			if entry.Destination == owner.Database && entry.Old != nil && entry.New != nil {
				found = true
			}
		}
		if !found {
			return ErrFileRecovery
		}
	}
	if err := filepath.WalkDir(work, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(work, path)
		if err != nil || entry.Type()&os.ModeSymlink != 0 {
			return ErrFileRecovery
		}
		if entry.IsDir() {
			if relative != "." && relative != "old" && relative != "new" {
				return ErrFileRecovery
			}
			return nil
		}
		if !entry.Type().IsRegular() || !allowed[filepath.ToSlash(relative)] {
			return ErrFileRecovery
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

// Only query_only preflight may have created these unregistered SQLite index
// artifacts. A nonempty WAL could contain newer data and is never discarded.
func cleanupPreparedSidecars(owner *panelstore.Ownership, plan filePlan) error {
	explicit := map[string]bool{}
	for _, entry := range plan.Entries {
		explicit[entry.Destination] = true
	}
	paths := []string{}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := owner.Database + suffix
		if explicit[path] {
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return ErrFileRecovery
		}
		if suffix == "-wal" && info.Size() != 0 || suffix == "-shm" && info.Size() != 32768 {
			return ErrFileRecovery
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		linkErr := filelock.CheckSingleLink(file)
		file.Close()
		if linkErr != nil {
			return ErrFileRecovery
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func canonicalFileDestination(path string) (string, error) {
	if len(path) > 8192 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrFileRecovery
	}
	ancestor := filepath.Dir(path)
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			canonical, err := filepath.EvalSymlinks(ancestor)
			if err != nil || canonical != ancestor {
				return "", ErrFileRecovery
			}
			return path, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if ancestor == filepath.Dir(ancestor) {
			return "", ErrFileRecovery
		}
		ancestor = filepath.Dir(ancestor)
	}
}
func reservedTransactionTarget(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case journalName, ".panel-owner.lock", ".store.lock":
		return true
	}
	return false
}

func checkDestination(ctx context.Context, entry fileEntry) error {
	actual, err := digest(ctx, entry.Destination, archive.MaxUnpackedBytes)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		if _, statErr := os.Lstat(entry.Destination); os.IsNotExist(statErr) {
			return nil
		}
		return err
	}
	for _, known := range []*archive.File{entry.Old, entry.New} {
		if known != nil && actual.Bytes == known.Bytes && actual.SHA256 == known.SHA256 {
			return nil
		}
	}
	return ErrFileRecovery
}
func replaceEntry(ctx context.Context, work, id string, index int, entry fileEntry, rollback bool) error {
	if err := checkDestination(ctx, entry); err != nil {
		return err
	}
	item := entry.New
	prefix := ".restore-file-"
	if rollback {
		item = entry.Old
		prefix = ".rollback-file-"
	}
	temporary := filepath.Join(filepath.Dir(entry.Destination), prefix+id+"-"+strconv.Itoa(index))
	// Both names are journal-owned and must have been absent before publishing.
	for _, name := range []string{".restore-file-", ".rollback-file-"} {
		path := filepath.Join(filepath.Dir(entry.Destination), name+id+"-"+strconv.Itoa(index))
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return ErrFileRecovery
			}
			if err := os.Remove(path); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if item == nil {
		if err := os.Remove(entry.Destination); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := copyNamed(ctx, filepath.Join(work, filepath.FromSlash(item.Name)), temporary, *item); err != nil {
		return err
	}
	if err := os.Rename(temporary, entry.Destination); err != nil {
		return err
	}
	return nil
}

func copyNamed(ctx context.Context, source, destination string, file archive.File) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := filelock.CheckSingleLink(input); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(output, hash), io.LimitReader(cancelReader{ctx, input}, file.Bytes+1))
	if err != nil {
		return err
	}
	if n != file.Bytes || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return ErrFileRecovery
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return output.Close()
}
