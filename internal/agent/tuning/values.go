// Package tuning implements the fixed, whitelisted sysctl helper protocol.
package tuning

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tuningcontract "proxycontrol/pkg/contracts/tuning"
)

const Directory = "/var/lib/proxy-agent/tuning"
const ConfigName = "99-proxy-tuning.conf"

func Keys() []string                  { return tuningcontract.Keys() }
func sysPath(root, key string) string { return filepath.Join(root, strings.ReplaceAll(key, ".", "/")) }
func vector(key string) bool          { return tuningcontract.Vector(key) }
func contains(list, value string) bool {
	for _, v := range strings.Fields(list) {
		if v == value {
			return true
		}
	}
	return false
}

func moduleRoots(root string) []string {
	roots := []string{root}
	if filepath.Base(root) == "sys" && filepath.Base(filepath.Dir(root)) == "proc" {
		roots = append(roots, filepath.Clean(filepath.Join(root, "../..")))
	}
	return roots
}

func hasKernelModule(root, subdir, modName string) (moduleTreePresent bool, found bool) {
	var kernel string
	if raw, err := os.ReadFile(sysPath(root, "kernel.osrelease")); err == nil {
		kernel = strings.TrimSpace(string(raw))
	}
	for _, base := range moduleRoots(root) {
		if raw, err := os.ReadFile(filepath.Join(base, "proc/modules")); err == nil {
			moduleTreePresent = true
			for _, line := range strings.Split(string(raw), "\n") {
				if fields := strings.Fields(line); len(fields) > 0 && fields[0] == modName {
					return true, true
				}
			}
		}
		var modDirs []string
		if kernel != "" {
			d := filepath.Join(base, "lib/modules", kernel)
			if info, err := os.Stat(d); err == nil && info.IsDir() {
				modDirs = append(modDirs, d)
			}
		} else if matches, err := filepath.Glob(filepath.Join(base, "lib/modules/*")); err == nil {
			modDirs = append(modDirs, matches...)
		}
		for _, modDir := range modDirs {
			moduleTreePresent = true
			if raw, err := os.ReadFile(filepath.Join(modDir, "modules.builtin")); err == nil {
				for _, line := range strings.Split(string(raw), "\n") {
					b := filepath.Base(strings.TrimSpace(line))
					name, _, _ := strings.Cut(b, ".ko")
					if name == modName {
						return true, true
					}
				}
			}
			pattern := filepath.Join(modDir, "kernel", subdir, modName+".ko*")
			if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
				return true, true
			}
		}
	}
	return moduleTreePresent, false
}

func Validate(settings map[string]string, root string) error {
	for k, v := range settings {
		if err := tuningcontract.ValidateValue(k, v); err != nil {
			return err
		}
		if k == "net.ipv4.tcp_congestion_control" {
			raw, err := os.ReadFile(sysPath(root, "net.ipv4.tcp_available_congestion_control"))
			if err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
			if !contains(string(raw), v) {
				if _, ok := hasKernelModule(root, "net/ipv4", "tcp_"+v); !ok {
					return fmt.Errorf("%s: algorithm unavailable", k)
				}
			}
		}
		if k == "net.core.default_qdisc" && v != "pfifo_fast" {
			if present, ok := hasKernelModule(root, "net/sched", "sch_"+v); present && !ok {
				return fmt.Errorf("%s: algorithm unavailable", k)
			}
		}
	}
	return nil
}
func Current(root string) map[string]string {
	values := map[string]string{}
	for _, k := range Keys() {
		if raw, err := os.ReadFile(sysPath(root, k)); err == nil {
			if v := strings.Join(strings.Fields(string(raw)), " "); v != "" {
				values[k] = v
			}
		}
	}
	return values
}
func Supported() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, paths := range [][]string{{"/etc/systemd/system/proxy-agent-tuning.service", "/etc/systemd/system/proxy-agent-tuning.path"}, {"/etc/init.d/proxy-agent-tuning", "/usr/local/lib/proxy-agent/tuning-watch"}} {
		good := true
		for _, p := range paths {
			info, err := os.Stat(p)
			if err != nil || !info.Mode().IsRegular() {
				good = false
			}
		}
		if good {
			return true
		}
	}
	return false
}

type Desired struct {
	RequestID string            `json:"request_id"`
	Settings  map[string]string `json:"settings"`
	Clear     bool              `json:"clear"`
}

func (d Desired) Validate(root string) error {
	if d.RequestID == "" || len(d.RequestID) > 128 || strings.ContainsAny(d.RequestID, "\r\n\x00") {
		return errors.New("invalid request id")
	}
	if d.Clear && len(d.Settings) > 0 {
		return errors.New("clear must have no settings")
	}
	return Validate(d.Settings, root)
}
