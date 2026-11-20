package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	canvas "proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/subscription/proxy"
)

// Chains expands only published snapshots and only endpoints already filtered
// for this viewer. A missing hop drops the entire scheme, never a partial route.
func (s *Store) Chains(ctx context.Context, entries []proxy.Proxy) ([]proxy.Proxy, []proxy.Warning, error) {
	rows, err := s.reader().QueryContext(ctx, "SELECT id,name,client_published_layout FROM canvas_schemes WHERE execution='client' AND client_published_layout<>'' ORDER BY id")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	warnings := []proxy.Warning{}
	byRef := map[string]proxy.Proxy{}
	used := map[string]bool{}
	for _, p := range entries {
		used[p.Name] = true
		if strings.HasPrefix(p.Origin.Source, "import:") {
			byRef["import:"+p.Origin.Ref] = p
		} else if !p.Origin.Relayed {
			byRef["inbound:"+p.Origin.Ref] = p
		}
	}
	for rows.Next() {
		var key, name, raw string
		if err = rows.Scan(&key, &name, &raw); err != nil {
			return nil, warnings, err
		}
		var layout canvas.CanvasLayout
		if err = json.Unmarshal([]byte(raw), &layout); err != nil {
			return nil, warnings, fmt.Errorf("client chain %s (%s): %w", name, key, err)
		}
		if err = canvas.ValidateClientLayout(layout); err != nil {
			return nil, warnings, fmt.Errorf("client chain %s (%s): %w", name, key, err)
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
			warnings = append(warnings, proxy.SourceWarning("client-chains", "客户端链「"+name+"」（"+key+"）已整体排除：组成端点不可用或未授权"))
			continue
		}
		for _, i := range layout.Instances {
			p := made[i.InstanceID]
			used[p.Name] = true
			entries = append(entries, p)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, warnings, err
	}
	return entries, warnings, nil
}
