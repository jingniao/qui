package tuning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	pb "proxycontrol/pkg/contracts/agentpb"
	tuningcontract "proxycontrol/pkg/contracts/tuning"
)

const probeName = "probe.json"
const installedHelperPath = "usr/local/bin/proxy-agent"

type probeReport struct {
	ModuleHelper bool                 `json:"module_helper,omitempty"`
	ModuleError  string               `json:"module_error,omitempty"`
	BootID       string               `json:"boot_id"`
	HelperSHA256 string               `json:"helper_sha256"`
	Keys         []*pb.TuningKeyState `json:"keys"`
}

// Open the current path before consulting this cache. Install and rollback
// atomically replace the bootstrap; the worker can be a different executable.
// Key permissions and probe readiness are always re-read.
var helperDigest struct {
	sync.Mutex
	info os.FileInfo
	sum  string
}

func helperSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	helperDigest.Lock()
	defer helperDigest.Unlock()
	cached := helperDigest.info
	if cached != nil && os.SameFile(info, cached) && info.Size() == cached.Size() && info.ModTime().Equal(cached.ModTime()) {
		return helperDigest.sum, nil
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	helperDigest.info, helperDigest.sum = info, hex.EncodeToString(hash.Sum(nil))
	return helperDigest.sum, nil
}

func bootID(proc string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(proc, "kernel/random/boot_id"))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return "", errors.New("empty boot ID")
	}
	return id, nil
}

func probeKey(proc, key string) *pb.TuningKeyState {
	st := &pb.TuningKeyState{Key: key}
	path := sysPath(proc, key)
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		st.Present = true
		err = f.Close()
		st.Writable = err == nil
	}
	if err == nil {
		return st
	}
	reason := "权限未知"
	switch {
	case errors.Is(err, os.ErrNotExist):
		reason = "缺少入口"
	default:
		_, statErr := os.Stat(path)
		st.Present = statErr == nil
		switch {
		case statErr != nil:
			err = errors.Join(err, statErr)
		case errors.Is(err, syscall.EROFS):
			reason = "只读挂载"
		case errors.Is(err, os.ErrPermission):
			reason = "无写入权限"
		}
	}
	st.ProbeError = reason + "：" + err.Error()
	return st
}

func collectProbe(proc, executable string) (probeReport, error) {
	id, err := bootID(proc)
	if err != nil {
		return probeReport{}, err
	}
	digest, err := helperSHA256(executable)
	if err != nil {
		return probeReport{}, fmt.Errorf("probe helper identity: %w", err)
	}
	report := probeReport{BootID: id, HelperSHA256: digest}
	for _, key := range Keys() {
		report.Keys = append(report.Keys, probeKey(proc, key))
	}
	return report, nil
}

func writeProbe(root *os.Root) error {
	report, err := collectProbe("/proc/sys", "/proc/self/exe")
	if err == nil {
		report.ModuleHelper = true
		if err := probeModuleHost(); err != nil {
			report.ModuleError = err.Error()
		}
		err = writeReceipt(root, probeName, report)
	}
	if err != nil {
		// A previous same-boot receipt must not mask a failed new probe.
		removeErr := root.Remove(probeName)
		if !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
		return fmt.Errorf("tuning probe: %w", err)
	}
	return nil
}

func RunProbe(directory string) error {
	root, err := requestRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	return writeProbe(root)
}

func ReadProbe(directory, proc, hostRoot string) ([]*pb.TuningKeyState, error) {
	report, err := readProbeReport(directory, proc, hostRoot)
	if err != nil {
		return nil, err
	}
	return report.Keys, nil
}

func ModuleSupport(directory, proc, hostRoot string) error {
	report, err := readProbeReport(directory, proc, hostRoot)
	if err != nil {
		return err
	}
	if !report.ModuleHelper {
		return errors.New("本机旧 helper 不支持模块补装")
	}
	if report.ModuleError != "" {
		return errors.New(report.ModuleError)
	}
	return nil
}

func readProbeReport(directory, proc, hostRoot string) (*probeReport, error) {
	raw, err := os.ReadFile(filepath.Join(directory, probeName))
	if err != nil {
		return nil, err
	}
	var report probeReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("invalid tuning probe: %w", err)
	}
	id, err := bootID(proc)
	if err != nil {
		return nil, err
	}
	if report.BootID != id {
		return nil, errors.New("tuning probe boot ID mismatch")
	}
	if err := checkOpenRCProbe(hostRoot); err != nil {
		return nil, err
	}
	digest, err := helperSHA256(filepath.Join(hostRoot, installedHelperPath))
	if err != nil {
		return nil, fmt.Errorf("installed tuning helper identity: %w", err)
	}
	if report.HelperSHA256 != digest {
		return nil, errors.New("tuning probe does not match installed helper")
	}
	if len(report.Keys) != len(Keys()) {
		return nil, errors.New("incomplete tuning probe")
	}
	seen := map[string]bool{}
	for _, st := range report.Keys {
		if st == nil || !tuningcontract.Known(st.Key) || seen[st.Key] {
			return nil, errors.New("invalid tuning probe key state")
		}
		if st.Writable != (st.ProbeError == "") || (st.Writable && !st.Present) {
			return nil, fmt.Errorf("inconsistent tuning probe for %s", st.Key)
		}
		seen[st.Key] = true
	}
	return &report, nil
}

// OpenRC supervises the watcher and can respawn it without rerunning start_pre.
// The root-owned run directory is searchable by the Agent but only the watcher writes its outcome.
func checkOpenRCProbe(root string) error {
	if _, err := os.Stat(filepath.Join(root, "run/systemd/system")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "run/openrc")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(root, "run/proxy-agent-tuning/probe-finished"))
	if err != nil {
		return fmt.Errorf("OpenRC tuning probe: %w", err)
	}
	state := strings.TrimSpace(string(raw))
	if state != "ready" {
		return fmt.Errorf("OpenRC tuning probe not ready: %q", state)
	}
	return nil
}
