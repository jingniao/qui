// Package policy is the shared matching contract for policy validation and compilation.
package policy

import (
	"errors"
	"golang.org/x/net/idna"
	"net/netip"
	"proxycontrol/pkg/contracts/desiredstate"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
)

func Domain(value string, wildcard bool) (string, error) {
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	if value == "" || strings.ContainsAny(value, "/:@?# \\\t\r\n") {
		return "", errors.New("domain must be a hostname without path or URL")
	}
	if !wildcard && strings.Contains(value, "*") {
		return "", errors.New("wildcard not allowed here")
	}
	if !strings.Contains(value, "*") {
		ascii, err := idna.Lookup.ToASCII(value)
		if err != nil {
			return "", errors.New("invalid domain")
		}
		value = ascii
	}
	if len(value) > 253 {
		return "", errors.New("domain too long")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return "", errors.New("invalid domain label")
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || wildcard && r == '*') {
				return "", errors.New("invalid domain character")
			}
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid domain label")
		}
	}
	return value, nil
}
func Keyword(value string) (string, error) {
	if len(value) < 1 || len(value) > 253 {
		return "", errors.New("keyword must be between 1 and 253 bytes")
	}
	if value != strings.ToLower(value) {
		return "", errors.New("keyword must be lowercase")
	}
	for _, r := range value {
		if r == ',' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", errors.New("keyword contains invalid characters")
		}
	}
	return value, nil
}
func Pattern(value string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(value), `\*`, ".*") + "$"
}

func PortRange(value string) (int, int, error) {
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, 0, errors.New("invalid port range")
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil || first < 1 || first > 65535 {
		return 0, 0, errors.New("invalid port")
	}
	last := first
	if len(parts) == 2 {
		last, err = strconv.Atoi(parts[1])
		if err != nil || last < first || last > 65535 {
			return 0, 0, errors.New("invalid port range")
		}
	}
	return first, last, nil
}
func minutes(value string) (int, error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, errors.New("time window must be HH:MM-HH:MM")
	}
	hour, err := strconv.Atoi(value[:2])
	if err != nil || hour < 0 || hour > 23 {
		return 0, errors.New("invalid hour")
	}
	minute, err := strconv.Atoi(value[3:])
	if err != nil || minute < 0 || minute > 59 {
		return 0, errors.New("invalid minute")
	}
	return hour*60 + minute, nil
}
func TimeWindow(value string) (int, int, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0, 0, errors.New("time window must be HH:MM-HH:MM")
	}
	start, err := minutes(parts[0])
	if err != nil {
		return 0, 0, err
	}
	end, err := minutes(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if start == end {
		return 0, 0, errors.New("time window must have different start and end")
	}
	return start, end, nil
}
func TimeActive(rule desiredstate.Policy, at time.Time) bool {
	if rule.Match.TimeRange == "" {
		return true
	}
	start, end, err := TimeWindow(rule.Match.TimeRange)
	if err != nil {
		return false
	}
	zone := rule.Timezone
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return false
	}
	local := at.In(location)
	minute := local.Hour()*60 + local.Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// MaxMatchers bounds the entries an Owner writes into one policy or service
// rule. MaxExpandedMatchers bounds one policy after its service rules and their
// external sources were expanded into it (v1.5.2): the built-in 国内 set alone
// carries about 15000 entries. Only Agents whose cores report
// ExpandedPolicySupported accept more than MaxMatchers (see
// desiredstate.RequireExpandedPolicySupport).
const (
	MaxMatchers         = desiredstate.MaxPolicyMatchers
	MaxExpandedMatchers = desiredstate.MaxExpandedPolicyMatchers
)

// ErrMatcherLimit is returned when a policy has too many match entries.
var ErrMatcherLimit = errors.New("policy matcher limit exceeded")

// MatcherCount is the number of domain, suffix, keyword and CIDR entries.
func MatcherCount(rule desiredstate.Policy) int {
	return len(rule.Match.Domains) + len(rule.Match.Suffixes) + len(rule.Match.Keywords) + len(rule.Match.CIDRs)
}

// Validate checks a policy as the Owner saves it: at most MaxMatchers entries.
func Validate(rule *desiredstate.Policy) error { return validate(rule, MaxMatchers) }

// ValidateExpanded checks a policy whose service rules were expanded into it,
// as the panel publishes it and the Agent renders it: at most
// MaxExpandedMatchers entries.
func ValidateExpanded(rule *desiredstate.Policy) error { return validate(rule, MaxExpandedMatchers) }

