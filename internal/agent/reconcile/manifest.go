package reconcile

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Absence is a fresh installation only if there are no retained revisions.
// Inspect at most one entry; damaged or inaccessible storage is not empty.
func readCurrentManifest(directory string) (*Manifest, error) {
	data, err := readInspectionFile(filepath.Join(directory, "current.json"), 64<<10)
	if errors.Is(err, os.ErrNotExist) {
		path := filepath.Join(directory, "revisions")
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("unsafe retained revisions directory")
		}
		folder, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer folder.Close()
		opened, err := folder.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return nil, errors.New("retained revisions directory changed")
		}
		entries, err := folder.ReadDir(1)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(entries) != 0 {
			return nil, errors.New("missing current manifest with retained revisions")
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(&struct{}{}) != io.EOF || manifest.Revision < 0 || len(manifest.Successful) > 5 {
		return nil, errors.New("invalid current manifest")
	}
	found := manifest.Revision == 0
	seen := map[int64]bool{}
	for _, revision := range manifest.Successful {
		if revision < 1 || seen[revision] {
			return nil, errors.New("invalid retained successful revisions")
		}
		seen[revision] = true
		found = found || revision == manifest.Revision
	}
	if !found {
		return nil, errors.New("current revision is not retained as successful")
	}
	if manifest.Revision == 0 {
		if manifest.Digest != "" {
			return nil, errors.New("empty current revision has a retained digest")
		}
	} else {
		raw, err := hex.DecodeString(manifest.Digest)
		if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != manifest.Digest {
			return nil, errors.New("invalid current revision digest")
		}
	}
	if err := validateSuccessReceiptMarkers(manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}
