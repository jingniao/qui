package rules

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FieldError is a validation failure of one field. Field is a path such as
// "rules[3].payload", so the page can show the message next to the input.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

func fieldError(field, format string, args ...any) *FieldError {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Lookup reports whether the panel objects a template refers to exist. A nil
// Lookup accepts every reference; the store validates with a real one.
type Lookup interface {
	ServiceSetExists(ctx context.Context, id string) bool
	NodeGroupExists(ctx context.Context, id string) bool
	NodeExists(ctx context.Context, id string) bool
}

// reservedNames are names mihomo gives a meaning of its own.
var reservedNames = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "GLOBAL": true}

var (
	modes        = map[string]bool{"rule": true, "global": true, "direct": true}
	logLevels    = map[string]bool{"silent": true, "error": true, "warning": true, "info": true, "debug": true}
	groupTypes   = map[string]bool{"select": true, "url-test": true, "fallback": true}
	behaviors    = map[string]bool{"domain": true, "ipcidr": true, "classical": true}
	enhanced     = map[string]bool{"fake-ip": true, "redir-host": true}
	dnsSchemes   = map[string]bool{"udp": true, "tcp": true, "tls": true, "https": true, "quic": true}
	noResolvable = map[string]bool{RuleIPCIDR: true, RuleIPCIDR6: true, RuleGeoIP: true, RuleSet: true}
)

// validName accepts a group or provider name a Clash rule line can carry: no
// comma (the rule separator), no control or line characters, no quotes and no
// leading '@' or reserved prefix.
func validName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > MaxNameRunes || strings.TrimSpace(name) != name {
		return false
	}
	if strings.HasPrefix(name, "@") || strings.HasPrefix(name, TargetNodePrefix) || strings.HasPrefix(name, ServicePrefix) {
		return false
	}
	for _, char := range name {
		if unicode.IsControl(char) || char == ',' || char == '"' || char == '\'' || char == '#' || char == '\\' {
			return false
		}
	}
	return true
}

