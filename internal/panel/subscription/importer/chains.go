package importer

import (
	"context"
	"encoding/json"
	"strings"

	canvas "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/proxy"
)

// Chains expands only published snapshots and only endpoints already filtered
// for this viewer. A missing hop drops the entire scheme, never a partial route.
func (s *Store) Chains(ctx context.Context, entries []proxy.Proxy) ([]proxy.Proxy, []proxy.Warning) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id,name,client_published_layout FROM canvas_schemes WHERE execution='client' AND client_published_layout<>'' ORDER BY id")
	if err != nil {
		return entries, []proxy.Warning{proxy.SourceWarning("client-chains", "Client chains unavailable")}
	}
	defer rows.Close()
	warnings := []proxy.Warning{}
	byRef := map[string]proxy.Proxy{}
	used := map[string]bool{}
	for _, p := range entries {
		used[p.Name] = true
		if strings.HasPrefix(p.Origin.Source, "import:") {
			byRef["import:"+p.Origin.Ref] = p
		} else {
			byRef["inbound:"+p.Origin.Ref] = p
		}
	}
	for rows.Next() {
		var key, name, raw string
		if rows.Scan(&key, &name, &raw) != nil {
			continue
		}
		var layout canvas.CanvasLayout
		if json.Unmarshal([]byte(raw), &layout) != nil || canvas.ValidateClientLayout(layout) != nil {
			warnings = append(warnings, proxy.SourceWarning("client-chains", "Invalid client chain snapshot"))
			continue
		}
		made := map[string]proxy.Proxy{}
		valid := len(layout.Connections) > 0
		for _, i := range layout.Instances {
			p, ok := byRef[i.Ref]
			if !ok {
				valid = false
				break
			}
			p.Name = "chain-" + key + "-" + i.InstanceID
			p.ClientChain = key
			if used[p.Name] {
				valid = false
				break
			}
			made[i.InstanceID] = p
		}
		for _, c := range layout.Connections {
			a, aok := made[c.Source]
			b, bok := made[c.Target]
			if !aok || !bok || (c.Type != "proxy_chain" && c.Type != "tunnel") || b.DialerProxy != "" {
				valid = false
				break
			}
			b.DialerProxy = a.Name
			made[c.Target] = b
		}
		if !valid {
			warnings = append(warnings, proxy.SourceWarning("client-chains", "Client chain omitted: a hop is unavailable or not granted"))
			continue
		}
		for _, i := range layout.Instances {
			p := made[i.InstanceID]
			used[p.Name] = true
			entries = append(entries, p)
		}
	}
	if rows.Err() != nil {
		warnings = append(warnings, proxy.SourceWarning("client-chains", "Client chains unavailable"))
	}
	return entries, warnings
}
