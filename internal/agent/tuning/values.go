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
