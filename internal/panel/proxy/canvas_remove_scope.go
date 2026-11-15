package proxy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type CanvasRemoveInput struct {
	UpdatedAt string `json:"updatedAt"`
}

type canvasRemoveEntry struct{ InboundID, NodeID string }
type CanvasRemoveReferrer struct{ Kind, ID, Name string }
type CanvasRemoveObject struct {
	CanvasTunnelObject
	TunnelID  string
	Keep      bool
	Referrers []CanvasRemoveReferrer
}
type CanvasRemovePlan struct {
	SchemeID, UpdatedAt string
	Entries             []canvasRemoveEntry
	Objects             []CanvasRemoveObject
	Nodes               []string
	unsharedTunnelIDs   []string
}

// This is an internal ownership calculation, not a removal preview. The final
// transaction verifies the captured rows before deleting any business objects.
func (s *Service) canvasRemovalScope(ctx context.Context, schemeID string, input CanvasRemoveInput) (CanvasRemovePlan, error) {
	return s.canvasRemovalScopeExcluding(ctx, schemeID, input, nil)
}

// excluded contains consumers removed by the same cascade transaction.
func (s *Service) canvasRemovalScopeExcluding(ctx context.Context, schemeID string, input CanvasRemoveInput, excluded map[string]bool) (CanvasRemovePlan, error) {
	result := CanvasRemovePlan{SchemeID: schemeID, UpdatedAt: input.UpdatedAt}
	if strings.TrimSpace(input.UpdatedAt) == "" {
		return result, errors.New("缺少 updatedAt 字段")
	}
	scheme, err := s.Repository.CanvasScheme(ctx, schemeID)
	if err != nil {
		return result, err
	}
	if scheme.UpdatedAt != input.UpdatedAt {
		return result, ErrStaleScheme
	}
	if scheme.Execution == canvasExecutionClient {
		return result, nil
	}
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT inbound_id FROM canvas_entry_bindings WHERE scheme_id=? ORDER BY id", schemeID)
	if err != nil {
		return result, err
	}
	bound := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		bound[id] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	// Historical unbound inbound entries belong to the scheme just like create.
	// A migrated reuse entry references a server and has no binding, so its
	// ordinary inbound is deliberately not collected here.
	for _, instance := range scheme.Layout.Instances {
		ref, _ := parseCanvasRef(instance.Ref)
		if instance.Role == "entry" && ref.kind == "inbound" {
			bound[ref.id] = true
		}
	}
	for id := range bound {
		inbound, e := s.Repository.Inbound(ctx, id, true)
		if e != nil {
			return result, e
		}
		refs, e := s.Repository.canvasReferrers(ctx, id)
		if e != nil {
			return result, e
		}
		extra, e := s.canvasRemoveExtraReferrers(ctx, schemeID, "inbound", id)
		if e != nil {
			return result, e
		}
		if excluded == nil && (len(refs) > 0 || len(extra) > 0) {
			return result, fmt.Errorf("入口「%s」仍被其他资源引用，无法撤除", inbound.Name)
		}
		result.Entries = append(result.Entries, canvasRemoveEntry{InboundID: id, NodeID: inbound.NodeID})
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].InboundID < result.Entries[j].InboundID })
	tunnels := map[string]bool{}
	for _, c := range scheme.Layout.Connections {
		if c.TunnelID != "" {
			tunnels[c.TunnelID] = true
		}
	}
	rows, err = s.Repository.Database.QueryContext(ctx, "SELECT tunnel_id FROM canvas_tunnel_publications WHERE scheme_id=?", schemeID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		tunnels[id] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	tunnelIDs := make([]string, 0, len(tunnels))
	for id := range tunnels {
		tunnelIDs = append(tunnelIDs, id)
	}
	sort.Strings(tunnelIDs)
	for _, tunnelID := range tunnelIDs {
		refs, e := s.Repository.tunnelSchemes(ctx, tunnelID)
		if e != nil {
			return result, e
		}
		// Published associations protect channels even after another draft drops them.
		rows, e := s.Repository.Database.QueryContext(ctx, "SELECT s.id,s.name FROM canvas_tunnel_publications p JOIN canvas_schemes s ON s.id=p.scheme_id WHERE p.tunnel_id=?", tunnelID)
		if e != nil {
			return result, e
		}
		for rows.Next() {
			var ref CanvasSchemeRef
			if e = rows.Scan(&ref.ID, &ref.Name); e != nil {
				rows.Close()
				return result, e
			}
			refs = append(refs, ref)
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return result, e
		}
		if e = rows.Close(); e != nil {
			return result, e
		}
		shared := false
		for _, ref := range refs {
			if ref.ID != schemeID && !excluded["scheme:"+ref.ID] {
				shared = true
			}
		}
		if !shared {
			result.unsharedTunnelIDs = append(result.unsharedTunnelIDs, tunnelID)
		}
		objects, e := s.Repository.tunnelObjects(ctx, tunnelID)
		if e != nil {
			return result, e
		}
		for _, object := range objects {
			item := CanvasRemoveObject{CanvasTunnelObject: object, TunnelID: tunnelID, Referrers: []CanvasRemoveReferrer{}}
			seen := map[string]bool{}
			for _, ref := range refs {
				if ref.ID != schemeID && !excluded["scheme:"+ref.ID] && !seen[ref.ID] {
					item.Keep = true
					item.Referrers = append(item.Referrers, CanvasRemoveReferrer{Kind: "scheme", ID: ref.ID, Name: ref.Name})
					seen[ref.ID] = true
				}
			}
			result.Objects = append(result.Objects, item)
		}
	}
	candidates := map[string]int{}
	for i, o := range result.Objects {
		candidates[o.Kind+":"+o.ID] = i
	}
	// Retention propagates from external users through the generated dependency graph.
	changed := true
	for changed {
		changed = false
		for index := range result.Objects {
			o := &result.Objects[index]
			if o.Keep {
				continue
			}
			refs, e := s.Repository.canvasReferrers(ctx, o.ID)
			if e != nil {
				return result, e
			}
			extra, e := s.canvasRemoveExtraReferrers(ctx, schemeID, o.Kind, o.ID)
			if e != nil {
				return result, e
			}
			for _, ref := range extra {
				if !excluded[ref.Kind+":"+ref.ID] {
					o.Referrers = append(o.Referrers, ref)
				}
			}
			for _, ref := range refs {
				if excluded[ref.Kind+":"+ref.ID] || ref.Kind == "inbound" && bound[ref.ID] {
					continue
				} else if ci, ok := candidates[ref.Kind+":"+ref.ID]; ok && !result.Objects[ci].Keep {
					continue
				}
				o.Referrers = append(o.Referrers, CanvasRemoveReferrer{Kind: ref.Kind, ID: ref.ID, Name: ref.Name})
			}
			if reader, ok := s.Policies.(PolicyReferences); ok {
				used, e := reader.UsesProxyTarget(ctx, o.ID)
				if e != nil {
					return result, e
				}
				if used {
					o.Referrers = append(o.Referrers, CanvasRemoveReferrer{Kind: "policy", ID: o.ID, Name: "引用此对象的分流策略"})
				}
			}
			if len(o.Referrers) > 0 {
				o.Keep = true
				changed = true
			}
		}
	}

	nodes := map[string]bool{}
	for _, entry := range result.Entries {
		nodes[entry.NodeID] = true
	}
	for _, object := range result.Objects {
		if !object.Keep && object.NodeID != "" {
			nodes[object.NodeID] = true
		}
	}
	for id := range nodes {
		result.Nodes = append(result.Nodes, id)
	}
	sort.Strings(result.Nodes)
	return result, nil
}

// Draft object references and linked relay rules are outside canvasReferrers'
// proxy-object graph, but remain consumers when a tunnel is removed.
func (s *Service) canvasRemoveExtraReferrers(ctx context.Context, schemeID, kind, objectID string) ([]CanvasRemoveReferrer, error) {
	rows, err := s.Repository.Database.QueryContext(ctx, `SELECT DISTINCT 'scheme',s.id,s.name FROM canvas_schemes s,json_each(s.layout_json,'$.instances') i WHERE s.id<>? AND json_extract(i.value,'$.ref')=?
UNION ALL SELECT 'tunnel_rule',id,name FROM tunnel_rules WHERE ?='inbound' AND target_inbound_id=?`, schemeID, kind+":"+objectID, kind, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := []CanvasRemoveReferrer{}
	for rows.Next() {
		var ref CanvasRemoveReferrer
		if err = rows.Scan(&ref.Kind, &ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
