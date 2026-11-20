package tuning

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	contract "proxycontrol/pkg/contracts/tuning"
)

//go:embed ubuntu_modules.py
var ubuntuModules string

const moduleRequestName = "module-request.json"
const moduleStateDir = "/var/lib/qui-tuning-modules"

type moduleRequest struct {
	ID      string   `json:"id"`
	Boot    string   `json:"boot"`
	Release string   `json:"release"`
	Targets []string `json:"targets"`
}

type moduleReceipt struct {
	moduleRequest
	Finished bool     `json:"finished"`
	Packages []string `json:"packages,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func moduleScript(args ...string) *exec.Cmd {
	cmd := exec.Command("/usr/bin/python3", append([]string{"-I", "-B", "-c", ubuntuModules}, args...)...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "HOME=/root", "SYSTEMD_OFFLINE=1", "NEEDRESTART_MODE=l"}
	return cmd
}

func probeModuleHost() error {
	for _, name := range []string{"proxy-agent-tuning-modules.service", "proxy-agent-tuning-modules.path"} {
		if _, err := os.Stat(filepath.Join("/etc/systemd/system", name)); err != nil {
			return fmt.Errorf("仅 Ubuntu/systemd 固定补装 helper 可用：%w", err)
		}
	}
	output, err := moduleScript("probe").CombinedOutput()
	if err != nil {
		return fmt.Errorf("模块补装环境：%s：%w", lastDiagnostic(output), err)
	}
	return nil
}

func lastDiagnostic(output []byte) string {
	message := strings.TrimSpace(string(output))
	if i := strings.LastIndexByte(message, '\n'); i >= 0 {
		message = message[i+1:]
	}
	if len(message) > 650 {
		message = message[len(message)-650:]
		for len(message) > 0 && !utf8.RuneStart(message[0]) {
			message = message[1:]
		}
	}
	return message
}

// Validate all unrelated settings before creating a package request. A missing
// available-congestion-control file is a probe failure, not a missing module.
func (d Desired) missingModules(proc string) ([]string, error) {
	if err := d.validateEnvelope(); err != nil {
		return nil, err
	}
	if !contract.HasModuleTarget(d.Settings) {
		return nil, errors.New("补装只适用于 bbr 或 fq")
	}
	var missing []string
	for key, value := range d.Settings {
		switch {
		case key == "net.ipv4.tcp_congestion_control" && value == "bbr":
			available, err := os.ReadFile(sysPath(proc, "net.ipv4.tcp_available_congestion_control"))
			if err != nil {
				return nil, err
			}
			if _, found := hasKernelModule(proc, "net/ipv4", "tcp_bbr"); !found && !contains(string(available), "bbr") {
				missing = append(missing, "bbr")
			}
		case key == "net.core.default_qdisc" && value == "fq":
			current, err := os.ReadFile(sysPath(proc, key))
			if err != nil {
				return nil, err
			}
			if _, found := hasKernelModule(proc, "net/sched", "sch_fq"); !found && strings.TrimSpace(string(current)) != "fq" {
				missing = append(missing, "fq")
			}
		default:
			if err := Validate(map[string]string{key: value}, proc); err != nil {
				return nil, err
			}
		}
	}
	slices.Sort(missing)
	return missing, nil
}

func moduleRequestFor(id string, targets []string, proc string) (moduleRequest, error) {
	boot, err := bootID(proc)
	if err != nil {
		return moduleRequest{}, err
	}
	raw, err := os.ReadFile(sysPath(proc, "kernel.osrelease"))
	if err != nil {
		return moduleRequest{}, err
	}
	return moduleRequest{ID: id, Boot: boot, Release: strings.TrimSpace(string(raw)), Targets: targets}, nil
}

func readModuleReceipt(dir string) (moduleReceipt, error) {
	var result moduleReceipt
	raw, err := os.ReadFile(filepath.Join(dir, "status.json"))
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}

func (r moduleRequest) matches(other moduleRequest) bool {
	return r.ID == other.ID && r.Boot == other.Boot && r.Release == other.Release && slices.Equal(r.Targets, other.Targets)
}

func PrepareModules(ctx context.Context, desired Desired, directory, proc, hostRoot string, install bool) ([]string, error) {
	state := filepath.Join(hostRoot, "var/lib/qui-tuning-modules")
	prev, readErr := readModuleReceipt(state)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	if !install && (prev.ID == "" || prev.Finished) {
		return nil, nil
	}
	r, err := moduleRequestFor(desired.RequestID, nil, proc)
	if err != nil {
		return nil, err
	}
	// One Ubuntu archive can supply both algorithms. Files may appear during
	// unpack, before the previous request finishes configuring that archive.
	if prev.ID != "" && (prev.ID != r.ID || !install) && !prev.Finished && prev.Boot == r.Boot && prev.Release == r.Release {
		if _, err := waitModuleReceipt(ctx, state, prev.moduleRequest); err != nil {
			return nil, err
		}
	}
	if !install {
		return nil, nil
	}
	missing, err := desired.missingModules(proc)
	if err != nil {
		return nil, err
	}
	r.Targets = missing
	if prev.ID == r.ID && prev.Boot == r.Boot && prev.Release == r.Release {
		r.Targets = prev.Targets
	} else if len(missing) == 0 {
		return nil, nil
	}
	if err = ModuleSupport(directory, proc, hostRoot); err != nil {
		return nil, err
	}
	if !r.matches(prev.moduleRequest) {
		if err := writeJSON(filepath.Join(directory, moduleRequestName), r, 0600); err != nil {
			return nil, err
		}
	}
	res, err := waitModuleReceipt(ctx, state, r)
	if err != nil {
		return res.Packages, err
	}
	check, err := moduleRequestFor(r.ID, r.Targets, proc)
	if err != nil {
		return res.Packages, err
	}
	if !check.matches(r) {
		return res.Packages, errors.New("补装后运行内核已变更")
	}
	if absent, err := desired.missingModules(proc); err != nil || len(absent) != 0 {
		return res.Packages, fmt.Errorf("补装后复核失败：缺少 %v：%v", absent, err)
	}
	return res.Packages, nil
}

func waitModuleReceipt(ctx context.Context, state string, r moduleRequest) (moduleReceipt, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		res, err := readModuleReceipt(state)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return moduleReceipt{}, err
		}
		if r.matches(res.moduleRequest) && res.Finished {
			if res.Error != "" {
				return res, fmt.Errorf("模块包请求 %s：%s", r.ID, res.Error)
			}
			return res, nil
		}
		select {
		case <-ctx.Done():
			return moduleReceipt{}, fmt.Errorf("tuning status unconfirmed: 模块包回执 %s：%w", r.ID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func RunModuleHelper() error {
	if os.Getenv("INVOCATION_ID") == "" {
		return errors.New("模块包任务须由专用 systemd 单元执行")
	}
	dir, err := requestRoot(Directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	f, err := openRequest(dir, moduleRequestName)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 2048 {
		return errors.New("无效模块请求文件")
	}
	data, err := io.ReadAll(io.LimitReader(f, 2049))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var req moduleRequest
	if err = dec.Decode(&req); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF || len(req.Targets) == 0 || len(req.Targets) > 2 || (Desired{RequestID: req.ID}).validateEnvelope() != nil {
		return errors.New("无效固定模块请求")
	}
	settings := map[string]string{}
	for _, target := range req.Targets {
		switch target {
		case "bbr":
			settings["net.ipv4.tcp_congestion_control"] = "bbr"
		case "fq":
			settings["net.core.default_qdisc"] = "fq"
		default:
			return errors.New("未知模块目标")
		}
	}
	if len(settings) != len(req.Targets) {
		return errors.New("重复模块目标")
	}
	return executeModuleRequest(req, settings, moduleStateDir, "/proc/sys", func(targets []string) ([]string, error) {
		keys, err := ReadProbe(Directory, "/proc/sys", "/")
		if err != nil {
			return nil, err
		}
		if err = contract.CheckProbe(settings, "ready", keys); err != nil {
			return nil, err
		}
		cmd := moduleScript(targets...)
		var packages, diag bytes.Buffer
		cmd.Stdout = &packages
		cmd.Stderr = io.MultiWriter(os.Stderr, &diag)
		err = cmd.Run()
		if err != nil {
			err = fmt.Errorf("Ubuntu 模块包 %s %v：%w：%s", req.Release, targets, err, lastDiagnostic(diag.Bytes()))
		}
		return strings.Fields(packages.String()), err
	})
}

func executeModuleRequest(req moduleRequest, settings map[string]string, state, proc string, install func([]string) ([]string, error)) error {
	actual, err := moduleRequestFor(req.ID, req.Targets, proc)
	if err != nil {
		return err
	}
	if !req.matches(actual) {
		return errors.New("拒绝此前启动或内核的模块请求")
	}
	previous, err := readModuleReceipt(state)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	result := moduleReceipt{moduleRequest: req}
	if req.matches(previous.moduleRequest) {
		if previous.Finished {
			return nil
		}
		result.Error = "此前包操作中断，配置状态未确认；请检查 dpkg --audit"
	} else {
		if err = writeJSON(filepath.Join(state, "status.json"), result, 0644); err != nil {
			return err
		}
		missing, checkErr := (Desired{RequestID: req.ID, Settings: settings}).missingModules(proc)
		if checkErr == nil && len(missing) > 0 {
			result.Packages, checkErr = install(missing)
		}
		if checkErr != nil {
			result.Error = checkErr.Error()
		}
	}
	result.Finished = true
	if err = writeJSON(filepath.Join(state, "status.json"), result, 0644); err != nil {
		return err
	}
	if result.Error != "" {
		slog.Error("module package operation failed", "request", req.ID, "error", result.Error)
		return errors.New(result.Error)
	}
	return nil
}
