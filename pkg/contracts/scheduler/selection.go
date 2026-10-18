package scheduler

import (
	"errors"
	"net/url"
	"proxycontrol/pkg/contracts/desiredstate"
	"sort"
	"time"
)

type Health struct {
	Healthy   bool      `json:"healthy"`
	LatencyMS int64     `json:"latencyMs"`
	CheckedAt time.Time `json:"checkedAt"`
}

func ValidateProbeURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(value) > 2048 {
		return errors.New("probe URL must be HTTP(S), without credentials, query or fragment")
	}
	return nil
}
func ValidateGroup(group desiredstate.OutboundGroup, available map[string]bool) error {
	if group.ID == "" || len(group.Members) < 1 || len(group.Members) > 16 {
		return errors.New("group needs one to sixteen members")
	}
	if group.Strategy != "primary" && group.Strategy != "round_robin" && group.Strategy != "latency" {
		return errors.New("invalid group strategy")
	}
	if err := ValidateProbeURL(group.ProbeURL); err != nil {
		return err
	}
	if group.ProbeInterval < 3 || group.ProbeInterval > 300 {
		return errors.New("probe interval must be 3 to 300 seconds")
	}
	if !desiredstate.ValidFailover(group.Failover) {
		return errors.New("invalid group failure policy")
	}
	if desiredstate.BackupKind(group.Failover) != "" && !available[group.BackupTargetID] {
		return errors.New("backup outbound missing")
	}
	seen := map[string]bool{}
	for _, member := range group.Members {
		if !available[member.OutboundID] || seen[member.OutboundID] || member.Priority < 0 || member.Priority > 1000 || member.Weight < 1 || member.Weight > 100 {
			return errors.New("invalid group member")
		}
		seen[member.OutboundID] = true
	}
	return nil
}
func HealthyMembers(group desiredstate.OutboundGroup, health map[string]Health, now time.Time) []desiredstate.GroupMember {
	members := []desiredstate.GroupMember{}
	for _, member := range group.Members {
		status, known := health[member.OutboundID]
		if !known || !status.Healthy || status.CheckedAt.IsZero() || now.Sub(status.CheckedAt) > time.Duration(max(group.ProbeInterval*3, 15))*time.Second || status.CheckedAt.After(now.Add(time.Minute)) {
			continue
		}
		members = append(members, member)
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].Priority == members[j].Priority {
			return members[i].OutboundID < members[j].OutboundID
		}
		return members[i].Priority < members[j].Priority
	})
	if group.Strategy == "latency" {
		sort.SliceStable(members, func(i, j int) bool {
			return health[members[i].OutboundID].LatencyMS < health[members[j].OutboundID].LatencyMS
		})
	}
	if (group.Strategy == "primary" || group.Strategy == "latency") && len(members) > 1 {
		members = members[:1]
	}
	return members
}