// Validate checks the whole template (plan section 4.4): unique names, existing
// targets, no group cycles, one final MATCH, payload syntax per type, https
// providers and the size limits.
func Validate(ctx context.Context, template Template, lookup Lookup) error {
	if err := validateClient(template.Client); err != nil {
		return err
	}
	if err := validateDNS(template.DNS); err != nil {
		return err
	}
	if len(template.Groups) == 0 {
		return fieldError("groups", "至少需要一个策略组")
	}
	if len(template.Groups) > MaxGroups {
		return fieldError("groups", "策略组最多 %d 个", MaxGroups)
	}
	if len(template.Providers) > MaxProviders {
		return fieldError("providers", "规则集最多 %d 个", MaxProviders)
	}
	if len(template.Rules) > MaxRules {
		return fieldError("rules", "规则最多 %d 条", MaxRules)
	}
	groups := map[string]int{}
	for index, group := range template.Groups {
		field := fmt.Sprintf("groups[%d]", index)
		if !validName(group.Name) || reservedNames[strings.ToUpper(group.Name)] {
			return fieldError(field+".name", "策略组名称不能为空、不能包含逗号或引号，也不能使用保留名称")
		}
		if _, taken := groups[group.Name]; taken {
			return fieldError(field+".name", "策略组名称 %q 重复", group.Name)
		}
		groups[group.Name] = index
	}
	providers := map[string]bool{}
	for index, provider := range template.Providers {
		field := fmt.Sprintf("providers[%d]", index)
		if !validName(provider.Name) {
			return fieldError(field+".name", "规则集名称不能为空、不能包含逗号或引号")
		}
		if providers[provider.Name] {
			return fieldError(field+".name", "规则集名称 %q 重复", provider.Name)
		}
		providers[provider.Name] = true
		if !behaviors[provider.Behavior] {
			return fieldError(field+".behavior", "规则集类型必须是 domain、ipcidr 或 classical")
		}
		switch provider.Format {
		case "", "yaml", "text":
		case "mrs":
			if provider.Behavior == "classical" {
				return fieldError(field+".format", "mrs 格式不支持 classical 类型")
			}
		default:
			return fieldError(field+".format", "规则集格式必须是 yaml、text 或 mrs")
		}
		if err := validateHTTPS(provider.URL); err != nil {
			return fieldError(field+".url", "%s", err.Error())
		}
		if provider.IntervalSeconds < 60 || provider.IntervalSeconds > 7*86400 {
			return fieldError(field+".intervalSeconds", "更新间隔必须在 60 秒到 7 天之间")
		}
	}
	for index, group := range template.Groups {
		field := fmt.Sprintf("groups[%d]", index)
		if !groupTypes[group.Type] {
			return fieldError(field+".type", "策略组类型必须是 select、url-test 或 fallback")
		}
		if len(group.Members) == 0 || len(group.Members) > MaxMembers {
			return fieldError(field+".members", "策略组需要 1 到 %d 个成员", MaxMembers)
		}
		seen := map[string]bool{}
		for _, member := range group.Members {
			if seen[member] {
				return fieldError(field+".members", "成员 %q 重复", member)
			}
			seen[member] = true
			if err := validateMember(ctx, member, group.Name, groups, lookup); err != nil {
				return fieldError(field+".members", "%s", err.Error())
			}
		}
		if group.Type != "select" {
			if err := validateTestURL(group.TestURL); err != nil {
				return fieldError(field+".testUrl", "%s", err.Error())
			}
			if group.IntervalSeconds < 10 || group.IntervalSeconds > 86400 {
				return fieldError(field+".intervalSeconds", "测速间隔必须在 10 秒到 1 天之间")
			}
		}
	}
	if cycle := groupCycle(template.Groups, groups); cycle != "" {
		return fieldError("groups", "策略组之间形成了循环引用（%s）", cycle)
	}
	ids := map[string]bool{}
	for index, rule := range template.Rules {
		field := fmt.Sprintf("rules[%d]", index)
		if rule.ID == "" || len(rule.ID) > MaxRuleIDBytes || !printable(rule.ID) {
			return fieldError(field+".id", "规则 ID 无效")
		}
		if ids[rule.ID] {
			return fieldError(field+".id", "规则 ID %q 重复", rule.ID)
		}
		ids[rule.ID] = true
		if rule.Priority < 1 || rule.Priority > 99999 {
			return fieldError(field+".priority", "优先级必须在 1 到 99999 之间")
		}
		if utf8.RuneCountInString(rule.Description) > MaxDescription || !printable(rule.Description) {
			return fieldError(field+".description", "备注最多 %d 个字符", MaxDescription)
		}
		if err := validatePayload(ctx, rule, providers, lookup); err != nil {
			return fieldError(field+".payload", "%s", err.Error())
		}
		if rule.NoResolve && !noResolvable[rule.Type] {
			return fieldError(field+".noResolve", "%s 规则不支持 no-resolve", rule.Type)
		}
		if err := validateTarget(ctx, rule.Target, groups, lookup); err != nil {
			return fieldError(field+".target", "%s", err.Error())
		}
	}
	ordered := Ordered(template.Rules)
	matches := 0
	if len(ordered) == 0 {
		return fieldError("rules", "至少需要一条已启用的 MATCH 规则")
	}
	for _, rule := range ordered {
		if rule.Type == RuleMatch {
			matches++
		}
	}
	if matches != 1 || ordered[len(ordered)-1].Type != RuleMatch {
		return fieldError("rules", "已启用的规则中必须恰好有一条 MATCH，且排在最后（优先级数字最大）")
	}
	return nil
}

