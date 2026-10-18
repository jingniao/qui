package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/release"
)

var ErrHistory = errors.New("release acceptance history requires original authenticated records")
var ErrReplay = errors.New("release sequence is older or conflicts with accepted release")

const maxHistoryEvents = 10000
const maxHistoryRecord = 32 << 10

// HistoryOptions binds the acceptance log to one enrolled node and an existing
// local identity key. Initialization is explicit; Open never replaces a missing
// key, checkpoint or ledger with an empty history.
type HistoryOptions struct {
	Directory  string
	Checkpoint string
	NodeID     string
	Key        []byte
}
type historyHead struct {
	Kind   string `json:"kind"`
	NodeID string `json:"node_id"`
	Number int64  `json:"number"`
	Digest string `json:"digest"`
}
type historyEvent struct {
	Kind             string            `json:"kind"`
	NodeID           string            `json:"node_id"`
	Number           int64             `json:"number"`
	Previous         string            `json:"previous"`
	Release          release.Signed    `json:"release"`
	Installation     *Installation     `json:"installation,omitempty"`
	BaselineRecovery *BaselineRecovery `json:"baseline_recovery,omitempty"`
	// Image records the locally verified executable of a direct-URL core
	// update, which has no signed acceptance to bind the target to.
	Image *Image `json:"image,omitempty"`
}
type History struct {
	inspectionOnly          bool
	mu                      sync.Mutex
	root                    *os.Root
	anchor                  *os.Root
	anchorName              string
	nodeID                  string
	key                     *vault.Vault
	lock, anchorLock        *filelock.Lock
	head                    historyHead
	highest                 map[string]release.Manifest
	directoryInfo           os.FileInfo
	installations           map[string]Installation
	requests                map[string]Installation
	baselineRecoveries      map[string]BaselineRecovery
	currentBaselineRecovery map[string]string
	refusedInstallations    map[string]bool
	verifiedImages          map[string]map[Image]bool
	broken                  bool
}

func historyPaths(options HistoryOptions) (string, string, error) {
	if options.Directory == "" || options.Checkpoint == "" || options.NodeID == "" || len(options.NodeID) > 128 || len(options.Key) != 32 {
		return "", "", ErrHistory
	}
	directory, err := filepath.Abs(options.Directory)
	if err != nil {
		return "", "", err
	}
	checkpoint, err := filepath.Abs(options.Checkpoint)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(directory, checkpoint)
	if err != nil || rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return "", "", ErrHistory
	}
	return directory, checkpoint, nil
}

// CreateHistory is only for explicit first-time local setup. A partially
// initialized directory remains visible on failure and must be investigated.
func CreateHistory(options HistoryOptions) (*History, error) {
	directory, checkpoint, err := historyPaths(options)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(checkpoint); !errors.Is(err, os.ErrNotExist) {
		return nil, ErrHistory
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(checkpoint), 0700); err != nil {
		return nil, err
	}
	h, err := historyHandles(options, directory, checkpoint)
	if err != nil {
		return nil, err
	}
	head := historyHead{Kind: "release-checkpoint-v1", NodeID: options.NodeID}
	data, err := h.encode(head)
	if err == nil {
		err = exclusiveRoot(h.anchor, h.anchorName, data)
	}
	if err != nil {
		h.Close()
		return nil, err
	}
	h.head = head
	return h, nil
}
func OpenHistory(options HistoryOptions) (*History, error) {
	directory, checkpoint, err := historyPaths(options)
	if err != nil {
		return nil, err
	}
	h, err := historyHandles(options, directory, checkpoint)
	if err != nil {
		return nil, err
	}
	if err := h.load(); err != nil {
		h.Close()
		return nil, err
	}
	return h, nil
}
func historyHandles(options HistoryOptions, directory, checkpoint string) (*History, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrHistory
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	h := &History{root: root, nodeID: options.NodeID, anchorName: filepath.Base(checkpoint), highest: map[string]release.Manifest{}, directoryInfo: info, installations: map[string]Installation{}, requests: map[string]Installation{}, baselineRecoveries: map[string]BaselineRecovery{}, currentBaselineRecovery: map[string]string{}, refusedInstallations: map[string]bool{}, verifiedImages: map[string]map[Image]bool{}}
	h.key, err = vault.FromKey(options.Key)
	if err == nil {
		h.lock, err = filelock.Try(filepath.Join(directory, ".history.lock"))
	}
	if err == nil {
		h.anchorLock, err = filelock.Try(checkpoint + ".lock")
	}
	if err == nil {
		h.anchor, err = os.OpenRoot(filepath.Dir(checkpoint))
	}
	if err != nil {
		h.Close()
		return nil, err
	}
	return h, nil
}
func (h *History) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var errs []error
	if h.anchor != nil {
		errs = append(errs, h.anchor.Close())
		h.anchor = nil
	}
	if h.root != nil {
		errs = append(errs, h.root.Close())
		h.root = nil
	}
	if h.anchorLock != nil {
		errs = append(errs, h.anchorLock.Close())
		h.anchorLock = nil
	}
	if h.lock != nil {
		errs = append(errs, h.lock.Close())
		h.lock = nil
	}
	return errors.Join(errs...)
}
func (h *History) held() error {
	if h.root == nil || h.anchor == nil || h.broken {
		return ErrHistory
	}
	if err := h.lock.AssertHeld(); err != nil {
		return err
	}
	if err := h.anchorLock.AssertHeld(); err != nil {
		return err
	}
	info, err := os.Lstat(h.root.Name())
	if err != nil || !os.SameFile(info, h.directoryInfo) {
		return ErrHistory
	}
	return nil
}

