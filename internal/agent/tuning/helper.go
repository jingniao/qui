package tuning

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"proxycontrol/internal/platform/vault"
	pb "proxycontrol/pkg/contracts/agentpb"
)

// The baseline is root-owned, outside the Agent-writable request directory.
const snapshotName = ".proxy-tuning-snapshot.json"

type snapshot struct {
	Before map[string]string `json:"before"`
	Active map[string]string `json:"active"`
}
type Helper struct {
	ProcRoot, ConfigDir string
	writeOverride       func(string, string) error
}

func atomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".tuning-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return vault.SyncDir(filepath.Dir(path))
}
func writeJSON(path string, v any, mode os.FileMode) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return atomic(path, append(raw, '\n'), mode)
}
func WriteDesired(directory string, d Desired) error {
	return writeJSON(filepath.Join(directory, "desired.json"), d, 0600)
}
func ReadStatus(directory string) (*pb.TuningResult, error) {
	raw, err := os.ReadFile(filepath.Join(directory, "status.json"))
	if err != nil {
		return nil, err
	}
	r := &pb.TuningResult{}
	err = json.Unmarshal(raw, r)
	return r, err
}
func readDesired(directory string) (Desired, error) {
	root, err := requestRoot(directory)
	if err != nil {
		return Desired{}, err
	}
	defer root.Close()
	return readDesiredRoot(root)
}

// Pin the Agent-controlled directory for this invocation. Root's traversal
// protection prevents a symlink/rename race escaping into another host path.
func requestRoot(directory string) (*os.Root, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("tuning directory must be a real directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	f, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	opened, err := f.Stat()
	f.Close()
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, errors.New("tuning directory changed while opening")
	}
	return root, nil
}