// Ordered returns the enabled rules in the order they run: ascending priority,
// ties kept in list order.
func Ordered(rules []Rule) []Rule {
	enabled := []Rule{}
	for _, rule := range rules {
		if rule.Enabled {
			enabled = append(enabled, rule)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool { return enabled[i].Priority < enabled[j].Priority })
	return enabled
}

func printable(value string) bool {
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validateClient(client Client) error {
	if !modes[client.Mode] {
		return fieldError("client.mode", "运行模式必须是 rule、global 或 direct")
	}
	if client.MixedPort < 1024 || client.MixedPort > 65535 {
		return fieldError("client.mixedPort", "混合端口必须在 1024 到 65535 之间")
	}
	if !logLevels[client.LogLevel] {
		return fieldError("client.logLevel", "日志级别必须是 silent、error、warning、info 或 debug")
	}
	return nil
}

func validateDNS(dns DNS) error {
	if !dns.Enable && len(dns.Nameservers) == 0 && len(dns.Fallback) == 0 && dns.EnhancedMode == "" {
		return nil
	}
	if !enhanced[dns.EnhancedMode] {
		return fieldError("dns.enhancedMode", "DNS 模式必须是 fake-ip 或 redir-host")
	}
	if dns.Enable && len(dns.Nameservers) == 0 {
		return fieldError("dns.nameservers", "启用 DNS 时至少需要一个上游服务器")
	}
	for name, list := range map[string][]string{"dns.nameservers": dns.Nameservers, "dns.fallback": dns.Fallback} {
		if len(list) > MaxResolvers {
			return fieldError(name, "最多 %d 个服务器", MaxResolvers)
		}
		for _, server := range list {
			if err := validateResolver(server); err != nil {
				return fieldError(name, "%q：%s", server, err.Error())
			}
		}
	}
	return nil
}

// validateResolver accepts a bare IP address (plain UDP) or a URL whose scheme
// a Clash client understands.
func validateResolver(server string) error {
	if _, err := netip.ParseAddr(server); err == nil {
		return nil
	}
	parsed, err := url.Parse(server)
	if err != nil || !dnsSchemes[parsed.Scheme] || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("必须是 IP 地址或 udp/tcp/tls/https/quic 地址")
	}
	return nil
}

func validateHTTPS(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || len(value) > 2048 {
		return fmt.Errorf("规则集地址必须是 https 地址")
	}
	return nil
}

func validateTestURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || len(value) > 2048 {
		return fmt.Errorf("测速地址必须是 http 或 https 地址")
	}
	return nil
}

func validateMember(ctx context.Context, member, self string, groups map[string]int, lookup Lookup) error {
	switch {
	case member == TargetDirect || member == TargetReject || member == MemberAll:
		return nil
	case strings.HasPrefix(member, MemberSource):
		id := strings.TrimPrefix(member, MemberSource)
		if id == "" || len(id) > 128 || strings.ContainsAny(id, " /\\\r\n\t:") {
			return fmt.Errorf("无效的导入来源 ID")
		}
		return nil
	case strings.HasPrefix(member, MemberNodeGroup):
		id := strings.TrimPrefix(member, MemberNodeGroup)
		if id == "" || (lookup != nil && !lookup.NodeGroupExists(ctx, id)) {
			return fmt.Errorf("节点组 %q 不存在", id)
		}
		return nil
	case member == self:
		return fmt.Errorf("策略组不能包含自己")
	}
	if _, ok := groups[member]; !ok {
		return fmt.Errorf("成员 %q 既不是 DIRECT/REJECT、@all、@nodeGroup:<id>，也不是已定义的策略组", member)
	}
	return nil
}

func validateTarget(ctx context.Context, target string, groups map[string]int, lookup Lookup) error {
	switch {
	case target == TargetDirect || target == TargetReject:
		return nil
	case strings.HasPrefix(target, TargetNodePrefix):
		id := strings.TrimPrefix(target, TargetNodePrefix)
		if id == "" || (lookup != nil && !lookup.NodeExists(ctx, id)) {
			return fmt.Errorf("节点 %q 不存在", id)
		}
		return nil
	}
	if _, ok := groups[target]; !ok {
		return fmt.Errorf("目标 %q 不是 DIRECT、REJECT、已定义的策略组或节点", target)
	}
	return nil
}