// RecoverCheckpoint revalidates the complete authenticated journal while the
// same lifetime locks remain held. Failure poisons this handle for all writes.
func (h *History) RecoverCheckpoint() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.held(); err != nil {
		return err
	}
	h.highest = map[string]release.Manifest{}
	h.installations = map[string]Installation{}
	h.requests = map[string]Installation{}
	h.baselineRecoveries = map[string]BaselineRecovery{}
	h.currentBaselineRecovery = map[string]string{}
	h.refusedInstallations = map[string]bool{}
	if err := h.load(); err != nil {
		h.broken = true
		return err
	}
	return nil
}
func (h *History) encode(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return h.key.Seal(data)
}
func (h *History) decode(root *os.Root, name string, value any) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxHistoryRecord {
		return nil, ErrHistory
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, ErrHistory
	}
	if err := filelock.CheckSingleLink(file); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxHistoryRecord+1))
	if err != nil || len(data) > maxHistoryRecord {
		return nil, ErrHistory
	}
	plaintext, err := h.key.Open(data)
	if err != nil {
		return nil, ErrHistory
	}
	d := json.NewDecoder(bytes.NewReader(plaintext))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(&struct{}{}) != io.EOF {
		return nil, ErrHistory
	}
	return data, nil
}
func eventName(number int64) string  { return fmt.Sprintf("%020d.event.enc", number) }
func eventDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (h *History) load() error {
	if err := h.held(); err != nil {
		return err
	}
	var head historyHead
	if _, err := h.decode(h.anchor, h.anchorName, &head); err != nil {
		return err
	}
	if head.Kind != "release-checkpoint-v1" || head.NodeID != h.nodeID || head.Number < 0 || head.Number > maxHistoryEvents || head.Number == 0 && head.Digest != "" || head.Number > 0 && len(head.Digest) != 64 {
		return ErrHistory
	}
	dir, err := h.root.Open(".")
	if err != nil {
		return err
	}
	names, err := dir.Readdirnames(maxHistoryEvents + 2)
	dir.Close()
	if err != nil && err != io.EOF || len(names) > maxHistoryEvents+1 {
		return ErrHistory
	}
	var events []string
	for _, name := range names {
		if name == ".history.lock" {
			continue
		}
		if !strings.HasSuffix(name, ".event.enc") {
			return ErrHistory
		}
		events = append(events, name)
	}
	sort.Strings(events)
	// Exactly one valid tail can have been fsynced before the checkpoint
	// replacement. This is an acceptance reservation, never install success.
	if int64(len(events)) < head.Number || int64(len(events)) > head.Number+1 {
		return ErrHistory
	}
	previous := ""
	for i, name := range events {
		number := int64(i + 1)
		if name != eventName(number) {
			return ErrHistory
		}
		var event historyEvent
		data, err := h.decode(h.root, name, &event)
		if err != nil {
			return err
		}
		if event.NodeID != h.nodeID || event.Number != number || event.Previous != previous {
			return ErrHistory
		}
		if err := h.observeEvent(event); err != nil {
			return err
		}
		previous = eventDigest(data)
		if number == head.Number && previous != head.Digest {
			return ErrHistory
		}
	}
	if int64(len(events)) == head.Number+1 {
		if h.inspectionOnly {
			return ErrUpdatePending
		}
		head.Number++
		head.Digest = previous
		if err := h.writeHead(head); err != nil {
			return err
		}
	} else if _, err := h.anchor.Lstat(h.anchorName + ".next"); !errors.Is(err, os.ErrNotExist) {
		// A pending checkpoint without its matching event is damaged state,
		// not permission to fall back to the older recorded release.
		return ErrHistory
	}
	h.head = head
	return nil
}
func (h *History) diskHead() error {
	if err := h.held(); err != nil {
		return err
	}
	var head historyHead
	if _, err := h.decode(h.anchor, h.anchorName, &head); err != nil {
		return err
	}
	if head != h.head {
		return ErrHistory
	}
	if _, err := h.anchor.Lstat(h.anchorName + ".next"); !errors.Is(err, os.ErrNotExist) {
		return ErrHistory
	}
	if _, err := h.root.Lstat(eventName(head.Number + 1)); !errors.Is(err, os.ErrNotExist) {
		return ErrHistory
	}
	if head.Number > 0 {
		var event historyEvent
		data, err := h.decode(h.root, eventName(head.Number), &event)
		if err != nil || eventDigest(data) != head.Digest {
			return ErrHistory
		}
	}
	return nil
}
func (h *History) CheckSequence(component string, sequence int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.diskHead(); err != nil {
		return err
	}
	if !release.Component(component) || sequence < 1 {
		return ErrReplay
	}
	if old, ok := h.highest[component]; ok && sequence < old.Sequence {
		return ErrReplay
	}
	return nil
}
func (h *History) check(s release.Signed, policy release.Policy) error {
	if err := s.Verify(policy); err != nil {
		return err
	}
	m := s.Manifest
	if m.OS != runtime.GOOS || m.Architecture != runtime.GOARCH {
		return release.ErrPolicy
	}
	if old, ok := h.highest[m.Component]; ok && (m.Sequence < old.Sequence || m.Sequence == old.Sequence && !sameManifest(m, old)) {
		return ErrReplay
	}
	return nil
}
func sameManifest(a, b release.Manifest) bool { return a == b }
func (h *History) Check(s release.Signed, policy release.Policy) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.diskHead(); err != nil {
		return err
	}
	return h.check(s, policy)
}
func (h *History) Accept(ctx context.Context, s release.Signed, policy release.Policy) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := h.diskHead(); err != nil {
		return err
	}
	if err := h.check(s, policy); err != nil {
		return err
	}
	if old, ok := h.highest[s.Manifest.Component]; ok && sameManifest(old, s.Manifest) {
		return nil
	}
	return h.appendEvent(historyEvent{Kind: "release-acceptance-v1", Release: s})
}
func exclusiveRoot(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return syncHistoryDirectory(root)
}
func (h *History) writeHead(head historyHead) error {
	data, err := h.encode(head)
	if err != nil {
		return err
	}
	// One lifetime lock owns this anchor. The fixed temporary name never
	// comes from a release. Existing leftovers are not silently overwritten.
	temporary := h.anchorName + ".next"
	if _, err := h.anchor.Lstat(temporary); err == nil {
		var pending historyHead
		if _, err := h.decode(h.anchor, temporary, &pending); err != nil || pending != head {
			return ErrHistory
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := exclusiveRoot(h.anchor, temporary, data); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := h.anchor.Rename(temporary, h.anchorName); err != nil {
		return err
	}
	return syncHistoryDirectory(h.anchor)
}