func readDesiredRoot(root *os.Root) (Desired, error) {
	f, err := openDesired(root)
	if err != nil {
		return Desired{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Desired{}, errors.New("desired file must be regular")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return Desired{}, err
	}
	if len(raw) > 16384 {
		return Desired{}, errors.New("desired file exceeds 16 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d Desired
	if err = dec.Decode(&d); err != nil {
		return d, err
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return d, errors.New("trailing desired input")
	}
	return d, nil
}
func (h Helper) read(key string) (string, error) {
	raw, err := os.ReadFile(sysPath(h.ProcRoot, key))
	return strings.Join(strings.Fields(string(raw)), " "), err
}
func (h Helper) write(key, value string) error {
	if h.writeOverride != nil {
		return h.writeOverride(key, value)
	}
	// Never create a missing sysctl. Procfs writes must be one complete write.
	f, err := os.OpenFile(sysPath(h.ProcRoot, key), os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = f.WriteString(value + "\n")
	return errors.Join(err, f.Close())
}
func safeOriginal(key, value string) bool {
	if _, ok := names[key]; ok {
		return value != "" && len(value) <= 64 && len(strings.Fields(value)) == 1 && !strings.ContainsAny(value, "\n\r=\x00")
	}
	if _, ok := bounds[key]; !ok {
		return false
	}
	fields := strings.Fields(value)
	want := 1
	if vector(key) {
		want = 3
	}
	if len(fields) != want {
		return false
	}
	for _, f := range fields {
		if _, err := strconvUint(f); err != nil {
			return false
		}
	}
	return true
}
func strconvUint(v string) (uint64, error) {
	var n uint64
	if v == "" {
		return 0, errors.New("empty integer")
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid integer")
		}
	}
	// JSON's uint64 parser also rejects overflow, without accepting shell syntax.
	err := json.Unmarshal([]byte(v), &n)
	return n, err
}

// Apply is transactional for ordinary failures. Rollback errors are explicitly
// returned; a read-only host/container must never be reported as successful.
func (h Helper) Apply(d Desired) *pb.TuningResult {
	r := &pb.TuningResult{RequestId: d.RequestID, Status: "failed"}
	fail := func(err error) *pb.TuningResult {
		keys := make([]string, 0, len(d.Settings))
		for key := range d.Settings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			r.Keys = append(r.Keys, &pb.TuningKeyResult{Key: key, Error: err.Error()})
		}
		if len(keys) == 0 {
			r.Keys = append(r.Keys, &pb.TuningKeyResult{Error: err.Error()})
		}
		return r
	}
	if err := d.Validate(h.ProcRoot); err != nil {
		return fail(err)
	}
	old := snapshot{Before: map[string]string{}, Active: map[string]string{}}
	snapshotPath := filepath.Join(h.ConfigDir, snapshotName)
	if raw, err := os.ReadFile(snapshotPath); err == nil {
		if err = json.Unmarshal(raw, &old); err != nil || old.Before == nil || old.Active == nil {
			return fail(errors.New("invalid tuning snapshot"))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	} else if len(d.Settings) > 0 {
		// The first baseline covers every readable whitelist key, so a later
		// profile and eventual clear still refer to the same original host state.
		old.Before = Current(h.ProcRoot)
	}
	for k, v := range old.Before {
		if !safeOriginal(k, v) {
			return fail(errors.New("invalid snapshot value"))
		}
	}
	targets := map[string]string{}
	if d.Clear || len(d.Settings) == 0 {
		for k, v := range old.Before {
			targets[k] = v
		}
	} else {
		for k := range old.Active {
			v, ok := old.Before[k]
			if !ok {
				return fail(errors.New("missing snapshot key"))
			}
			targets[k] = v
		}
	}
	if !d.Clear {
		for k, v := range d.Settings {
			current, err := h.read(k)
			if err != nil {
				return fail(fmt.Errorf("%s: %w", k, err))
			}
			if !safeOriginal(k, current) {
				return fail(fmt.Errorf("%s: invalid current value", k))
			}
			if _, ok := old.Before[k]; !ok {
				old.Before[k] = current
			}
			if vector(k) {
				f := strings.Fields(current)
				v = f[0] + " " + f[1] + " " + v
			}
			targets[k] = v
		}
	}
	keys := make([]string, 0, len(targets))
	for _, k := range Keys() {
		if _, ok := targets[k]; ok {
			keys = append(keys, k)
		}
	}
	before := map[string]string{}
	for _, k := range keys {
		v, err := h.read(k)
		if err != nil {
			return fail(fmt.Errorf("%s: %w", k, err))
		}
		before[k] = v
	}
	// Save the original values durably BEFORE changing /proc. Failed applies may
	// retain extra baseline entries, but do not change Active or persistence.
	if len(targets) > 0 {
		if err := writeJSON(snapshotPath, old, 0600); err != nil {
			return fail(err)
		}
	}
	attempted := []string{}
	rollback := func(cause error) *pb.TuningResult {
		for i := len(attempted) - 1; i >= 0; i-- {
			k := attempted[i]
			if current, err := h.read(k); err == nil && current == before[k] {
				continue
			}
			if err := h.write(k, before[k]); err != nil {
				cause = errors.Join(cause, fmt.Errorf("restore %s: %w", k, err))
			}
			if current, err := h.read(k); err != nil || current != before[k] {
				cause = errors.Join(cause, fmt.Errorf("restore %s: read-back differs from previous value", k), err)
			}
		}
		r.Keys = nil
		for _, k := range keys {
			v, err := h.read(k)
			r.Keys = append(r.Keys, &pb.TuningKeyResult{Key: k, Value: v, Error: errors.Join(cause, err).Error()})
		}
		return r
	}
	for _, k := range keys {
		// An unchanged read-only knob need not be rewritten. This also lets a
		// container clear a profile without touching unrelated host-only sysctls.
		if before[k] == targets[k] {
			continue
		}
		attempted = append(attempted, k)
		if err := h.write(k, targets[k]); err != nil {
			return rollback(fmt.Errorf("%s: %w", k, err))
		}
		got, err := h.read(k)
		if err != nil || got != targets[k] {
			return rollback(fmt.Errorf("%s: read-back mismatch: %w", k, errors.Join(err, errors.New("value differs"))))
		}
	}
	configPath := filepath.Join(h.ConfigDir, ConfigName)
	previousConfig, configErr := os.ReadFile(configPath)
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		return rollback(configErr)
	}
	active := map[string]string{}
	var config bytes.Buffer
	config.WriteString("# Managed by proxy-agent tuning helper.\n")
	for _, k := range Keys() {
		if _, ok := d.Settings[k]; ok && !d.Clear {
			active[k] = targets[k]
			fmt.Fprintf(&config, "%s = %s\n", k, targets[k])
		}
	}
	var err error
	if len(active) == 0 {
		err = os.Remove(configPath)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	} else {
		err = atomic(configPath, config.Bytes(), 0644)
	}
	if err != nil {
		return rollback(err)
	}
	old.Active = active
	if len(active) == 0 {
		err = os.Remove(snapshotPath)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	} else {
		err = writeJSON(snapshotPath, old, 0600)
	}
	if err != nil {
		if configErr == nil {
			err = errors.Join(err, atomic(configPath, previousConfig, 0644))
		} else {
			err = errors.Join(err, os.Remove(configPath))
		}
		return rollback(err)
	}
	for _, k := range keys {
		v, _ := h.read(k)
		r.Keys = append(r.Keys, &pb.TuningKeyResult{Key: k, Value: v, Applied: true})
	}
	r.Status = "applied"
	if len(active) == 0 {
		r.Status = "cleared"
	}
	return r
}

func RunHelper(directory string) error {
	root, err := requestRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	h := Helper{ProcRoot: "/proc/sys", ConfigDir: "/etc/sysctl.d"}
	unlock, err := lock(filepath.Join(h.ConfigDir, ".proxy-tuning.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	d, err := readDesiredRoot(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var r *pb.TuningResult
	if err != nil {
		r = &pb.TuningResult{RequestId: d.RequestID, Status: "failed", Keys: []*pb.TuningKeyResult{{Error: err.Error()}}}
	} else {
		r = h.Apply(d)
	}
	// No secrets: status must be readable by the unprivileged Agent. The parent
	// directory is 0700 and owned by that Agent; the root snapshot stays private.
	if err := writeStatusRoot(root, r); err != nil {
		return err
	}
	if r.Status == "failed" {
		return errors.New("tuning apply failed; see status.json")
	}
	return nil
}

func writeStatusRoot(root *os.Root, result *pb.TuningResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	name := ".status-" + rand.Text()
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if err = f.Chmod(0644); err == nil {
		_, err = f.Write(append(raw, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = root.Rename(name, "status.json"); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
