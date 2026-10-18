package desiredstate

import (
	"errors"
	"net/url"
	"strings"
)

func validateGroup(group OutboundGroup, outbounds map[string]bool) error {
	// "weighted" is gone (plan D4): upstream Xray has no weighted sharing and
	// sing-box has no weighted selector. A stored revision that still says
	// "weighted" is rewritten to "round_robin" by Decode before validation.
	if group.Strategy != "primary" && group.Strategy != "round_robin" && group.Strategy != "latency" {
		return errors.New("invalid outbound group strategy")
	}
	if len(group.Members) < 1 || len(group.Members) > 16 {
		return errors.New("outbound group requires one to sixteen members")
	}
	parsed, err := url.Parse(group.ProbeURL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("invalid group probe URL")
	}
	if group.ProbeInterval < 3 || group.ProbeInterval > 300 {
		return errors.New("invalid group probe interval")
	}
	if !ValidFailover(group.Failover) {
		return errors.New("invalid group failover")
	}
	seen := map[string]bool{}
	for _, member := range group.Members {
		if !outbounds[member.OutboundID] || seen[member.OutboundID] || member.Priority < 0 || member.Priority > 1000 || member.Weight < 1 || member.Weight > 100 {
			return errors.New("invalid outbound group member")
		}
		seen[member.OutboundID] = true
	}
	return nil
}
func targetExists(value string, targets map[string]bool) bool {
	return targets[value] || strings.HasPrefix(value, "group:") && targets[strings.TrimPrefix(value, "group:")] || strings.HasPrefix(value, "chain:") && targets[strings.TrimPrefix(value, "chain:")]
}
