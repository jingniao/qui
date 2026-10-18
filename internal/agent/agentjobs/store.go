// Package agentjobs binds fixed Agent requests and immutable results in an
// encrypted, hash-chained local journal with an independent identity checkpoint.
package agentjobs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"proxycontrol/internal/platform/filelock"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/pkg/contracts/release"
)

var ErrState = errors.New("Agent update journal requires original authenticated records")
var ErrBusy = errors.New("another Agent update request is pending")

const maxEvents = 10000
const maxRecord = 48 << 10

type Options struct {
	Directory, Checkpoint, NodeID string
	Key                           []byte
}
type head struct {
	Kind   string `json:"kind"`
	NodeID string `json:"node_id"`
	Number int    `json:"number"`
	Digest string `json:"digest"`
}
type event struct {
	Kind     string                     `json:"kind"`
	NodeID   string                     `json:"node_id"`
	Number   int                        `json:"number"`
	Previous string                     `json:"previous"`
	Request  release.AgentUpdateRequest `json:"request"`
	Result   *release.UpdateResult      `json:"result,omitempty"`
}
type Job struct {
	Request release.AgentUpdateRequest
	Result  *release.UpdateResult
	Started bool
}
type Store struct {
	inspectionOnly       bool
	root, anchor         *os.Root
	rootLock, anchorLock *filelock.Lock
	key                  *vault.Vault
	anchorName, nodeID   string
	head                 head
	jobs                 map[string]Job
	order                []string
}

func paths(o Options) (string, string, error) {
	if o.Directory == "" || o.Checkpoint == "" || o.NodeID == "" || len(o.NodeID) > 128 || len(o.Key) != 32 {
		return "", "", ErrState
	}
	dir, err := filepath.Abs(o.Directory)
	if err != nil {
		return "", "", err
	}
	checkpoint, err := filepath.Abs(o.Checkpoint)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(dir, checkpoint)
	if err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", ErrState
	}
	return dir, checkpoint, nil
}

func Initialize(o Options) error {
	dir, checkpoint, err := paths(o)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(checkpoint); !errors.Is(err, os.ErrNotExist) {
		return ErrState
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return err
	}
	s, err := handles(o, dir, checkpoint)
	if err != nil {
		return err
	}
	defer s.Close()
	sealed, err := s.seal(head{Kind: "agent-jobs-head-v1", NodeID: o.NodeID})
	if err != nil {
		return err
	}
	return exclusive(s.anchor, s.anchorName, sealed)
}

