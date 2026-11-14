package singbox

import (
	"fmt"

	"proxycontrol/internal/panel/subscription/proxy"
)

func outboundTag(endpoint proxy.Proxy) string {
	if endpoint.Origin.ImportKind == "content" {
		return endpoint.Name
	}
	if endpoint.Protocol == "wireguard" {
		return endpoint.Origin.Node.Name + "-" + endpoint.Origin.Ref
	}
	return fmt.Sprintf("%s-%s-%s", endpoint.Origin.Node.Name, endpoint.Origin.Ref, endpoint.Protocol)
}

// resolveTags allocates tags across outbounds and WireGuard endpoints.
func resolveTags(entries []proxy.Proxy, rendered []bool) []string {
	tags := make([]string, len(entries))
	used := map[string]bool{}
	for i, p := range entries {
		if !rendered[i] {
			continue
		}
		tag := outboundTag(p)
		if used[tag] {
			base := tag + "-" + p.Origin.Ref
			tag = base
			for suffix := 2; used[tag]; suffix++ {
				tag = fmt.Sprintf("%s-%d", base, suffix)
			}
		}
		tags[i] = tag
		used[tag] = true
	}
	return tags
}
