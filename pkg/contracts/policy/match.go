// Package policy is the shared matching contract for simulation and compilation.
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
)

type Request struct {
	UserID    string    `json:"userId"`
	DeviceID  string    `json:"deviceId"`
	NodeID    string    `json:"nodeId"`
	GroupIDs  []string  `json:"groupIds"`
	Domain    string    `json:"domain"`
	TargetIP  string    `json:"targetIp"`
	Port      int       `json:"port"`
	Network   string    `json:"network"`
	Time      time.Time `json:"time"`
	InboundID string    `json:"inboundId"`
}
type Decision struct {
	Matched           bool                        `json:"matched"`
	PolicyID          string                      `json:"policyId"`
	PolicyName        string                      `json:"policyName"`
	Priority          int                         `json:"priority"`
	Action            desiredstate.Action         `json:"action"`
	Reason            string                      `json:"reason"`
	Logging           *desiredstate.PolicyLogging `json:"logging,omitempty"`
	Path              []string                    `json:"path,omitempty"`
	OutboundProtocol  string                      `json:"outboundProtocol,omitempty"`
	FinalEgressNodeID string                      `json:"finalEgressNodeId,omitempty"`
	SelectionStrategy string                      `json:"selectionStrategy,omitempty"`
	CandidatePaths    [][]string                  `json:"candidatePaths,omitempty"`
}

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
func Pattern(value string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(value), `\*`, ".*") + "$"
}
func domainMatches(pattern, domain string) bool {
	if domain == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return pattern == domain
	}
	matched, err := regexp.MatchString(Pattern(pattern), domain)
	return err == nil && matched
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
func ScopeMatches(scope desiredstate.Scope, request Request) bool {
	if len(scope.UserIDs) > 0 && !contains(scope.UserIDs, request.UserID) {
		return false
	}
	if len(scope.DeviceIDs) > 0 && !contains(scope.DeviceIDs, request.DeviceID) {
		return false
	}
	if len(scope.NodeIDs) > 0 && !contains(scope.NodeIDs, request.NodeID) {
		return false
	}
	if len(scope.GroupIDs) > 0 {
		found := false
		for _, group := range request.GroupIDs {
			if contains(scope.GroupIDs, group) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func Validate(rule *desiredstate.Policy) error {
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
	if len(rule.Match.Domains)+len(rule.Match.Suffixes)+len(rule.Match.CIDRs) > 2000 || len(rule.Match.Ports) > 100 || len(rule.Match.ServiceIDs) > 32 {
		return errors.New("policy matcher limit exceeded")
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
func ValidateRequest(request *Request) error {
	if request.NodeID == "" || request.Port < 1 || request.Port > 65535 || request.Network != "tcp" && request.Network != "udp" {
		return errors.New("node, valid port and tcp/udp are required")
	}
	if request.Domain != "" {
		domain, err := Domain(request.Domain, false)
		if err != nil {
			return err
		}
		request.Domain = domain
	}
	if request.TargetIP != "" {
		if address, err := netip.ParseAddr(request.TargetIP); err != nil {
			return errors.New("invalid target IP")
		} else {
			request.TargetIP = address.Unmap().String()
		}
	}
	if request.Domain == "" && request.TargetIP == "" {
		return errors.New("domain or target IP required")
	}
	if request.Time.IsZero() {
		request.Time = time.Now().UTC()
	}
	return nil
}
func Matches(rule desiredstate.Policy, request Request) bool {
	if !rule.Enabled || !ScopeMatches(rule.Scope, request) || !TimeActive(rule, request.Time) {
		return false
	}
	if rule.Match.Network != "" && rule.Match.Network != "all" && rule.Match.Network != request.Network {
		return false
	}
	if len(rule.Match.Ports) > 0 {
		found := false
		for _, value := range rule.Match.Ports {
			first, last, err := PortRange(value)
			if err == nil && request.Port >= first && request.Port <= last {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	hasAddress := len(rule.Match.Domains)+len(rule.Match.Suffixes)+len(rule.Match.CIDRs) > 0
	if !hasAddress {
		return len(rule.Match.ServiceIDs) == 0
	}
	for _, domain := range rule.Match.Domains {
		if domainMatches(domain, request.Domain) {
			return true
		}
	}
	for _, suffix := range rule.Match.Suffixes {
		if request.Domain == suffix || strings.HasSuffix(request.Domain, "."+suffix) {
			return true
		}
	}
	if address, err := netip.ParseAddr(request.TargetIP); err == nil {
		for _, value := range rule.Match.CIDRs {
			if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Contains(address) {
				return true
			}
			if target, err := netip.ParseAddr(value); err == nil && target == address {
				return true
			}
		}
	}
	return false
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
func Simulate(rules []desiredstate.Policy, request Request, fallback desiredstate.Action) Decision {
	for _, rule := range Sort(rules) {
		if Matches(rule, request) {
			return Decision{Matched: true, PolicyID: rule.ID, PolicyName: rule.Name, Priority: rule.Priority, Action: rule.Action, Reason: "matched scope, address, port, network and active time window", Logging: rule.Logging}
		}
	}
	return Decision{Action: fallback, Reason: "no matching policy; inbound default rule"}
}
