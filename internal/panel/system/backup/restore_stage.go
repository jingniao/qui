package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"proxycontrol/internal/platform/config"
	"proxycontrol/internal/platform/filelock"
	"reflect"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const restoreMarker = "proxy-control-restore-stage-v1\n"
const restorePlanName = ".restore-plan.json"
const restoreConfigName = "configuration/target.yaml"
const maxRestorePlanBytes = 1 << 20

var ErrRestoreStage = errors.New("restore staging integrity or target validation failed")

// RestoreTarget maps authenticated backup materials onto explicitly chosen
// local paths. Endpoints/mode come from the target, not the former machine.
type RestoreTarget struct {
	Panel             config.Panel `json:"panel"`
	ConfigurationPath string       `json:"configurationPath"`
}

type RestoreFile struct {
	File
	Destination string `json:"destination"`
}

type RestorePlan struct {
	Version       int           `json:"version"`
	PreparedAt    time.Time     `json:"preparedAt"`
	ArchiveSHA256 string        `json:"archiveSha256"`
	ArchiveBytes  int64         `json:"archiveBytes"`
	Source        Manifest      `json:"source"`
	Target        RestoreTarget `json:"target"`
	Files         []RestoreFile `json:"files"`
}

type PreparedRestore struct {
	Directory      string
	ManifestSHA256 string
	Plan           RestorePlan
}

// PrepareRestore retains an isolated verified material set for the offline
// recovery coordinator. It never opens the target DB, acquires live ownership,
// replaces files, starts services or authorizes restoring a previewed snapshot.
// The returned manifest digest must be pinned by the coordinator independently
// of the stage before it can be used after a process restart.
func PrepareRestore(ctx context.Context, directory, archivePath, password string, target RestoreTarget) (prepared PreparedRestore, resultErr error) {
	return prepareRestore(ctx, directory, archivePath, password, target)
}

func prepareRestore(ctx context.Context, directory, archivePath, password string, target RestoreTarget) (prepared PreparedRestore, resultErr error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return prepared, err
	}
	directory = absolute
	validate := validateContents
	var stage string
	defer func() {
		if resultErr != nil && stage != "" {
			resultErr = errors.Join(resultErr, removeRestoreStage(directory, stage))
			prepared = PreparedRestore{}
		}
	}()
	_, err = inspectWithValidation(ctx, directory, archivePath, password, func(result Result, work string) error {
		var err error
		target, err = normalizeRestoreTarget(target)
		if err != nil {
			return err
		}
		stage, err = os.MkdirTemp(directory, ".restore-")
		if err != nil {
			return err
		}
		if err := writeProtected(ctx, filepath.Join(stage, ".panel-restore-stage"), []byte(restoreMarker)); err != nil {
			return err
		}
		plan := RestorePlan{Version: 1, PreparedAt: time.Now().UTC(), ArchiveSHA256: result.SHA256, ArchiveBytes: result.Bytes, Source: result.Manifest, Target: target}
		plan.Files, err = restoreFiles(plan.Source, target)
		if err != nil {
			return err
		}
		if err := validateStageDestinations(stage, work, archivePath, plan.Files, target.ConfigurationPath); err != nil {
			return err
		}
		for _, item := range plan.Source.Files {
			if err := copyRestoreFile(ctx, work, stage, item); err != nil {
				return err
			}
		}
		configuration, err := yaml.Marshal(target.Panel)
		if err != nil || len(configuration) > int(fileLimit("configuration/panel.yaml")) {
			return ErrRestoreStage
		}
		if err := writeProtected(ctx, filepath.Join(stage, filepath.FromSlash(restoreConfigName)), configuration); err != nil {
			return err
		}
		digest := sha256.Sum256(configuration)
		plan.Files = append(plan.Files, RestoreFile{File: File{Name: restoreConfigName, Bytes: int64(len(configuration)), SHA256: hex.EncodeToString(digest[:])}, Destination: target.ConfigurationPath})
		if err := validate(ctx, stage); err != nil {
			return err
		}
		if err := validateTargetMaterials(target.Panel, stage); err != nil {
			return err
		}
		data, err := json.Marshal(plan)
		if err != nil || len(data) > maxRestorePlanBytes {
			return ErrRestoreStage
		}
		// A completed manifest is written last. An interrupted stage without it
		// cannot be mistaken for a verified recovery candidate.
		if err := writeProtected(ctx, filepath.Join(stage, restorePlanName), data); err != nil {
			return err
		}
		manifestDigest := sha256.Sum256(data)
		prepared = PreparedRestore{Directory: stage, ManifestSHA256: hex.EncodeToString(manifestDigest[:]), Plan: plan}
		_, err = VerifyPreparedRestore(ctx, stage, prepared.ManifestSHA256)
		return err
	}, validate)
	if err != nil {
		return PreparedRestore{}, err
	}
	return prepared, nil
}

