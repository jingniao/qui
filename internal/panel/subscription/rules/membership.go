package rules

import (
	"strings"

	"golang.org/x/text/unicode/norm"
	"proxycontrol/internal/panel/subscription/proxy"
)

// EndpointRef derives a selectable identity before display-name renaming.
// An imported proxy is keyed by name|protocol like node group imports, because
// its Origin.Ref is a content hash that changes on refresh. Callers exclude
// client-chain clones using Proxy.ClientChain.
func EndpointRef(endpoint proxy.Proxy) string {
	origin := endpoint.Origin
	switch {
	case origin.Relayed:
		return "relay:" + origin.Ref
	case origin.Source == "published":
		return "inbound:" + origin.Ref
	case strings.HasPrefix(origin.Source, "import:"):
		return origin.Source + ":" + endpoint.Name + "|" + endpoint.Protocol
	default:
		return ""
	}
}

func normalizeKeyword(value string) string {
	return strings.ToLower(norm.NFKC.String(strings.TrimSpace(value)))
}

func matchesKeywords(name string, group Group) bool {
	name = normalizeKeyword(name)
	matches := func(keywords []string) bool {
		for _, keyword := range keywords {
			if strings.Contains(name, normalizeKeyword(keyword)) {
				return true
			}
		}
		return false
	}
	return (len(group.IncludeKeywords) == 0 || matches(group.IncludeKeywords)) && !matches(group.ExcludeKeywords)
}

// EmptyGroup is shared by authorization expansion and format filtering.
func EmptyGroup(name, policy string) (Member, proxy.Warning) {
	target := TargetDirect
	if policy == "reject" {
		target = TargetReject
	}
	return Member{Kind: KindBuiltin, Name: target}, warning("策略组 " + name + " 没有可用节点，已改为 " + target)
}
