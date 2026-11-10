package tuningadvice

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const RulesVersion = "2026.11-1"

const (
	giB       = 1024 * 1024 * 1024
	cap8MiB   = 8388608
	cap16MiB  = 16777216
	cap32MiB  = 33554432
	bdpFloor  = 4194304
	quicCheck = 7864320
	quicFloor = 8388608
)

type KeyState struct {
	Present  bool
	Writable bool
}

type Inputs struct {
	MemoryBytes         uint64
	CPUCores            int
	Virtualization      string
	Keys                map[string]KeyState
	Current             map[string]string
	Sources             map[string]string
	CongestionAvailable []string
	CongestionLoadable  []string
	QdiscAvailable      []string
	QdiscLoadable       []string
	HasQUICInbound      bool
	RelayRole           bool
	MedianRTTMillis     *float64
	BandwidthMbps       *int
}

type Advice struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
	Level  string `json:"level"`
	Rule   string `json:"rule"`
}

func usable(in Inputs, key string) bool {
	st, ok := in.Keys[key]
	return ok && st.Present && st.Writable
}

func hasAlgo(avail, loadable []string, name string) bool {
	return slices.Contains(avail, name) || slices.Contains(loadable, name)
}

func currentUpperBound(raw string) (uint64, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseUint(fields[len(fields)-1], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func bufferCap(in Inputs) (uint64, string) {
	var tierCap uint64
	var tierLabel string
	switch {
	case in.MemoryBytes < giB:
		tierCap = cap8MiB
		tierLabel = "<1 GiB"
	case in.MemoryBytes < 2*giB:
		tierCap = cap16MiB
		tierLabel = "1–2 GiB"
	default:
		tierCap = cap32MiB
		tierLabel = "≥2 GiB"
	}
	capVal := tierCap
	if in.BandwidthMbps != nil && *in.BandwidthMbps > 0 && in.MedianRTTMillis != nil && *in.MedianRTTMillis > 0 && !math.IsNaN(*in.MedianRTTMillis) && !math.IsInf(*in.MedianRTTMillis, 0) {
		bdp := uint64(math.Round(float64(*in.BandwidthMbps) * 125000.0 * (*in.MedianRTTMillis / 1000.0) * 2.0))
		if bdp < bdpFloor {
			bdp = bdpFloor
		}
		if bdp < capVal {
			capVal = bdp
		}
	}
	return capVal, fmt.Sprintf("按内存 %s 与线路估算的缓冲上限", tierLabel)
}

// Recommend returns per-machine tuning recommendations sorted by Key.
// Absent/read-only keys and keys whose current value already satisfies the rule are omitted.
func Recommend(in Inputs) []Advice {
	byKey := map[string]Advice{}

	put := func(a Advice) {
		if !usable(in, a.Key) {
			return
		}
		existing, ok := byKey[a.Key]
		if !ok {
			byKey[a.Key] = a
			return
		}
		ev, err1 := strconv.ParseUint(existing.Value, 10, 64)
		nv, err2 := strconv.ParseUint(a.Value, 10, 64)
		if err1 == nil && err2 == nil && nv >= ev {
			existing.Value = a.Value
			if nv > ev {
				existing.Rule = a.Rule
			}
		}
		if a.Reason != "" && !strings.Contains(existing.Reason, a.Reason) {
			existing.Reason = existing.Reason + "；" + a.Reason
		}
		if a.Level == "suggested" {
			existing.Level = "suggested"
		}
		byKey[a.Key] = existing
	}

	// 1. cc-bbr
	if hasAlgo(in.CongestionAvailable, in.CongestionLoadable, "bbr") &&
		strings.TrimSpace(in.Current["net.ipv4.tcp_congestion_control"]) != "bbr" {
		put(Advice{
			Key:    "net.ipv4.tcp_congestion_control",
			Value:  "bbr",
			Reason: "BBR 在长距离和有丢包的线路上吞吐更稳定",
			Level:  "suggested",
			Rule:   "cc-bbr",
		})
	}

	// 2. qdisc-fq
	if hasAlgo(in.QdiscAvailable, in.QdiscLoadable, "fq") &&
		strings.TrimSpace(in.Current["net.core.default_qdisc"]) != "fq" {
		put(Advice{
			Key:    "net.core.default_qdisc",
			Value:  "fq",
			Reason: "fq 为 BBR 提供发送节奏控制",
			Level:  "suggested",
			Rule:   "qdisc-fq",
		})
	}

	// 3. idle-restart
	if strings.TrimSpace(in.Current["net.ipv4.tcp_slow_start_after_idle"]) != "0" {
		put(Advice{
			Key:    "net.ipv4.tcp_slow_start_after_idle",
			Value:  "0",
			Reason: "代理长连接空闲后不再重新慢启动",
			Level:  "suggested",
			Rule:   "idle-restart",
		})
	}

	// 4. notsent-lowat
	if usable(in, "net.ipv4.tcp_notsent_lowat") {
		raw := strings.TrimSpace(in.Current["net.ipv4.tcp_notsent_lowat"])
		adviseLowat := false
		if raw == "" || raw == "-1" {
			adviseLowat = true
		} else if v, err := strconv.ParseUint(raw, 10, 64); err == nil && v > 131072 {
			adviseLowat = true
		}
		if adviseLowat {
			put(Advice{
				Key:    "net.ipv4.tcp_notsent_lowat",
				Value:  "131072",
				Reason: "减少发送端排队，降低交互延迟",
				Level:  "suggested",
				Rule:   "notsent-lowat",
			})
		}
	}

	// 5. buffer-cap
	capVal, capReason := bufferCap(in)
	capStr := strconv.FormatUint(capVal, 10)
	for _, key := range []string{
		"net.ipv4.tcp_rmem",
		"net.ipv4.tcp_wmem",
		"net.core.rmem_max",
		"net.core.wmem_max",
	} {
		if !usable(in, key) {
			continue
		}
		if cur, ok := currentUpperBound(in.Current[key]); !ok || cur < capVal {
			put(Advice{
				Key:    key,
				Value:  capStr,
				Reason: capReason,
				Level:  "suggested",
				Rule:   "buffer-cap",
			})
		}
	}

	// 6. quic-buffer
	if in.HasQUICInbound {
		curRmem, hasRmem := currentUpperBound(in.Current["net.core.rmem_max"])
		if !hasRmem || curRmem < quicCheck {
			target := capVal
			if target < quicFloor {
				target = quicFloor
			}
			targetStr := strconv.FormatUint(target, 10)
			for _, key := range []string{"net.core.rmem_max", "net.core.wmem_max"} {
				if !usable(in, key) {
					continue
				}
				if cur, ok := currentUpperBound(in.Current[key]); !ok || cur < target {
					put(Advice{
						Key:    key,
						Value:  targetStr,
						Reason: "QUIC（Hysteria2/TUIC）需要约 7 MB 以上的 UDP 缓冲",
						Level:  "suggested",
						Rule:   "quic-buffer",
					})
				}
			}
		}
	}

	// 7. mtu-probing
	if in.RelayRole && strings.TrimSpace(in.Current["net.ipv4.tcp_mtu_probing"]) == "0" {
		put(Advice{
			Key:    "net.ipv4.tcp_mtu_probing",
			Value:  "1",
			Reason: "中转线路上避开路径 MTU 黑洞",
			Level:  "optional",
			Rule:   "mtu-probing",
		})
	}

	out := make([]Advice, 0, len(byKey))
	for _, a := range byKey {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}