func Open(o Options) (*Store, error) {
	dir, checkpoint, err := paths(o)
	if err != nil {
		return nil, err
	}
	s, err := handles(o, dir, checkpoint)
	if err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func handles(o Options, dir, checkpoint string) (*Store, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrState
	}
	s := &Store{anchorName: filepath.Base(checkpoint), nodeID: o.NodeID, jobs: map[string]Job{}}
	s.root, err = os.OpenRoot(dir)
	if err == nil {
		s.anchor, err = os.OpenRoot(filepath.Dir(checkpoint))
	}
	if err == nil {
		s.key, err = vault.FromKey(o.Key)
	}
	if err == nil {
		s.rootLock, err = filelock.Try(filepath.Join(dir, ".jobs.lock"))
	}
	if err == nil {
		s.anchorLock, err = filelock.Try(checkpoint + ".lock")
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	var err error
	if s.root != nil {
		err = errors.Join(err, s.root.Close())
		s.root = nil
	}
	if s.anchor != nil {
		err = errors.Join(err, s.anchor.Close())
		s.anchor = nil
	}
	if s.anchorLock != nil {
		err = errors.Join(err, s.anchorLock.Close())
	}
	if s.rootLock != nil {
		err = errors.Join(err, s.rootLock.Close())
	}
	return err
}
func eventName(n int) string    { return fmt.Sprintf("%020d.event.enc", n) }
func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (s *Store) seal(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(data) > maxRecord-1024 {
		return nil, ErrState
	}
	return s.key.Seal(data)
}

func read(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecord {
		return nil, ErrState
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || filelock.CheckSingleLink(f) != nil {
		return nil, ErrState
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRecord+1))
	if err != nil || len(data) > maxRecord {
		return nil, ErrState
	}
	return data, nil
}

func (s *Store) decode(root *os.Root, name string, value any) ([]byte, error) {
	sealed, err := read(root, name)
	if err != nil {
		return nil, err
	}
	plain, err := s.key.Open(sealed)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	d := json.NewDecoder(bytes.NewReader(plain))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(&struct{}{}) != io.EOF {
		return nil, ErrState
	}
	return sealed, nil
}

func (s *Store) validate(e event) error {
	if e.NodeID != s.nodeID || e.Request.Validate() != nil || e.Request.NodeID != s.nodeID {
		return ErrState
	}
	requestData, err := json.Marshal(e.Request)
	if err != nil || len(requestData) > release.MaxUpdateRequestBytes {
		return ErrState
	}
	job, exists := s.jobs[e.Request.ID]
	switch e.Kind {
	case "agent-job-request-v1":
		if exists || e.Result != nil {
			return ErrState
		}
		for _, prior := range s.jobs {
			if prior.Result == nil {
				return ErrBusy
			}
		}
		if e.Request.Operation == "recover" {
			original, ok := s.jobs[e.Request.OriginalID]
			if !ok || original.Request.Operation != "install" || original.Request.Release != e.Request.Release || original.Request.Revision != e.Request.Revision {
				return ErrState
			}
		}
		if err := s.recoveryReservation(e.Request); err != nil {
			return err
		}
	case "agent-job-result-v1":
		if !exists || job.Request != e.Request || job.Result != nil || e.Result == nil || release.ValidateAgentResult(e.Request, *e.Result) != nil {
			return ErrState
		}
		if (e.Result.Status == "success" || e.Result.Status == "rolled_back") && !job.Started {
			return ErrState
		}
	case "agent-job-start-v1":
		if !exists || job.Request != e.Request || job.Result != nil || job.Started || e.Result != nil {
			return ErrState
		}
	default:
		return ErrState
	}
	return nil
}

// A terminal failure is still an execution reservation. Only a new recovery
// of that original installation may proceed; history remains immutable.
func (s *Store) recoveryReservation(request release.AgentUpdateRequest) error {
	for original := range s.unresolvedRecoveries() {
		if request.Operation != "recover" || request.OriginalID != original {
			return ErrBusy
		}
	}
	return nil
}

func (s *Store) NeedsRecovery() bool { return len(s.unresolvedRecoveries()) != 0 }

func (s *Store) unresolvedRecoveries() map[string]bool {
	unresolved := map[string]bool{}
	for _, id := range s.order {
		job := s.jobs[id]
		if job.Result == nil {
			continue
		}
		original := job.Request.ID
		if job.Request.Operation == "recover" {
			original = job.Request.OriginalID
		}
		switch job.Result.Status {
		case "failed_closed":
			unresolved[original] = true
		case "success", "rolled_back":
			if job.Request.Operation == "recover" {
				delete(unresolved, original)
			}
		}
	}
	return unresolved
}
func (s *Store) observe(e event) {
	if e.Kind == "agent-job-request-v1" {
		s.order = append(s.order, e.Request.ID)
	}
	started := s.jobs[e.Request.ID].Started || e.Kind == "agent-job-start-v1"
	s.jobs[e.Request.ID] = Job{Request: e.Request, Result: e.Result, Started: started}
}
func (s *Store) load() error {
	if _, err := s.decode(s.anchor, s.anchorName, &s.head); err != nil {
		return err
	}
	if s.head.Kind != "agent-jobs-head-v1" || s.head.NodeID != s.nodeID || s.head.Number < 0 || s.head.Number > maxEvents || s.head.Number == 0 && s.head.Digest != "" {
		return ErrState
	}
	dir, err := s.root.Open(".")
	if err != nil {
		return err
	}
	names, err := dir.Readdirnames(maxEvents + 2)
	dir.Close()
	if err != nil && err != io.EOF || len(names) > maxEvents+1 {
		return ErrState
	}
	files := []string{}
	for _, name := range names {
		if name != ".jobs.lock" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	if len(files) < s.head.Number || len(files) > s.head.Number+1 {
		return ErrState
	}
	previous := ""
	for index, name := range files {
		var e event
		data, err := s.decode(s.root, name, &e)
		if err != nil || name != eventName(index+1) || e.Number != index+1 || e.Previous != previous || s.validate(e) != nil {
			return ErrState
		}
		s.observe(e)
		previous = digest(data)
		if e.Number == s.head.Number && previous != s.head.Digest {
			return ErrState
		}
	}
	if len(files) == s.head.Number+1 {
		if s.inspectionOnly {
			return ErrState
		}
		next := head{Kind: s.head.Kind, NodeID: s.nodeID, Number: len(files), Digest: previous}
		if err := s.writeHead(next); err != nil {
			return err
		}
		s.head = next
	} else if _, err := s.anchor.Lstat(s.anchorName + ".next"); !errors.Is(err, os.ErrNotExist) {
		return ErrState
	}
	return nil
}
func exclusive(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Sync(), f.Close()); err != nil {
		return err
	}
	return syncDirectory(root)
}
func (s *Store) writeHead(next head) error {
	name := s.anchorName + ".next"
	var pending head
	if _, err := s.decode(s.anchor, name, &pending); err == nil {
		if pending != next {
			return ErrState
		}
	} else if errors.Is(err, os.ErrNotExist) {
		data, err := s.seal(next)
		if err != nil {
			return err
		}
		if err := exclusive(s.anchor, name, data); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := s.anchor.Rename(name, s.anchorName); err != nil {
		return err
	}
	return syncDirectory(s.anchor)
}
func (s *Store) append(e event) error {
	if s.diskHead() != nil || s.head.Number >= maxEvents || s.validate(e) != nil {
		return ErrState
	}
	e.NodeID, e.Number, e.Previous = s.nodeID, s.head.Number+1, s.head.Digest
	data, err := s.seal(e)
	if err != nil {
		return err
	}
	if err := exclusive(s.root, eventName(e.Number), data); err != nil {
		return err
	}
	next := head{Kind: s.head.Kind, NodeID: s.nodeID, Number: e.Number, Digest: digest(data)}
	if err := s.writeHead(next); err != nil {
		return err
	}
	s.observe(e)
	s.head = next
	return nil
}
func (s *Store) Queue(request release.AgentUpdateRequest) error {
	if err := s.diskHead(); err != nil {
		return err
	}
	if job, ok := s.jobs[request.ID]; ok {
		if job.Request == request {
			return nil
		}
		return ErrState
	}
	// Reserve execution binding and the terminal event; never execute a job
	// knowing its outcome cannot fit into this bounded journal.
	if s.head.Number > maxEvents-3 {
		return ErrState
	}
	for _, job := range s.jobs {
		if job.Result == nil {
			return ErrBusy
		}
	}
	if err := s.recoveryReservation(request); err != nil {
		return err
	}
	return s.append(event{Kind: "agent-job-request-v1", NodeID: s.nodeID, Request: request})
}
func (s *Store) Finish(request release.AgentUpdateRequest, result release.UpdateResult) error {
	if err := s.diskHead(); err != nil {
		return err
	}
	if job, ok := s.jobs[request.ID]; ok && job.Result != nil {
		if job.Request == request && *job.Result == result {
			return nil
		}
		return ErrState
	}
	return s.append(event{Kind: "agent-job-result-v1", NodeID: s.nodeID, Request: request, Result: &result})
}

// MarkStarted binds this complete request before the parent stops the worker
// or begins installation. An unrelated legacy history ID cannot prove a replay.
func (s *Store) MarkStarted(request release.AgentUpdateRequest) error {
	if err := s.diskHead(); err != nil {
		return err
	}
	job, ok := s.jobs[request.ID]
	if !ok || job.Request != request || job.Result != nil {
		return ErrState
	}
	if job.Started {
		return nil
	}
	if s.head.Number >= maxEvents-1 {
		return ErrState
	}
	return s.append(event{Kind: "agent-job-start-v1", NodeID: s.nodeID, Request: request})
}
func (s *Store) Pending() *Job {
	for _, id := range s.order {
		job := s.jobs[id]
		if job.Result == nil {
			return &job
		}
	}
	return nil
}
func (s *Store) Get(id string) (Job, bool) {
	job, ok := s.jobs[id]
	if job.Result != nil {
		copy := *job.Result
		job.Result = &copy
	}
	return job, ok
}

func (s *Store) diskHead() error {
	if s.rootLock.AssertHeld() != nil || s.anchorLock.AssertHeld() != nil {
		return ErrState
	}
	var current head
	if _, err := s.decode(s.anchor, s.anchorName, &current); err != nil || current != s.head {
		return ErrState
	}
	if _, err := s.anchor.Lstat(s.anchorName + ".next"); !errors.Is(err, os.ErrNotExist) {
		return ErrState
	}
	if _, err := s.root.Lstat(eventName(current.Number + 1)); !errors.Is(err, os.ErrNotExist) {
		return ErrState
	}
	if current.Number > 0 {
		data, err := read(s.root, eventName(current.Number))
		if err != nil || digest(data) != current.Digest {
			return ErrState
		}
	}
	return nil
}

// Fingerprint is a bounded polling hint, not authorization. It includes any
// interrupted tail; callers open/validate the complete journal when it changes.
func Fingerprint(o Options) (string, error) {
	dir, checkpoint, err := paths(o)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	anchor, err := os.OpenRoot(filepath.Dir(checkpoint))
	if err != nil {
		return "", err
	}
	defer anchor.Close()
	key, err := vault.FromKey(o.Key)
	if err != nil {
		return "", err
	}
	s := &Store{root: root, anchor: anchor, key: key}
	var h head
	data, err := s.decode(anchor, filepath.Base(checkpoint), &h)
	if err != nil || h.Kind != "agent-jobs-head-v1" || h.NodeID != o.NodeID || h.Number < 0 || h.Number > maxEvents {
		return "", ErrState
	}
	parts := []string{digest(data)}
	for _, item := range []struct {
		root *os.Root
		name string
	}{{anchor, filepath.Base(checkpoint) + ".next"}, {root, eventName(h.Number + 1)}} {
		tail, err := read(item.root, item.name)
		if errors.Is(err, os.ErrNotExist) {
			parts = append(parts, "absent")
			continue
		}
		if err != nil {
			return "", err
		}
		parts = append(parts, digest(tail))
	}
	return strings.Join(parts, ":"), nil
}
