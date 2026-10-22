package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"proxycontrol/pkg/contracts/release"
)

// coreBinaryName resolves the expected executable name inside an archive. It is
// a variable so archive fixtures can stay independent of the host platform.
var coreBinaryName = release.CoreBinaryName

// extractCore writes the single expected executable to a fixed name in the
// staging directory. Archive entry names never choose a path: the destination
// is derived from the component and the running platform only.
func extractCore(asset, component, archive, staging string) (string, error) {
	// The staging name is fixed and never taken from the archive. Windows needs
	// the executable suffix before the core can be run for its version.
	target := filepath.Join(staging, "core"+coreExecutableSuffix())
	expected := coreBinaryName(component, runtime.GOOS)
	var err error
	switch release.ArchiveKind(asset) {
	case "zip":
		err = extractZip(archive, expected, target)
	case "tar.gz":
		err = extractTarGz(archive, expected, target)
	default:
		return "", ErrCoreArchive
	}
	if err != nil {
		os.Remove(target)
		return "", err
	}
	return target, nil
}

// safeEntryName rejects absolute paths, traversal and separators that differ
// between the archive and the local filesystem.
func safeEntryName(name string) (string, error) {
	if name == "" || len(name) > 256 || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") {
		return "", ErrCoreArchive
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrCoreArchive
	}
	return clean, nil
}

func copyEntry(reader io.Reader, target string) error {
	// The staged core is run for its version before it is installed, so it must
	// be executable by its owner.
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return ErrCoreArchive
	}
	count, err := io.Copy(file, io.LimitReader(reader, MaxCoreBinaryBytes+1))
	if err != nil {
		file.Close()
		return ErrCoreArchive
	}
	if count < 1 || count > MaxCoreBinaryBytes || file.Sync() != nil || file.Close() != nil {
		return ErrCoreArchive
	}
	return nil
}

// coreExecutableSuffix is the platform suffix the staged core needs to run.
func coreExecutableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func extractZip(archive, expected, target string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return ErrCoreArchive
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > maxCoreArchiveEntries {
		return ErrCoreArchive
	}
	seen := map[string]bool{}
	found := false
	for _, entry := range reader.File {
		name, err := safeEntryName(entry.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return ErrCoreArchive
		}
		seen[name] = true
		mode := entry.Mode()
		if mode&os.ModeSymlink != 0 || mode&os.ModeDevice != 0 || mode&os.ModeNamedPipe != 0 || mode&os.ModeSocket != 0 || mode&os.ModeIrregular != 0 {
			return ErrCoreArchive
		}
		if mode.IsDir() || strings.HasSuffix(entry.Name, "/") {
			continue
		}
		if path.Base(name) != expected {
			continue
		}
		if found || entry.UncompressedSize64 > uint64(MaxCoreBinaryBytes) {
			return ErrCoreArchive
		}
		opened, err := entry.Open()
		if err != nil {
			return ErrCoreArchive
		}
		err = copyEntry(opened, target)
		opened.Close()
		if err != nil {
			return err
		}
		found = true
	}
	if !found {
		return ErrCoreArchive
	}
	return nil
}

func extractTarGz(archive, expected, target string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return ErrCoreArchive
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	seen := map[string]bool{}
	found := false
	for count := 0; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrCoreArchive
		}
		if count >= maxCoreArchiveEntries {
			return ErrCoreArchive
		}
		name, err := safeEntryName(header.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return ErrCoreArchive
		}
		seen[name] = true
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
		default:
			// Symlinks, hard links, devices and other special entries.
			return ErrCoreArchive
		}
		if path.Base(name) != expected {
			continue
		}
		if found || header.Size < 1 || header.Size > MaxCoreBinaryBytes {
			return ErrCoreArchive
		}
		if err := copyEntry(reader, target); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return ErrCoreArchive
	}
	return nil
}