// VerifyPreparedRestore requires a separately trusted plan digest. A digest
// read from this same directory is not an authentication or execution permit.
func VerifyPreparedRestore(ctx context.Context, directory, expectedManifestSHA256 string) (RestorePlan, error) {
	return verifyPreparedRestore(ctx, directory, expectedManifestSHA256)
}

func readRestorePlan(directory, expectedManifestSHA256 string) (RestorePlan, error) {
	var plan RestorePlan
	if !validDigest(expectedManifestSHA256) {
		return plan, ErrRestoreStage
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !strings.HasPrefix(filepath.Base(directory), ".restore-") {
		return plan, ErrRestoreStage
	}
	marker, err := readRestoreFile(directory, ".panel-restore-stage", 128)
	if err != nil || string(marker) != restoreMarker {
		return plan, ErrRestoreStage
	}
	data, err := readRestoreFile(directory, restorePlanName, maxRestorePlanBytes)
	if err != nil {
		return plan, ErrRestoreStage
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != expectedManifestSHA256 {
		return plan, ErrRestoreStage
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil || decoder.Decode(&struct{}{}) != io.EOF || plan.Version != 1 || plan.PreparedAt.IsZero() || !validDigest(plan.ArchiveSHA256) || plan.ArchiveBytes < headerBytes || plan.ArchiveBytes > MaxArchiveBytes {
		return RestorePlan{}, ErrRestoreStage
	}
	manifestData, err := json.Marshal(plan.Source)
	if err != nil {
		return RestorePlan{}, ErrRestoreStage
	}
	if _, err := decodeManifest(manifestData); err != nil {
		return RestorePlan{}, ErrRestoreStage
	}
	normalized, err := normalizeRestoreTarget(plan.Target)
	if err != nil || !reflect.DeepEqual(normalized, plan.Target) {
		return RestorePlan{}, ErrRestoreStage
	}
	return plan, nil
}

func verifyPreparedRestore(ctx context.Context, directory, expectedManifestSHA256 string) (RestorePlan, error) {
	plan, err := readRestorePlan(directory, expectedManifestSHA256)
	if err != nil {
		return RestorePlan{}, err
	}
	expectedFiles, err := restoreFiles(plan.Source, plan.Target)
	if err != nil || len(plan.Files) != len(expectedFiles)+1 {
		return RestorePlan{}, ErrRestoreStage
	}
	if err := validateStageDestinations(directory, "", "", expectedFiles, plan.Target.ConfigurationPath); err != nil {
		return RestorePlan{}, err
	}
	configuration, err := yaml.Marshal(plan.Target.Panel)
	if err != nil || len(configuration) > int(fileLimit("configuration/panel.yaml")) {
		return RestorePlan{}, ErrRestoreStage
	}
	configDigest := sha256.Sum256(configuration)
	expectedFiles = append(expectedFiles, RestoreFile{File: File{Name: restoreConfigName, Bytes: int64(len(configuration)), SHA256: hex.EncodeToString(configDigest[:])}, Destination: plan.Target.ConfigurationPath})
	allowedFiles := map[string]bool{".panel-restore-stage": true, restorePlanName: true}
	allowedDirectories := map[string]bool{".": true}
	for i, item := range expectedFiles {
		if plan.Files[i] != item {
			return RestorePlan{}, ErrRestoreStage
		}
		allowedFiles[item.Name] = true
		for parent := filepath.Dir(filepath.FromSlash(item.Name)); parent != "."; parent = filepath.Dir(parent) {
			allowedDirectories[filepath.ToSlash(parent)] = true
		}
	}
	// Refuse sidecars, stale jobs, links and anything outside the verified set.
	err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil || entry.Type()&os.ModeSymlink != 0 {
			return ErrRestoreStage
		}
		name := filepath.ToSlash(relative)
		if entry.IsDir() {
			if !allowedDirectories[name] {
				return ErrRestoreStage
			}
			return nil
		}
		if !entry.Type().IsRegular() || !allowedFiles[name] {
			return ErrRestoreStage
		}
		return nil
	})
	if err != nil {
		return RestorePlan{}, ErrRestoreStage
	}
	for _, item := range expectedFiles {
		file, err := digestRestoreFile(ctx, directory, item)
		if err != nil || file.Bytes != item.Bytes || file.SHA256 != item.SHA256 {
			return RestorePlan{}, errors.Join(ErrRestoreStage, ctx.Err())
		}
	}
	if err := validateContents(ctx, directory); err != nil {
		return RestorePlan{}, err
	}
	if err := validateTargetMaterials(plan.Target.Panel, directory); err != nil {
		return RestorePlan{}, err
	}
	return plan, nil
}

// DiscardPreparedRestore deletes only the caller-pinned, fully checked stage.
// It deliberately refuses corrupted/unrecognized stages for manual diagnosis.
func DiscardPreparedRestore(ctx context.Context, prepared PreparedRestore) error {
	if _, err := VerifyPreparedRestore(ctx, prepared.Directory, prepared.ManifestSHA256); err != nil {
		return err
	}
	return removeRestoreStage(filepath.Dir(prepared.Directory), prepared.Directory)
}

func normalizeRestoreTarget(target RestoreTarget) (RestoreTarget, error) {
	if !target.Panel.Development && (target.Panel.TLS.Certificate == "" || target.Panel.TLS.Key == "" || target.Panel.TLS.ClientCA == "") {
		return target, ErrRestoreStage
	}
	for _, path := range []*string{&target.Panel.Database, &target.Panel.NodeIdentityDirectory, &target.ConfigurationPath} {
		if *path == "" {
			return target, ErrRestoreStage
		}
		resolved, err := resolveDestination(*path)
		if err != nil {
			return target, err
		}
		*path = resolved
	}
	for _, path := range []*string{&target.Panel.TLS.Certificate, &target.Panel.TLS.Key, &target.Panel.TLS.ClientCA} {
		if *path == "" {
			continue
		}
		resolved, err := resolveDestination(*path)
		if err != nil {
			return target, err
		}
		*path = resolved
	}
	if info, err := os.Stat(target.Panel.NodeIdentityDirectory); err == nil {
		if !info.IsDir() {
			return target, ErrRestoreStage
		}
	} else if !os.IsNotExist(err) {
		return target, err
	}
	for _, path := range []string{target.Panel.Database, target.ConfigurationPath, target.Panel.TLS.Certificate, target.Panel.TLS.Key, target.Panel.TLS.ClientCA} {
		if path == "" {
			continue
		}
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return target, ErrRestoreStage
			}
		} else if !os.IsNotExist(err) {
			return target, err
		}
	}
	return target, nil
}

