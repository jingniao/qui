package updater

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"debug/pe"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"proxycontrol/pkg/contracts/release"
)

var ErrArtifact = errors.New("release artifact size, digest or executable platform mismatch")

type Candidate struct {
	Manifest release.Manifest `json:"manifest"`
	SHA256   string           `json:"sha256"`
	Path     string           `json:"-"`
	Signed   release.Signed   `json:"-"`
}

// Stage validates a signed raw executable into an operator-configured directory.
// Remote release data cannot choose a path or cause process execution. Candidates
// remain non-executable until the separate fixed-component switch is approved.
func Stage(ctx context.Context, directory string, signed release.Signed, policy release.Policy, artifact io.Reader) (candidate Candidate, resultErr error) {
	if err := signed.Verify(policy); err != nil {
		return candidate, err
	}
	if err := ctx.Err(); err != nil {
		return candidate, err
	}
	if artifact == nil {
		return candidate, ErrArtifact
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return candidate, err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return candidate, ErrArtifact
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return candidate, err
	}
	defer root.Close()
	randomName := make([]byte, 16)
	if _, err := rand.Read(randomName); err != nil {
		return candidate, err
	}
	tempName := ".candidate-" + hex.EncodeToString(randomName)
	file, err := root.OpenFile(tempName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return candidate, err
	}
	defer func() { file.Close(); _ = root.Remove(tempName) }()
	if err := file.Chmod(0600); err != nil {
		return candidate, err
	}
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx: ctx, reader: artifact}, signed.Manifest.Bytes+1))
	if err != nil {
		return candidate, err
	}
	if err := ctx.Err(); err != nil {
		return candidate, err
	}
	if count != signed.Manifest.Bytes || hex.EncodeToString(hash.Sum(nil)) != signed.Manifest.SHA256 {
		return candidate, ErrArtifact
	}
	if err := validateExecutable(file, signed.Manifest); err != nil {
		return candidate, err
	}
	if err := file.Sync(); err != nil {
		return candidate, err
	}
	if err := file.Close(); err != nil {
		return candidate, err
	}
	finalName := signed.Manifest.SHA256 + ".bin"
	// An existing content-addressed file must independently validate, rather
	// than being trusted or overwritten when a request is repeated.
	existing, err := openCandidate(root, finalName)
	if err == nil {
		validErr := validateStored(ctx, existing, signed.Manifest)
		existing.Close()
		if validErr != nil {
			return candidate, validErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return candidate, err
	} else {
		// A hard link publishes without replacing another concurrent candidate.
		if err := root.Link(tempName, finalName); err != nil {
			existing, openErr := openCandidate(root, finalName)
			if openErr != nil {
				return candidate, err
			}
			validErr := validateStored(ctx, existing, signed.Manifest)
			existing.Close()
			if validErr != nil {
				return candidate, validErr
			}
		}
	}
	absolute, err := filepath.Abs(filepath.Join(directory, finalName))
	if err != nil {
		return candidate, err
	}
	return Candidate{Manifest: signed.Manifest, SHA256: signed.Manifest.SHA256, Path: absolute, Signed: signed}, nil
}

func openCandidate(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrArtifact
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, ErrArtifact
	}
	return file, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func validateStored(ctx context.Context, file *os.File, manifest release.Manifest) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != manifest.Bytes || info.Mode().Perm()&0111 != 0 {
		return ErrArtifact
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(contextReader{ctx: ctx, reader: file}, manifest.Bytes+1))
	if err != nil {
		return err
	}
	if count != manifest.Bytes || hex.EncodeToString(hash.Sum(nil)) != manifest.SHA256 {
		return ErrArtifact
	}
	return validateExecutable(file, manifest)
}

func validateExecutable(file io.ReaderAt, manifest release.Manifest) error {
	if manifest.OS == "linux" {
		binary, err := elf.NewFile(file)
		if err != nil {
			return ErrArtifact
		}
		defer binary.Close()
		expected := elf.EM_X86_64
		if manifest.Architecture == "arm64" {
			expected = elf.EM_AARCH64
		}
		if binary.Class != elf.ELFCLASS64 || binary.Data != elf.ELFDATA2LSB || binary.Machine != expected || binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN || binary.Entry == 0 {
			return ErrArtifact
		}
	} else {
		binary, err := pe.NewFile(file)
		if err != nil {
			return ErrArtifact
		}
		defer binary.Close()
		expected := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
		if manifest.Architecture == "arm64" {
			expected = pe.IMAGE_FILE_MACHINE_ARM64
		}
		header, ok := binary.OptionalHeader.(*pe.OptionalHeader64)
		if !ok || header.AddressOfEntryPoint == 0 || binary.Machine != expected || binary.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || binary.Characteristics&pe.IMAGE_FILE_DLL != 0 {
			return ErrArtifact
		}
	}
	return nil
}