func validatePayload(ctx context.Context, rule Rule, providers map[string]bool, lookup Lookup) error {
	payload := rule.Payload
	if len(payload) > MaxPayloadBytes {
		return fmt.Errorf("匹配内容最多 %d 字节", MaxPayloadBytes)
	}
	switch rule.Type {
	case RuleMatch:
		if payload != "" {
			return fmt.Errorf("MATCH 规则没有匹配内容")
		}
		return nil
	case RuleDomain, RuleDomainSuffix:
		if !validDomain(payload) {
			return fmt.Errorf("%q 不是有效的域名", payload)
		}
	case RuleDomainKeyword:
		if payload == "" || strings.ContainsAny(payload, ", \t") || !printable(payload) {
			return fmt.Errorf("关键字不能为空，也不能包含逗号或空白")
		}
	case RuleIPCIDR, RuleIPCIDR6:
		prefix, err := netip.ParsePrefix(payload)
		if err != nil || prefix.Masked() != prefix {
			return fmt.Errorf("%q 不是规范的 CIDR 网段（例如 192.168.0.0/16）", payload)
		}
		if rule.Type == RuleIPCIDR6 && !prefix.Addr().Is6() {
			return fmt.Errorf("IP-CIDR6 需要 IPv6 网段")
		}
	case RuleGeoIP:
		if !validGeoIP(payload) {
			return fmt.Errorf("GEOIP 需要两位国家代码，或 LAN / PRIVATE")
		}
	case RuleGeoSite:
		if !validGeoSite(payload) {
			return fmt.Errorf("%q 不是有效的 GEOSITE 分类", payload)
		}
	case RuleDstPort:
		if err := validatePorts(payload); err != nil {
			return err
		}
	case RuleSet:
		if strings.HasPrefix(payload, ServicePrefix) {
			id := strings.TrimPrefix(payload, ServicePrefix)
			if id == "" || (lookup != nil && !lookup.ServiceSetExists(ctx, id)) {
				return fmt.Errorf("服务规则 %q 不存在", id)
			}
			return nil
		}
		if !providers[payload] {
			return fmt.Errorf("规则集 %q 未定义", payload)
		}
	default:
		return fmt.Errorf("不支持的规则类型 %q", rule.Type)
	}
	return nil
}

func validDomain(value string) bool {
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
				return false
			}
		}
	}
	return true
}

func validGeoIP(value string) bool {
	switch strings.ToUpper(value) {
	case "LAN", "PRIVATE":
		return true
	}
	if len(value) != 2 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z') {
			return false
		}
	}
	return true
}

func validGeoSite(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '!' || char == '@') {
			return false
		}
	}
	return true
}

// validatePorts accepts mihomo's DST-PORT syntax: ports or ranges separated by
// '/', for example "80/443/8000-8100". A comma would end the rule early.
func validatePorts(value string) error {
	if value == "" {
		return fmt.Errorf("端口不能为空")
	}
	if strings.Contains(value, ",") {
		return fmt.Errorf("多个端口用 / 分隔（例如 80/443），逗号会截断规则")
	}
	for _, part := range strings.Split(value, "/") {
		low, high, isRange := strings.Cut(part, "-")
		first, err := strconv.Atoi(low)
		if err != nil || first < 1 || first > 65535 {
			return fmt.Errorf("%q 不是有效的端口", part)
		}
		if isRange {
			last, err := strconv.Atoi(high)
			if err != nil || last < first || last > 65535 {
				return fmt.Errorf("%q 不是有效的端口范围", part)
			}
		}
	}
	return nil
}

// groupCycle reports a group that reaches itself through group members, or "".
func groupCycle(groups []Group, index map[string]int) string {
	const (
		unvisited = iota
		active
		done
	)
	state := make([]int, len(groups))
	var visit func(int) string
	visit = func(at int) string {
		state[at] = active
		for _, member := range groups[at].Members {
			next, ok := index[member]
			if !ok {
				continue
			}
			switch state[next] {
			case active:
				return groups[at].Name + " → " + member
			case unvisited:
				if found := visit(next); found != "" {
					return found
				}
			}
		}
		state[at] = done
		return ""
	}
	for at := range groups {
		if state[at] == unvisited {
			if found := visit(at); found != "" {
				return found
			}
		}
	}
	return ""
}
