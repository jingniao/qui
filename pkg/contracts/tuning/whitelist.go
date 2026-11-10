// Package tuning defines the shared sysctl whitelist and value bounds used by
// the panel, Agent, privileged helper, and backup archive validator.
package tuning

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
)

type Bound struct {
	Min, Max uint64
}

var Bounds = map[string]Bound{
	"net.ipv4.tcp_ecn":                   {Min: 0, Max: 2},
	"fs.file-max":                        {Min: 65536, Max: 16777216},
	"net.ipv4.tcp_rmem":                  {Min: 4194304, Max: 268435456},
	"net.ipv4.tcp_wmem":                  {Min: 4194304, Max: 268435456},
	"net.ipv4.tcp_tw_reuse":              {Min: 0, Max: 2},
	"net.core.somaxconn":                 {Min: 128, Max: 65535},
	"net.ipv4.tcp_max_syn_backlog":       {Min: 128, Max: 262144},
	"net.core.rmem_max":                  {Min: 212992, Max: 268435456},
	"net.core.wmem_max":                  {Min: 212992, Max: 268435456},
	"net.ipv4.tcp_fastopen":              {Min: 0, Max: 3},
	"net.ipv4.tcp_mtu_probing":           {Min: 0, Max: 2},
	"net.ipv4.tcp_slow_start_after_idle": {Min: 0, Max: 1},
	"net.ipv4.tcp_notsent_lowat":         {Min: 16384, Max: 16777216},
	"net.core.rmem_default":              {Min: 212992, Max: 268435456},
	"net.core.wmem_default":              {Min: 212992, Max: 268435456},
	"net.ipv4.udp_rmem_min":              {Min: 4096, Max: 1048576},
	"net.ipv4.udp_wmem_min":              {Min: 4096, Max: 1048576},
}

var Choices = map[string][]string{
	"net.ipv4.tcp_congestion_control": {"bbr", "cubic", "reno", "htcp", "hybla", "bbr2", "bbr3"},
	"net.core.default_qdisc":          {"fq", "fq_codel", "cake", "pfifo_fast"},
}

func Keys() []string {
	keys := make([]string, 0, len(Bounds)+len(Choices))
	for k := range Bounds {
		keys = append(keys, k)
	}
	for k := range Choices {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func Known(key string) bool {
	if _, ok := Bounds[key]; ok {
		return true
	}
	_, ok := Choices[key]
	return ok
}

func Vector(key string) bool {
	return key == "net.ipv4.tcp_rmem" || key == "net.ipv4.tcp_wmem"
}

func ValidateValue(key, value string) error {
	if list, ok := Choices[key]; ok {
		if !slices.Contains(list, value) {
			return fmt.Errorf("%s: unsupported value", key)
		}
		return nil
	}
	b, ok := Bounds[key]
	if !ok {
		return fmt.Errorf("unknown tuning key %q", key)
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != value || n < b.Min || n > b.Max {
		return fmt.Errorf("%s: value out of bounds", key)
	}
	return nil
}
