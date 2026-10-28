package rules

import (
	"strings"

	"proxycontrol/internal/panel/subscription/proxy"
)

// Resolved is a template expanded for one user: group members are concrete
// proxy names, disabled rules are gone, rules are in run order and every
// reference the user cannot use is removed with a warning. Exporters render
// this and only this.
type Resolved struct {
	Client    Client
	DNS       DNS
	Groups    []ResolvedGroup
	Providers []ResolvedProvider
	Rules     []ResolvedRule
	Warnings  []proxy.Warning
}

// Member is a resolved group member or rule target. Proxy names and group names
// share one namespace in a client profile, so the kind is carried explicitly: an
// exporter that has to rename a proxy (for example one named like a group) still
// knows which references point at it.
type Member struct {
	Kind string
	Name string
}

// The kinds of a Member.
const (
	KindBuiltin = "builtin" // DIRECT or REJECT
	KindGroup   = "group"
	KindProxy   = "proxy"
)

// ResolvedGroup is a group of proxies, groups, DIRECT or REJECT.
type ResolvedGroup struct {
	Name            string
	Type            string
	Members         []Member
	TestURL         string
	IntervalSeconds int
}

// ResolvedProvider is a rule provider. ServiceSetID is set for a panel service
// rule, which the panel itself serves; URL is set for an external provider.
type ResolvedProvider struct {
	Name            string
	Behavior        string
	Format          string
	URL             string
	ServiceSetID    string
	IntervalSeconds int
}

// ResolvedRule is one rule in run order. For RULE-SET the payload is the
// provider name.
type ResolvedRule struct {
	Type      string
	Payload   string
	Target    Member
	NoResolve bool
}

// ServiceProviderName is the provider name of a panel service rule. It is
// derived from the id, never from the display name, so two services can never
// collide (the earlier page lowercased CJK names into the same slug).
func ServiceProviderName(serviceSetID string) string { return "qui-service-" + serviceSetID }

// serviceRefreshSeconds is how often a client refreshes a panel service rule.
const serviceRefreshSeconds = 86400

func warning(message string) proxy.Warning {
	return proxy.Warning{Stage: "rules", Ref: "template", Message: message}
}

// Expand resolves the template against the proxies of one user. Expansion
// never fails: whatever cannot apply to this user is dropped or replaced by
// DIRECT, with a warning for the Owner's preview.
func Expand(template Template, proxies []proxy.Proxy) Resolved {
	resolved := Resolved{Client: template.Client, DNS: template.DNS}
	names := make([]string, 0, len(proxies))
	byGroup := map[string][]string{}
	byNode := map[string]string{}
	for _, endpoint := range proxies {
		names = append(names, endpoint.Name)
		for _, label := range endpoint.Origin.Labels {
			if group, ok := strings.CutPrefix(label, "nodeGroup:"); ok {
				byGroup[group] = append(byGroup[group], endpoint.Name)
			}
		}
		if _, taken := byNode[endpoint.Origin.Node.ID]; !taken && endpoint.Origin.Node.ID != "" {
			byNode[endpoint.Origin.Node.ID] = endpoint.Name
		}
	}
	groups := map[string]bool{}
	for _, group := range template.Groups {
		groups[group.Name] = true
	}
	for _, group := range template.Groups {
		members := []Member{}
		seen := map[Member]bool{}
		add := func(member Member) {
			if !seen[member] {
				seen[member] = true
				members = append(members, member)
			}
		}
		for _, member := range group.Members {
			switch {
			case member == MemberAll:
				for _, name := range names {
					add(Member{Kind: KindProxy, Name: name})
				}
			case strings.HasPrefix(member, MemberNodeGroup):
				// Only granted groups carry labels, so a group the user was
				// not granted expands to nothing.
				for _, name := range byGroup[strings.TrimPrefix(member, MemberNodeGroup)] {
					add(Member{Kind: KindProxy, Name: name})
				}
			case strings.HasPrefix(member, MemberSource):
				for _, endpoint := range proxies {
					if endpoint.Origin.Source == "import:"+strings.TrimPrefix(member, MemberSource) {
						add(Member{Kind: KindProxy, Name: endpoint.Name})
					}
				}
			case member == TargetDirect || member == TargetReject:
				add(Member{Kind: KindBuiltin, Name: member})
			default:
				add(Member{Kind: KindGroup, Name: member})
			}
		}
		if len(members) == 0 {
			members = []Member{{Kind: KindBuiltin, Name: TargetDirect}}
			resolved.Warnings = append(resolved.Warnings, warning("策略组 "+group.Name+" 对该用户没有可用节点，已改为 DIRECT"))
		}
		resolved.Groups = append(resolved.Groups, ResolvedGroup{Name: group.Name, Type: group.Type, Members: members, TestURL: group.TestURL, IntervalSeconds: group.IntervalSeconds})
	}
	providers := map[string]bool{}
	for _, provider := range template.Providers {
		providers[provider.Name] = true
		resolved.Providers = append(resolved.Providers, ResolvedProvider{Name: provider.Name, Behavior: provider.Behavior, Format: provider.Format, URL: provider.URL, IntervalSeconds: provider.IntervalSeconds})
	}
	services := map[string]bool{}
	for _, rule := range Ordered(template.Rules) {
		target := Member{Kind: KindGroup, Name: rule.Target}
		switch {
		case strings.HasPrefix(rule.Target, TargetNodePrefix):
			name, found := byNode[strings.TrimPrefix(rule.Target, TargetNodePrefix)]
			if !found {
				resolved.Warnings = append(resolved.Warnings, warning("规则 "+rule.ID+" 指向的节点不在该用户的订阅中，已跳过"))
				continue
			}
			target = Member{Kind: KindProxy, Name: name}
		case rule.Target == TargetDirect || rule.Target == TargetReject:
			target.Kind = KindBuiltin
		case !groups[rule.Target]:
			resolved.Warnings = append(resolved.Warnings, warning("规则 "+rule.ID+" 的目标 "+rule.Target+" 不存在，已跳过"))
			continue
		}
		payload := rule.Payload
		if rule.Type == RuleSet {
			if id, ok := strings.CutPrefix(payload, ServicePrefix); ok {
				payload = ServiceProviderName(id)
				if !services[id] {
					services[id] = true
					resolved.Providers = append(resolved.Providers, ResolvedProvider{Name: payload, Behavior: "classical", Format: "text", ServiceSetID: id, IntervalSeconds: serviceRefreshSeconds})
				}
			} else if !providers[payload] {
				resolved.Warnings = append(resolved.Warnings, warning("规则 "+rule.ID+" 引用的规则集 "+payload+" 不存在，已跳过"))
				continue
			}
		}
		resolved.Rules = append(resolved.Rules, ResolvedRule{Type: rule.Type, Payload: payload, Target: target, NoResolve: rule.NoResolve})
	}
	return resolved
}
