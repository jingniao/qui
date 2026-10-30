// Package tuning implements the fixed, whitelisted sysctl helper protocol.
package tuning

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const Directory = "/var/lib/proxy-agent/tuning"
const ConfigName = "99-proxy-tuning.conf"

type bound struct{ min, max uint64 }

var bounds = map[string]bound{
	"net.ipv4.tcp_ecn": {0, 2}, "fs.file-max": {65536, 16777216},
	"net.ipv4.tcp_rmem": {4194304, 268435456}, "net.ipv4.tcp_wmem": {4194304, 268435456},
	"net.ipv4.tcp_tw_reuse": {0, 2}, "net.core.somaxconn": {128, 65535},
	"net.ipv4.tcp_max_syn_backlog": {128, 262144}, "net.core.rmem_max": {212992, 268435456},
	"net.core.wmem_max": {212992, 268435456}, "net.ipv4.tcp_fastopen": {0, 3}, "net.ipv4.tcp_mtu_probing": {0, 2},
}
var names = map[string]string{
	"net.ipv4.tcp_congestion_control": "bbr cubic reno htcp hybla bbr2 bbr3",
	"net.core.default_qdisc":          "fq fq_codel cake pfifo_fast",
}

func Keys() []string {
	keys := make([]string, 0, len(bounds)+len(names))
	for k := range bounds {
		keys = append(keys, k)
	}
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func sysPath(root, key string) string { return filepath.Join(root, strings.ReplaceAll(key, ".", "/")) }
func vector(key string) bool          { return key == "net.ipv4.tcp_rmem" || key == "net.ipv4.tcp_wmem" }
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
		if choices, ok := names[k]; ok {
			if !contains(choices, v) {
				return fmt.Errorf("%s: unsupported value", k)
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
			continue
		}
		b, ok := bounds[k]
		if !ok {
			return fmt.Errorf("unknown tuning key %q", k)
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || strconv.FormatUint(n, 10) != v || n < b.min || n > b.max {
			return fmt.Errorf("%s: value out of bounds", k)
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