func validate(rule *desiredstate.Policy, maxMatchers int) error {
	if rule.ID == "" || len(rule.ID) > 128 || len(rule.Name) > 240 || rule.Priority < 0 || rule.Priority > 1000000 {
		return errors.New("invalid policy identity or priority")
	}
	for _, scope := range [][]string{rule.Scope.UserIDs, rule.Scope.DeviceIDs, rule.Scope.NodeIDs, rule.Scope.GroupIDs} {
		if len(scope) > 100 {
			return errors.New("policy scope exceeds limit")
		}
		seen := map[string]bool{}
		for _, id := range scope {
			if id == "" || len(id) > 128 || seen[id] {
				return errors.New("invalid or duplicate policy scope")
			}
			seen[id] = true
		}
	}
	if MatcherCount(*rule) > maxMatchers || len(rule.Match.Ports) > 100 || len(rule.Match.ServiceIDs) > 32 || len(rule.Match.RuleSets) > desiredstate.MaxPolicyRuleSets {
		return ErrMatcherLimit
	}
	for _, set := range rule.Match.RuleSets {
		if err := desiredstate.ValidateRuleSet(set); err != nil {
			return err
		}
	}
	for index, value := range rule.Match.Domains {
		domain, err := Domain(value, true)
		if err != nil {
			return err
		}
		rule.Match.Domains[index] = domain
	}
	for index, value := range rule.Match.Suffixes {
		domain, err := Domain(strings.TrimPrefix(value, "."), false)
		if err != nil {
			return err
		}
		rule.Match.Suffixes[index] = domain
	}
	for index, value := range rule.Match.Keywords {
		kw, err := Keyword(value)
		if err != nil {
			return err
		}
		rule.Match.Keywords[index] = kw
	}
	for index, value := range rule.Match.CIDRs {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			rule.Match.CIDRs[index] = prefix.Masked().String()
		} else if address, err := netip.ParseAddr(value); err == nil {
			rule.Match.CIDRs[index] = address.String()
		} else {
			return errors.New("invalid IP/CIDR")
		}
	}
	for _, port := range rule.Match.Ports {
		if _, _, err := PortRange(port); err != nil {
			return err
		}
	}
	if rule.Match.Network != "" && rule.Match.Network != "all" && rule.Match.Network != "tcp" && rule.Match.Network != "udp" {
		return errors.New("network must be all, tcp or udp")
	}
	if rule.Match.TimeRange != "" {
		if _, _, err := TimeWindow(rule.Match.TimeRange); err != nil {
			return err
		}
		if rule.Timezone == "" {
			rule.Timezone = "UTC"
		}
		if _, err := time.LoadLocation(rule.Timezone); err != nil {
			return errors.New("invalid policy timezone")
		}
	}
	if !contains([]string{"Direct", "Block", "Outbound", "OutboundGroup", "ProxyChain"}, rule.Action.Type) {
		return errors.New("invalid policy action")
	}
	if rule.Action.Type != "Direct" && rule.Action.Type != "Block" && rule.Action.TargetID == "" {
		return errors.New("policy target required")
	}
	if rule.Action.Failover != "" && !contains([]string{"Block", "Direct", "BackupOutbound", "BackupOutboundGroup", "BackupProxyChain"}, rule.Action.Failover) {
		return errors.New("invalid policy failover")
	}
	if strings.HasPrefix(rule.Action.Failover, "Backup") && rule.Action.BackupID == "" {
		return errors.New("backup target required")
	}
	if rule.Logging == nil {
		rule.Logging = &desiredstate.PolicyLogging{LogDomain: true, LogTraffic: true}
	}
	return nil
}

func Sort(rules []desiredstate.Policy) []desiredstate.Policy {
	ordered := append([]desiredstate.Policy(nil), rules...)
	for index := range ordered {
		rule := &ordered[index]
		rule.Match.Domains = append([]string(nil), rule.Match.Domains...)
		rule.Match.Suffixes = append([]string(nil), rule.Match.Suffixes...)
		rule.Match.CIDRs = append([]string(nil), rule.Match.CIDRs...)
		rule.Match.Ports = append([]string(nil), rule.Match.Ports...)
		rule.Match.ServiceIDs = append([]string(nil), rule.Match.ServiceIDs...)
		rule.Scope.UserIDs = append([]string(nil), rule.Scope.UserIDs...)
		rule.Scope.DeviceIDs = append([]string(nil), rule.Scope.DeviceIDs...)
		rule.Scope.NodeIDs = append([]string(nil), rule.Scope.NodeIDs...)
		rule.Scope.GroupIDs = append([]string(nil), rule.Scope.GroupIDs...)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority == ordered[j].Priority {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Priority < ordered[j].Priority
	})
	return ordered
}