// Resolve existing parent aliases without creating target runtime directories.
func resolveDestination(path string) (string, error) {
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") || strings.Contains(path, "://") {
		return "", ErrRestoreStage
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	ancestor := absolute
	var missing []string
	for {
		info, err := os.Lstat(ancestor)
		if err == nil {
			if ancestor == absolute && info.Mode()&os.ModeSymlink != 0 {
				return "", ErrRestoreStage
			}
			if len(missing) > 0 && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return "", ErrRestoreStage
			}
			canonical, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				canonical = filepath.Join(canonical, missing[i])
			}
			return canonical, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(ancestor) == ancestor {
			return "", ErrRestoreStage
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = filepath.Dir(ancestor)
	}
}

func restoreFiles(source Manifest, target RestoreTarget) ([]RestoreFile, error) {
	for _, item := range []struct {
		name  string
		limit int64
	}{{"node-ca-round-active.enc", 1 << 20}, {"node-ca-round-retired.enc", 32768}} {
		path := filepath.Join(target.Panel.NodeIdentityDirectory, item.name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		data, err := readRestoreFile(target.Panel.NodeIdentityDirectory, item.name, item.limit)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(data)
		matched := false
		for _, file := range source.Files {
			if file.Name == "identity/"+item.name && file.SHA256 == hex.EncodeToString(hash[:]) {
				matched = true
			}
		}
		if !matched {
			return nil, errors.New("restore cannot remove or replace authenticated successor CA history")
		}
	}
	if _, err := os.Lstat(filepath.Join(target.Panel.NodeIdentityDirectory, "node-ca-retired.enc")); err == nil {
		retired, err := readRestoreFile(target.Panel.NodeIdentityDirectory, "node-ca-retired.enc", 32768)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(retired)
		matched := false
		for _, item := range source.Files {
			if item.Name == "identity/node-ca-retired.enc" && item.SHA256 == hex.EncodeToString(digest[:]) {
				matched = true
			}
		}
		if !matched {
			return nil, errors.New("restore cannot remove or replace current original CA retirement")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, activeErr := os.Lstat(filepath.Join(target.Panel.NodeIdentityDirectory, "node-ca-active.enc"))
	if activeErr == nil {
		active, err := readRestoreFile(target.Panel.NodeIdentityDirectory, "node-ca-active.enc", 32768)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(active)
		matched := false
		for _, item := range source.Files {
			if item.Name == "identity/node-ca-active.enc" && item.SHA256 == hex.EncodeToString(digest[:]) {
				matched = true
			}
		}
		if !matched {
			return nil, errors.New("restore cannot remove or replace the current active CA transition")
		}
	} else if !errors.Is(activeErr, os.ErrNotExist) {
		return nil, activeErr
	}
	// A retained recovered current-root base must likewise survive a restore
	// unchanged: a backup that predates it cannot silently drop it, and a
	// different base cannot replace it. Old archives without the file are still
	// compatible because the guard only fires when the target already has one.
	if baseInfo, baseErr := os.Lstat(filepath.Join(target.Panel.NodeIdentityDirectory, "node-ca-recovered-base.enc")); baseErr == nil {
		if !baseInfo.Mode().IsRegular() || baseInfo.Size() < 1 || baseInfo.Size() > 1<<20 {
			return nil, ErrRestoreStage
		}
		base, err := readRestoreFile(target.Panel.NodeIdentityDirectory, "node-ca-recovered-base.enc", 1<<20)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(base)
		matched := false
		for _, item := range source.Files {
			if item.Name == "identity/node-ca-recovered-base.enc" && item.SHA256 == hex.EncodeToString(digest[:]) {
				matched = true
			}
		}
		if !matched {
			return nil, errors.New("restore cannot remove or replace the current recovered current-root base")
		}
	} else if !errors.Is(baseErr, os.ErrNotExist) {
		return nil, baseErr
	}
	// A retained recovered-round head and its retirement proof must likewise
	// survive a restore unchanged, exactly like the recovered base: a backup
	// that predates them cannot silently drop them and a different head cannot
	// replace them. Old archives without the files stay compatible because the
	// guard only fires when the target already has one.
	for _, item := range []struct {
		name  string
		limit int64
	}{{"node-ca-recovered-round.enc", 1 << 20}, {"node-ca-recovered-retired.enc", 32768}} {
		info, err := os.Lstat(filepath.Join(target.Panel.NodeIdentityDirectory, item.name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > item.limit {
			return nil, ErrRestoreStage
		}
		data, err := readRestoreFile(target.Panel.NodeIdentityDirectory, item.name, item.limit)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(data)
		matched := false
		for _, file := range source.Files {
			if file.Name == "identity/"+item.name && file.SHA256 == hex.EncodeToString(digest[:]) {
				matched = true
			}
		}
		if !matched {
			return nil, errors.New("restore cannot remove or replace authenticated recovered CA history")
		}
	}
	files := make([]RestoreFile, 0, len(source.Files)+1)
	seen := map[string]bool{}
	key := func(path string) string {
		if runtime.GOOS == "windows" {
			return strings.ToLower(path)
		}
		return path
	}
	seen[key(target.ConfigurationPath)] = true
	if reservedRestoreDestination(target.ConfigurationPath) {
		return nil, ErrRestoreStage
	}
	for _, item := range source.Files {
		destination := ""
		switch {
		case item.Name == "database/panel.db":
			destination = target.Panel.Database
		case strings.HasPrefix(item.Name, "identity/"):
			destination = filepath.Join(target.Panel.NodeIdentityDirectory, strings.TrimPrefix(item.Name, "identity/"))
		case strings.HasPrefix(item.Name, "rulesets/"):
			destination = filepath.Join(filepath.Dir(target.Panel.Database), filepath.FromSlash(item.Name))
		case item.Name == "tls/server.crt" && !target.Panel.Development:
			destination = target.Panel.TLS.Certificate
		case item.Name == "tls/server.key" && !target.Panel.Development:
			destination = target.Panel.TLS.Key
		case item.Name == "tls/client-ca.crt" && !target.Panel.Development:
			destination = target.Panel.TLS.ClientCA
		}
		if destination != "" {
			if reservedRestoreDestination(destination) {
				return nil, ErrRestoreStage
			}
			resolved, err := resolveDestination(destination)
			if err != nil || key(resolved) != key(destination) {
				return nil, ErrRestoreStage
			}
			if info, err := os.Lstat(destination); err == nil {
				if !info.Mode().IsRegular() {
					return nil, ErrRestoreStage
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
			if seen[key(destination)] {
				return nil, ErrRestoreStage
			}
			seen[key(destination)] = true
		}
		files = append(files, RestoreFile{File: item, Destination: destination})
	}
	// A file destination cannot also be an ancestor directory of another file.
	for path := range seen {
		for parent := filepath.Dir(path); parent != path; parent = filepath.Dir(parent) {
			if seen[parent] {
				return nil, ErrRestoreStage
			}
			if parent == filepath.Dir(parent) {
				break
			}
		}
	}
	return files, nil
}

func reservedRestoreDestination(path string) bool {
	name := filepath.Base(path)
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
	}
	// Product material must never overwrite execution permits or their fixed
	// publication aliases, including offline coordinator startup fences.
	for _, prefix := range []string{".panel-controller-", ".panel-handoff-", ".panel-recovery-coordinator", ".panel-settled-candidate-"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	switch name {
	case ".panel-owner.lock", ".store.lock", ".panel-restore.json", ".panel-restore-stage", restorePlanName:
		return true
	}
	return false
}

func validateStageDestinations(stage, work, archivePath string, files []RestoreFile, configurationPath string) error {
	paths := []string{configurationPath}
	for _, file := range files {
		if file.Destination != "" {
			paths = append(paths, file.Destination)
		}
	}
	for _, destination := range paths {
		for _, protected := range []string{stage, work} {
			if protected == "" {
				continue
			}
			absolute, err := filepath.Abs(protected)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(absolute, destination)
			if err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
				return ErrRestoreStage
			}
		}
		if archivePath != "" {
			absolute, err := filepath.Abs(archivePath)
			if err != nil {
				return err
			}
			if destination == absolute || runtime.GOOS == "windows" && strings.EqualFold(destination, absolute) {
				return ErrRestoreStage
			}
		}
	}
	return nil
}

func validateTargetMaterials(panel config.Panel, stage string) error {
	panel.Database = filepath.Join(stage, "database", "panel.db")
	panel.NodeIdentityDirectory = filepath.Join(stage, "identity")
	if !panel.Development {
		panel.TLS = config.TLS{Certificate: filepath.Join(stage, "tls", "server.crt"), Key: filepath.Join(stage, "tls", "server.key"), ClientCA: filepath.Join(stage, "tls", "client-ca.crt")}
	}
	if err := panel.Validate(); err != nil {
		return ErrRestoreStage
	}
	return nil
}

func copyRestoreFile(ctx context.Context, source, stage string, item File) error {
	input, err := os.Open(filepath.Join(source, filepath.FromSlash(item.Name)))
	if err != nil {
		return err
	}
	defer input.Close()
	path := filepath.Join(stage, filepath.FromSlash(item.Name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(output, digest), io.LimitReader(contextReader{ctx, input}, item.Bytes+1))
	if err != nil {
		return err
	}
	if n != item.Bytes || hex.EncodeToString(digest.Sum(nil)) != item.SHA256 {
		return ErrRestoreStage
	}
	if err := output.Sync(); err != nil {
		return err
	}
	return output.Close()
}

func writeProtected(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

func readRestoreFile(directory, name string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, ErrRestoreStage
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err := filelock.CheckSingleLink(file); err != nil {
		return nil, ErrRestoreStage
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrRestoreStage
	}
	return data, nil
}

func digestRestoreFile(ctx context.Context, directory string, item RestoreFile) (File, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return File{}, err
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(item.Name))
	if err != nil {
		return File{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != item.Bytes {
		return File{}, ErrRestoreStage
	}
	if err := filelock.CheckSingleLink(file); err != nil {
		return File{}, ErrRestoreStage
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(contextReader{ctx, file}, item.Bytes+1))
	if err != nil {
		return File{}, err
	}
	return File{Bytes: n, SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func removeRestoreStage(directory, stage string) error {
	parent, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(stage)
	if err != nil || filepath.Dir(target) != parent || !strings.HasPrefix(filepath.Base(target), ".restore-") {
		return ErrRestoreStage
	}
	return os.RemoveAll(target)
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}
