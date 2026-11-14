package proxy

import (
	"context"
	"fmt"
	"strings"
)

// InboundRouteSummary describes the associated drawing, not a confirmed runtime revision.
type InboundRouteSummary struct {
	Type        string   `json:"type"`
	SchemeID    string   `json:"schemeId,omitempty"`
	SchemeName  string   `json:"schemeName,omitempty"`
	BindingMode string   `json:"bindingMode,omitempty"`
	Path        []string `json:"path"`
	ExitNodeID  string   `json:"exitNodeId,omitempty"`
	// PathIncomplete marks a drawing whose route cannot be resolved (a fork, a
	// cycle or a missing reference); Path then holds only the entry server.
	PathIncomplete bool `json:"pathIncomplete,omitempty"`
}

type canvasRouteRef struct{ nodeID, name, protocol string }

// ProjectInboundRoutes enriches only the list response. Both queries are batch reads;
// no credentials, publication state or stored inbound fields are changed.
func (r *Repository) ProjectInboundRoutes(ctx context.Context, items []Inbound) error {
	if len(items) == 0 {
		return nil
	}
	rows, err := r.Database.QueryContext(ctx, `SELECT 'server:'||id,id,name,'' FROM nodes
 UNION ALL SELECT 'inbound:'||i.id,n.id,n.name,'' FROM inbounds i JOIN nodes n ON n.id=i.node_id
 UNION ALL SELECT 'outbound:'||o.id,n.id,n.name,o.protocol FROM outbounds o JOIN nodes n ON n.id=COALESCE(o.target_node_id,o.node_id)
 UNION ALL SELECT 'tunnel:'||h.tunnel_id,n.id,n.name,'' FROM tunnel_hops h JOIN nodes n ON n.id=h.node_id WHERE h.role='exit'
 UNION ALL SELECT 'rule:'||r.id,n.id,n.name,'' FROM tunnel_rules r
 LEFT JOIN tunnel_hops h ON h.tunnel_id=r.tunnel_id AND h.role='exit'
 LEFT JOIN inbounds i ON i.id=r.target_inbound_id
 JOIN nodes n ON n.id=COALESCE(h.node_id,i.node_id,r.owner_node_id)`)
	if err != nil {
		return err
	}
	refs := map[string]canvasRouteRef{}
	for rows.Next() {
		var ref string
		var value canvasRouteRef
		if err := rows.Scan(&ref, &value.nodeID, &value.name, &value.protocol); err != nil {
			rows.Close()
			return err
		}
		refs[ref] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	ids := make([]any, len(items))
	indices := map[string][]int{}
	for index := range items {
		entry := &items[index]
		route := entry.DefaultOutbound
		protocol := refs["outbound:"+route].protocol
		kind := "direct"
		if route != "" && route != "direct" && route != "block" && protocol != "direct" && protocol != "block" {
			kind = "custom"
		}
		entry.RouteSummary = &InboundRouteSummary{Type: kind, Path: []string{refs["server:"+entry.NodeID].name}}
		if owner := entry.ManagedBy; owner != nil && (owner.Kind == "tunnel" || owner.Kind == "rule") {
			entry.RouteSummary.Type = "tunnel"
			if exit, ok := refs[owner.Kind+":"+owner.ID]; ok {
				entry.RouteSummary.ExitNodeID = exit.nodeID
				if exit.nodeID != entry.NodeID {
					entry.RouteSummary.Path = append(entry.RouteSummary.Path, exit.name)
				}
			}
		}
		ids[index] = entry.ID
		indices[entry.ID] = append(indices[entry.ID], index)
	}
	rows, err = r.Database.QueryContext(ctx, `SELECT b.inbound_id,b.instance_id,b.mode,COALESCE(s.id,''),COALESCE(s.name,''),COALESCE(s.layout_json,'')
 FROM canvas_entry_bindings b LEFT JOIN canvas_schemes s ON s.id=b.scheme_id
 WHERE b.inbound_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var inboundID, instanceID, mode, schemeID, schemeName, stored string
		if err := rows.Scan(&inboundID, &instanceID, &mode, &schemeID, &schemeName, &stored); err != nil {
			return err
		}
		// One unreadable drawing must not fail the whole inbound list: its entry
		// keeps the relay type and the entry server, marked incomplete.
		var layout CanvasLayout
		layoutErr := error(nil)
		if stored != "" {
			layout, layoutErr = decodeCanvasLayout([]byte(stored))
		}
		for _, index := range indices[inboundID] {
			summary := items[index].RouteSummary
			summary.Type = "relay"
			summary.SchemeID = schemeID
			summary.SchemeName = schemeName
			summary.BindingMode = mode
			if stored == "" {
				continue
			} // Detached bindings retain ownership but have no drawing.
			if layoutErr != nil {
				summary.PathIncomplete = true
				continue
			}
			path, exit, err := canvasRoutePath(layout, instanceID, items[index].NodeID, refs)
			if err != nil {
				summary.PathIncomplete = true
				continue
			}
			summary.Path = path
			summary.ExitNodeID = exit
		}
	}
	return rows.Err()
}

func canvasRoutePath(layout CanvasLayout, start, entryNode string, refs map[string]canvasRouteRef) ([]string, string, error) {
	instances := map[string]CanvasInstance{}
	next := map[string]string{}
	ambiguous := map[string]bool{}
	for _, instance := range layout.Instances {
		instances[instance.InstanceID] = instance
	}
	for _, connection := range layout.Connections {
		if previous, ok := next[connection.Source]; ok && previous != connection.Target {
			ambiguous[connection.Source] = true
		}
		next[connection.Source] = connection.Target
	}
	entry, ok := refs["server:"+entryNode]
	if !ok {
		return nil, "", fmt.Errorf("route server %s is missing", entryNode)
	}
	path := []string{entry.name}
	last := entryNode
	seen := map[string]bool{}
	for current := start; current != ""; current = next[current] {
		if ambiguous[current] {
			return nil, "", fmt.Errorf("ambiguous route at instance %s", current)
		}
		if seen[current] {
			return nil, "", fmt.Errorf("route cycle at instance %s", current)
		}
		seen[current] = true
		instance, ok := instances[current]
		if !ok {
			return nil, "", fmt.Errorf("route instance %s is missing", current)
		}
		ref, ok := refs[instance.Ref]
		if !ok {
			return nil, "", fmt.Errorf("route reference %s is missing", instance.Ref)
		}
		if ref.nodeID != last {
			path = append(path, ref.name)
			last = ref.nodeID
		}
	}
	return path, last, nil
}
