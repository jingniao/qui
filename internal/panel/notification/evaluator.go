package notification

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type NodeSnapshot struct {
	ID, Name, Status string
	LastHeartbeat    time.Time
	Addresses        []string
}

type MetricWindow struct {
	NodeID            string
	CPU, Memory, Disk []*float64
}

type TrafficSnapshot struct {
	NodeID                    string
	UsedBytes, AllowanceBytes *uint64
}

type Snapshot struct {
	Now     time.Time
	Nodes   []NodeSnapshot
	Metrics map[string]MetricWindow
	Traffic map[string]TrafficSnapshot
}

type Event struct {
	Kind, Severity, NodeID, Title, Body, DedupeKey, Value string
	Resolved                                              bool
}

func Evaluate(rules []Rule, snap Snapshot, states map[string]State) (events []Event, next map[string]State) {
	ruleMap := make(map[string]Rule, len(rules))
	for _, r := range rules {
		if r.Enabled {
			ruleMap[r.Kind] = r
		}
	}

	next = make(map[string]State, len(states))
	for k, v := range states {
		next[k] = v
	}

	for _, node := range snap.Nodes {
		// 1. node_offline rule
		if node.Status != "pending" && node.Status != "disabled" {
			key := "node_offline:" + node.ID
			st := next[key]
			r, ok := ruleMap["node_offline"]

			if node.Status == "offline" && ok {
				dur := time.Duration(r.DurationMinutes) * time.Minute
				offlineDur := snap.Now.Sub(node.LastHeartbeat)
				if !node.LastHeartbeat.IsZero() && offlineDur >= dur {
					cooldown := time.Duration(r.CooldownMinutes) * time.Minute
					minutes := int(offlineDur.Minutes())
					shouldSend := false
					if !st.Active {
						shouldSend = true
					} else if cooldown > 0 && snap.Now.Sub(st.LastNotified) >= cooldown {
						shouldSend = true
					}
					if shouldSend {
						events = append(events, Event{
							Kind:      "node_offline",
							Severity:  "critical",
							NodeID:    node.ID,
							Title:     fmt.Sprintf("节点「%s」离线", node.Name),
							Body:      fmt.Sprintf("已 %d 分钟没有心跳", minutes),
							DedupeKey: key,
							Resolved:  false,
						})
						st.Key = key
						st.Active = true
						if !st.Active || st.Since.IsZero() {
							st.Since = snap.Now
						}
						st.LastNotified = snap.Now
						next[key] = st
					}
				}
			} else if node.Status == "online" {
				if st.Active {
					events = append(events, Event{
						Kind:      "node_offline",
						Severity:  "resolved",
						NodeID:    node.ID,
						Title:     fmt.Sprintf("节点「%s」离线恢复", node.Name),
						Body:      "心跳已恢复正常",
						DedupeKey: key,
						Resolved:  true,
					})
					st.Active = false
					delete(next, key)
				}
			}
		}

		// 2. cpu, memory, disk rules
		if mw, ok := snap.Metrics[node.ID]; ok && node.Status != "disabled" {
			metricList := []struct {
				kind    string
				samples []*float64
			}{
				{kind: "cpu", samples: mw.CPU},
				{kind: "memory", samples: mw.Memory},
				{kind: "disk", samples: mw.Disk},
			}

			for _, m := range metricList {
				r, enabled := ruleMap[m.kind]
				if !enabled {
					continue
				}
				key := m.kind + ":" + node.ID
				st := next[key]

				// If all samples are nil or empty, neither fire nor resolve
				hasAnyNonNil := false
				for _, s := range m.samples {
					if s != nil {
						hasAnyNonNil = true
						break
					}
				}
				if !hasAnyNonNil {
					continue
				}

				winSize := r.DurationMinutes
				if winSize < 1 {
					winSize = 1
				}

				samples := m.samples
				if len(samples) > winSize {
					samples = samples[len(samples)-winSize:]
				}

				// Check if full window is >= threshold
				fullWindowHigh := len(samples) >= winSize
				for _, s := range samples {
					if s == nil || *s < r.Threshold {
						fullWindowHigh = false
						break
					}
				}

				if fullWindowHigh {
					cooldown := time.Duration(r.CooldownMinutes) * time.Minute
					shouldSend := false
					if !st.Active {
						shouldSend = true
					} else if cooldown > 0 && snap.Now.Sub(st.LastNotified) >= cooldown {
						shouldSend = true
					}
					lastVal := *samples[len(samples)-1]
					if shouldSend {
						events = append(events, Event{
							Kind:      m.kind,
							Severity:  "warning",
							NodeID:    node.ID,
							Title:     fmt.Sprintf("节点「%s」%s 持续 %d 分钟超过 %d%%", node.Name, strings.ToUpper(m.kind), r.DurationMinutes, int(r.Threshold)),
							Body:      fmt.Sprintf("当前采样值: %.1f%%", lastVal),
							DedupeKey: key,
							Value:     fmt.Sprintf("%.1f", lastVal),
							Resolved:  false,
						})
						st.Key = key
						st.Active = true
						if st.Since.IsZero() {
							st.Since = snap.Now
						}
						st.LastNotified = snap.Now
						st.Value = fmt.Sprintf("%.1f", lastVal)
						next[key] = st
					}
				} else {
					// Check if latest non-nil sample dropped below threshold -> resolve
					var latestNonNil *float64
					for i := len(m.samples) - 1; i >= 0; i-- {
						if m.samples[i] != nil {
							latestNonNil = m.samples[i]
							break
						}
					}
					if latestNonNil != nil && *latestNonNil < r.Threshold && st.Active {
						events = append(events, Event{
							Kind:      m.kind,
							Severity:  "resolved",
							NodeID:    node.ID,
							Title:     fmt.Sprintf("节点「%s」%s 告警恢复", node.Name, strings.ToUpper(m.kind)),
							Body:      fmt.Sprintf("当前采样值: %.1f%%", *latestNonNil),
							DedupeKey: key,
							Value:     fmt.Sprintf("%.1f", *latestNonNil),
							Resolved:  true,
						})
						delete(next, key)
					}
				}
			}
		}

		// 3. public_ip rule
		if r, ok := ruleMap["public_ip"]; ok && node.Status != "disabled" {
			key := "public_ip:" + node.ID
			st, hasState := next[key]

			addrs := append([]string(nil), node.Addresses...)
			sort.Strings(addrs)
			currentVal := strings.Join(addrs, ", ")

			if !hasState {
				// 首次见到只记录不发
				if currentVal != "" {
					next[key] = State{
						Key:          key,
						Active:       false,
						Since:        snap.Now,
						LastNotified: snap.Now,
						Value:        currentVal,
					}
				}
			} else {
				// Empty candidates usually mean missing observations; keep the baseline.
				if currentVal != "" && st.Value != "" && currentVal != st.Value {
					cooldown := time.Duration(r.CooldownMinutes) * time.Minute
					shouldSend := false
					if !st.Active {
						shouldSend = true
					} else if cooldown > 0 && snap.Now.Sub(st.LastNotified) >= cooldown {
						shouldSend = true
					}
					if shouldSend {
						events = append(events, Event{
							Kind:      "public_ip",
							Severity:  "info",
							NodeID:    node.ID,
							Title:     fmt.Sprintf("节点「%s」公网 IP 变化", node.Name),
							Body:      fmt.Sprintf("%s → %s", st.Value, currentVal),
							DedupeKey: key,
							Value:     currentVal,
							Resolved:  false,
						})
						st.Value = currentVal
						st.Active = true
						st.LastNotified = snap.Now
						next[key] = st
					}
				} else if st.Value == "" && currentVal != "" {
					st.Value = currentVal
					next[key] = st
				}
			}
		}

		// 4. machine_traffic rule
		if r, ok := ruleMap["machine_traffic"]; ok && node.Status != "disabled" {
			if ts, ok := snap.Traffic[node.ID]; ok && ts.UsedBytes != nil && ts.AllowanceBytes != nil && *ts.AllowanceBytes > 0 {
				key := "machine_traffic:" + node.ID
				st := next[key]
				pct := float64(*ts.UsedBytes) / float64(*ts.AllowanceBytes) * 100.0

				if pct >= r.Threshold {
					cooldown := time.Duration(r.CooldownMinutes) * time.Minute
					shouldSend := false
					if !st.Active {
						shouldSend = true
					} else if cooldown > 0 && snap.Now.Sub(st.LastNotified) >= cooldown {
						shouldSend = true
					}
					if shouldSend {
						events = append(events, Event{
							Kind:      "machine_traffic",
							Severity:  "warning",
							NodeID:    node.ID,
							Title:     fmt.Sprintf("节点「%s」流量超过阈值", node.Name),
							Body:      fmt.Sprintf("已使用 %.1f%%（阈值 %.0f%%）", pct, r.Threshold),
							DedupeKey: key,
							Value:     fmt.Sprintf("%.1f", pct),
							Resolved:  false,
						})
						st.Key = key
						st.Active = true
						if st.Since.IsZero() {
							st.Since = snap.Now
						}
						st.LastNotified = snap.Now
						st.Value = fmt.Sprintf("%.1f", pct)
						next[key] = st
					}
				} else if st.Active {
					// 跌回去发 resolved (新周期)
					events = append(events, Event{
						Kind:      "machine_traffic",
						Severity:  "resolved",
						NodeID:    node.ID,
						Title:     fmt.Sprintf("节点「%s」流量告警恢复", node.Name),
						Body:      fmt.Sprintf("当前使用率已降至 %.1f%%", pct),
						DedupeKey: key,
						Value:     fmt.Sprintf("%.1f", pct),
						Resolved:  true,
					})
					delete(next, key)
				}
			}
		}
	}

	return events, next
}
