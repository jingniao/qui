package updater

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"proxycontrol/internal/platform/filelock"
	"proxycontrol/pkg/contracts/release"
)

func InspectImage(ctx context.Context, component, path, version string) (Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return Image{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > release.MaxArtifactBytes {
		return Image{}, ErrArtifact
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, reader: file}, release.MaxArtifactBytes+1))
	if err != nil {
		return Image{}, err
	}
	image := Image{SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: n, Version: version}
	if !image.valid(component) || validateExecutable(file, image.manifest(component)) != nil {
		return Image{}, ErrArtifact
	}
	return image, nil
}
func imageName(component string, image Image) string {
	name := component + "-" + image.SHA256 + ".bin"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}
func validateImageFile(ctx context.Context, file *os.File, component string, image Image) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != image.Bytes || info.Mode().Perm()&0222 != 0 {
		return ErrArtifact
	}
	if err := filelock.CheckSingleLink(file); err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, reader: file}, image.Bytes+1))
	if err != nil {
		return err
	}
	if n != image.Bytes || hex.EncodeToString(hash.Sum(nil)) != image.SHA256 {
		return ErrArtifact
	}
	return validateExecutable(file, image.manifest(component))
}

// MaterializeImage copies to a component/digest-derived local path. Callers must
// supply a verified release or the explicitly configured original core. No
// remote executable name, arguments, archive entries or script are supported.
func MaterializeImage(ctx context.Context, directory, source, component string, image Image) (string, error) {
	if !image.valid(component) {
		return "", ErrArtifact
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrArtifact
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name := imageName(component, image)
	if existing, err := openCandidate(root, name); err == nil {
		defer existing.Close()
		if err := validateImageFile(ctx, existing, component, image); err != nil {
			return "", err
		}
		return filepath.Abs(filepath.Join(directory, name))
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	inputInfo, err := input.Stat()
	if err != nil || !inputInfo.Mode().IsRegular() || inputInfo.Size() != image.Bytes {
		return "", ErrArtifact
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	temporary := ".image-" + hex.EncodeToString(random)
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer func() { file.Close(); _ = root.Remove(temporary) }()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx: ctx, reader: input}, image.Bytes+1))
	if err != nil {
		return "", err
	}
	if n != image.Bytes || hex.EncodeToString(hash.Sum(nil)) != image.SHA256 || validateExecutable(file, image.manifest(component)) != nil {
		return "", ErrArtifact
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := root.Link(temporary, name); err != nil {
		return "", err
	}
	if err := root.Remove(temporary); err != nil {
		return "", err
	}
	// Windows cannot remove the temporary hardlink after a shared read-only
	// attribute has been set. Remove it first, then seal the final image. An
	// interrupted writable image is rejected rather than treated as installed.
	published, err := root.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	if err := published.Chmod(0500); err != nil {
		published.Close()
		return "", err
	}
	if err := published.Sync(); err != nil {
		published.Close()
		return "", err
	}
	if err := published.Close(); err != nil {
		return "", err
	}
	if err := syncHistoryDirectory(root); err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(directory, name))
}
func ResolveImage(ctx context.Context, directory, component string, image Image) (string, error) {
	if !image.valid(component) {
		return "", ErrArtifact
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name := imageName(component, image)
	file, err := openCandidate(root, name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if err := validateImageFile(ctx, file, component, image); err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(directory, name))
}
