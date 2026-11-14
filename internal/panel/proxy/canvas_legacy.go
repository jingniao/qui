package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/platform/httpapi"
	"proxycontrol/internal/platform/id"
)

type CanvasLegacyBinding struct {
	ID                      string                 `json:"id"`
	InstanceID              string                 `json:"instanceId"`
	InboundID               string                 `json:"inboundId"`
	Name                    string                 `json:"name"`
	NodeID                  string                 `json:"nodeId"`
	NodeName                string                 `json:"nodeName"`
	Mode                    string                 `json:"mode"`
	OriginalDefaultOutbound string                 `json:"originalDefaultOutbound"`
	CurrentDefaultOutbound  string                 `json:"currentDefaultOutbound"`
	Referrers               []CanvasRemoveReferrer `json:"referrers"`
	inbound                 Inbound
}

type CanvasLegacyTunnel struct {
	CanvasTunnel
	SourceNodeID string                 `json:"sourceNodeId"`
	TargetNodeID string                 `json:"targetNodeId"`
	Referrers    []CanvasRemoveReferrer `json:"referrers"`
}

type CanvasLegacyGroup struct {
	ID              string                 `json:"id"`
	BindingIDs      []string               `json:"bindingIds"`
	TunnelIDs       []string               `json:"tunnelIds"`
	Bindings        []CanvasLegacyBinding  `json:"bindings"`
	Tunnels         []CanvasLegacyTunnel   `json:"tunnels"`
	Referrers       []CanvasRemoveReferrer `json:"referrers"`
	CanReattach     bool                   `json:"canReattach"`
	CanRemove       bool                   `json:"canRemove"`
	Empty           bool                   `json:"empty,omitempty"`
	ReattachReasons []string               `json:"reattachReasons"`
	RemoveReasons   []string               `json:"removeReasons"`
}

type CanvasLegacyResources struct {
	Detached      []CanvasLegacyGroup  `json:"detached"`
	OrphanTunnels []CanvasLegacyGroup  `json:"orphanTunnels"`
	Unowned       []CanvasTunnelObject `json:"unowned"`
}

type CanvasLegacyReattachInput struct {
	BindingIDs []string `json:"bindingIds,omitempty"`
	TunnelIDs  []string `json:"tunnelIds,omitempty"`
	Name       string   `json:"name"`
}

type CanvasLegacyConflict struct {
	Reasons []string `json:"reasons"`
}

func (e *CanvasLegacyConflict) Error() string { return strings.Join(e.Reasons, "；") }

func (h *Handler) canvasLegacyResources(w http.ResponseWriter, r *http.Request) {
	result, err := h.Service.LegacyCanvasResources(r.Context())
	if err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}

func (h *Handler) reattachCanvasLegacy(w http.ResponseWriter, r *http.Request) {
	var input CanvasLegacyReattachInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.failCanvas(w, err)
		return
	}
	scheme, err := h.Service.ReattachCanvasLegacy(r.Context(), input)
	if err != nil {
		var conflict *CanvasLegacyConflict
		if errors.As(err, &conflict) {
			httpapi.ErrorDetails(w, http.StatusConflict, "canvas_legacy_conflict", err.Error(), map[string]any{"reasons": conflict.Reasons})
		} else {
			h.failCanvas(w, err)
		}
		return
	}
	if err = h.Audit.Record(r.Context(), auth.Actor(r.Context()).Username, "重新挂接中转遗留资源", scheme.ID, "成功", r.RemoteAddr); err != nil {
		h.internal(w, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, map[string]string{"schemeId": scheme.ID})
}

func (s *Service) LegacyCanvasResources(ctx context.Context) (CanvasLegacyResources, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	return s.legacyCanvasResources(ctx)
}

func (s *Service) legacyCanvasResources(ctx context.Context) (CanvasLegacyResources, error) {
	result := CanvasLegacyResources{Detached: []CanvasLegacyGroup{}, OrphanTunnels: []CanvasLegacyGroup{}, Unowned: []CanvasTunnelObject{}}
	rows, err := s.Repository.Database.QueryContext(ctx, "SELECT "+canvasEntryBindingColumns+" FROM canvas_entry_bindings WHERE scheme_id IS NULL ORDER BY id")
	if err != nil {
		return result, err
	}
	bindings := []CanvasEntryBindingRecord{}
	for rows.Next() {
		b, e := scanCanvasEntryBinding(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		bindings = append(bindings, b)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	if err = rows.Close(); err != nil {
		return result, err
	}
	tunnels, err := s.Repository.canvasTunnelsReadOnly(ctx)
	if err != nil {
		return result, err
	}
	// Resource vertices connect only through persisted endpoint identities. A
	// detached binding can be represented by its inbound or its server endpoint.
	groups := []CanvasLegacyGroup{}
	keys := [][]string{}
	for _, b := range bindings {
		in, e := s.Repository.Inbound(ctx, b.InboundID, true)
		if e != nil {
			return result, e
		}
		var nodeName string
		if e = s.Repository.Database.QueryRowContext(ctx, "SELECT name FROM nodes WHERE id=?", in.NodeID).Scan(&nodeName); e != nil {
			return result, e
		}
		binding := CanvasLegacyBinding{ID: b.ID, InstanceID: b.InstanceID, InboundID: b.InboundID, Name: in.Name, NodeID: in.NodeID, NodeName: nodeName, Mode: b.Mode, OriginalDefaultOutbound: b.OriginalDefaultOutbound, CurrentDefaultOutbound: in.DefaultOutbound, Referrers: []CanvasRemoveReferrer{}, inbound: in}
		groups = append(groups, CanvasLegacyGroup{BindingIDs: []string{b.ID}, TunnelIDs: []string{}, Bindings: []CanvasLegacyBinding{binding}, Tunnels: []CanvasLegacyTunnel{}})
		keys = append(keys, []string{"inbound:" + b.InboundID, "server:" + in.NodeID})
	}
	for _, t := range tunnels {
		if len(t.Schemes) > 0 {
			continue
		}
		source, e := s.canvasLegacyRefNode(ctx, t.Source)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return result, e
		}
		target, e := s.canvasLegacyRefNode(ctx, t.Target)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return result, e
		}
		groups = append(groups, CanvasLegacyGroup{BindingIDs: []string{}, TunnelIDs: []string{t.ID}, Bindings: []CanvasLegacyBinding{}, Tunnels: []CanvasLegacyTunnel{{CanvasTunnel: t, SourceNodeID: source, TargetNodeID: target, Referrers: []CanvasRemoveReferrer{}}}})
		if len(t.Objects) == 0 {
			keys = append(keys, nil) // Empty historical rows are not resource edges.
		} else {
			keys = append(keys, []string{t.Source, t.Target})
		}
	}
	visited := map[int]bool{}
	for i := range groups {
		if visited[i] {
			continue
		}
		group := CanvasLegacyGroup{BindingIDs: []string{}, TunnelIDs: []string{}, Bindings: []CanvasLegacyBinding{}, Tunnels: []CanvasLegacyTunnel{}, ReattachReasons: []string{}, RemoveReasons: []string{}}
		queue := []int{i}
		visited[i] = true
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			group.BindingIDs = append(group.BindingIDs, groups[n].BindingIDs...)
			group.TunnelIDs = append(group.TunnelIDs, groups[n].TunnelIDs...)
			group.Bindings = append(group.Bindings, groups[n].Bindings...)
			group.Tunnels = append(group.Tunnels, groups[n].Tunnels...)
			for j := range groups {
				if visited[j] {
					continue
				}
				connected := false
				for _, a := range keys[n] {
					for _, b := range keys[j] {
						if a == b {
							connected = true
						}
					}
				}
				if connected {
					visited[j] = true
					queue = append(queue, j)
				}
			}
		}
		if len(group.BindingIDs) > 0 {
			group.ID = "binding:" + group.BindingIDs[0]
		} else {
			group.ID = "tunnel:" + group.TunnelIDs[0]
		}
		group.Empty = len(group.Bindings) == 0 && len(group.Tunnels) == 1 && len(group.Tunnels[0].Objects) == 0
		if group.Empty {
			group.ReattachReasons = append(group.ReattachReasons, "空通道已无运行对象，可直接清除")
		} else {
			_, group.ReattachReasons, err = s.canvasLegacyLayout(ctx, group)
			if err != nil {
				return result, err
			}
			group.CanReattach = len(group.ReattachReasons) == 0
			group.RemoveReasons = append(group.RemoveReasons, group.ReattachReasons...)
		}
		if err = s.canvasLegacyReferrers(ctx, &group); err != nil {
			return result, err
		}
		group.CanRemove = len(group.RemoveReasons) == 0
		if len(group.Bindings) > 0 {
			result.Detached = append(result.Detached, group)
		} else {
			result.OrphanTunnels = append(result.OrphanTunnels, group)
		}
	}
	result.Unowned, err = s.Repository.legacyCanvasObjectsReadOnly(ctx)
	return result, err
}

func (s *Service) canvasLegacyReferrers(ctx context.Context, g *CanvasLegacyGroup) error {
	g.Referrers = []CanvasRemoveReferrer{}
	internal := map[string]bool{}
	for _, b := range g.Bindings {
		internal["inbound:"+b.InboundID] = true
	}
	for _, t := range g.Tunnels {
		for _, o := range t.Objects {
			internal[o.Kind+":"+o.ID] = true
		}
	}
	collect := func(kind, objectID string) ([]CanvasRemoveReferrer, error) {
		refs, err := s.Repository.canvasReferrers(ctx, objectID)
		if err != nil {
			return nil, err
		}
		out := []CanvasRemoveReferrer{}
		for _, r := range refs {
			if !internal[r.Kind+":"+r.ID] {
				out = append(out, CanvasRemoveReferrer{Kind: r.Kind, ID: r.ID, Name: r.Name})
			}
		}
		extra, err := s.canvasRemoveExtraReferrers(ctx, "", kind, objectID)
		if err != nil {
			return nil, err
		}
		out = append(out, extra...)
		if reader, ok := s.Policies.(PolicyReferences); ok {
			used, err := reader.UsesProxyTarget(ctx, objectID)
			if err != nil {
				return nil, err
			}
			if used {
				out = append(out, CanvasRemoveReferrer{Kind: "policy", ID: objectID, Name: "引用此对象的分流策略"})
			}
		}
		return out, nil
	}
	for i := range g.Bindings {
		b := &g.Bindings[i]
		if b.Mode != "create" {
			continue
		}
		refs, err := collect("inbound", b.InboundID)
		if err != nil {
			return err
		}
		b.Referrers = refs
		g.Referrers = append(g.Referrers, refs...)
		for _, ref := range refs {
			g.RemoveReasons = append(g.RemoveReasons, "入口「"+b.Name+"」仍被「"+ref.Name+"」引用")
		}
	}
	for i := range g.Tunnels {
		t := &g.Tunnels[i]
		for _, o := range t.Objects {
			refs, err := collect(o.Kind, o.ID)
			if err != nil {
				return err
			}
			t.Referrers = append(t.Referrers, refs...)
		}
		rows, err := s.Repository.Database.QueryContext(ctx, "SELECT s.id,s.name FROM canvas_tunnel_publications p JOIN canvas_schemes s ON s.id=p.scheme_id WHERE p.tunnel_id=? ORDER BY s.id", t.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			ref := CanvasRemoveReferrer{Kind: "scheme"}
			if err = rows.Scan(&ref.ID, &ref.Name); err != nil {
				rows.Close()
				return err
			}
			t.Referrers = append(t.Referrers, ref)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		for _, ref := range t.Referrers {
			g.RemoveReasons = append(g.RemoveReasons, "通道「"+t.Name+"」仍被「"+ref.Name+"」引用")
		}
		g.Referrers = append(g.Referrers, t.Referrers...)
	}
	return nil
}

// A malformed endpoint remains visible with a reconstruction reason. Database
// failures still propagate; endpoint existence is checked by layout validation.
func (s *Service) canvasLegacyRefNode(ctx context.Context, raw string) (string, error) {
	if _, err := parseCanvasRef(raw); err != nil {
		return "", nil
	}
	return s.canvasRemoveRefNode(ctx, raw)
}

// Reconstruct exact references, rejecting ambiguities instead of inventing an
// edge or using the current route to guess which tunnel used to be selected.
func (s *Service) canvasLegacyLayout(ctx context.Context, g CanvasLegacyGroup) (CanvasLayout, []string, error) {
	layout := CanvasLayout{Instances: []CanvasInstance{}, Connections: []CanvasConnection{}}
	reasons := []string{}
	refs := map[string]bool{}
	ins, outs := map[string]int{}, map[string]int{}
	for _, t := range g.Tunnels {
		refs[t.Source] = true
		refs[t.Target] = true
		outs[t.Source]++
		ins[t.Target]++
	}
	instances := map[string]CanvasInstance{}
	ids := map[string]bool{}
	for _, b := range g.Bindings {
		ref := "inbound:" + b.InboundID
		server := "server:" + b.NodeID
		if refs[ref] && refs[server] {
			reasons = append(reasons, "入口「"+b.Name+"」同时匹配入站与服务器端点，无法确定原布局")
			continue
		}
		if refs[server] {
			ref = server
		}
		if _, exists := instances[ref]; exists {
			reasons = append(reasons, "多个入口绑定匹配同一端点："+ref)
			continue
		}
		if ids[b.InstanceID] {
			reasons = append(reasons, "入口实例 ID 重复："+b.InstanceID)
			continue
		}
		if ins[ref] > 0 {
			reasons = append(reasons, "入口「"+b.Name+"」位于通道中间，无法还原入口方向")
		}
		instance := CanvasInstance{InstanceID: b.InstanceID, Ref: ref, Role: "entry"}
		if ref == server {
			binding := &CanvasEntryBinding{Mode: b.Mode, PortMode: "manual", Port: &b.inbound.Port, GroupMode: "none"}
			if b.Mode == "create" {
				binding.Protocol = b.inbound.Protocol
				binding.Core = b.inbound.Core
			} else {
				binding.InboundID = b.InboundID
			}
			instance.EntryBinding = binding
		}
		instances[ref] = instance
		ids[b.InstanceID] = true
		refs[ref] = true
	}
	ordered := make([]string, 0, len(refs))
	for ref := range refs {
		ordered = append(ordered, ref)
	}
	sort.Strings(ordered)
	for n, ref := range ordered {
		if ins[ref] > 1 || outs[ref] > 1 {
			reasons = append(reasons, "端点存在分叉或汇合："+ref)
		}
		instance, exists := instances[ref]
		if !exists {
			instanceID := fmt.Sprintf("legacy-%d", n)
			for ids[instanceID] {
				instanceID += "-node"
			}
			role := "relay"
			if ins[ref] == 0 {
				role = "entry"
			} else if outs[ref] == 0 {
				role = "exit"
			}
			instance = CanvasInstance{InstanceID: instanceID, Ref: ref, Role: role}
			instances[ref] = instance
			ids[instanceID] = true
		}
		instance.X = float64(n) * 280
		layout.Instances = append(layout.Instances, instance)
		if strings.HasPrefix(ref, "import:") {
			reasons = append(reasons, "导入节点端点不能直接还原为服务端通道："+ref)
		}
		if err := s.validateRefs(ctx, CanvasLayout{Instances: []CanvasInstance{instance}}); err != nil {
			reasons = append(reasons, "端点不存在或不可用（"+ref+"）："+err.Error())
		}
	}
	for _, t := range g.Tunnels {
		c := t.connection
		c.ID = t.ID
		c.TunnelID = t.ID
		c.Source = instances[t.Source].InstanceID
		c.Target = instances[t.Target].InstanceID
		layout.Connections = append(layout.Connections, c)
	}
	if len(g.Tunnels) > 0 {
		remaining := map[string]int{}
		queue := []string{}
		for ref := range refs {
			remaining[ref] = ins[ref]
			if ins[ref] == 0 {
				queue = append(queue, ref)
			}
		}
		visited := 0
		for len(queue) > 0 {
			ref := queue[0]
			queue = queue[1:]
			visited++
			for _, t := range g.Tunnels {
				if t.Source == ref {
					remaining[t.Target]--
					if remaining[t.Target] == 0 {
						queue = append(queue, t.Target)
					}
				}
			}
		}
		if visited != len(refs) {
			reasons = append(reasons, "通道端点形成环路，无法还原合法布局")
		}
	}
	if err := layout.validate(); err != nil {
		reasons = append(reasons, "布局无法还原："+err.Error())
	}
	if len(reasons) == 0 {
		if err := s.validateCanvasExits(ctx, layout); err != nil {
			reasons = append(reasons, "出口无法还原："+err.Error())
		}
	}
	return layout, reasons, nil
}

func (s *Service) ReattachCanvasLegacy(ctx context.Context, input CanvasLegacyReattachInput) (CanvasScheme, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > canvasMaxNameBytes || strings.ContainsAny(name, "[]") {
		return CanvasScheme{}, errors.New("方案名称须为 1–80 字节且不能包含方括号")
	}
	if len(input.BindingIDs)+len(input.TunnelIDs) == 0 {
		return CanvasScheme{}, errors.New("请选择遗留资源")
	}
	resources, err := s.legacyCanvasResources(ctx)
	if err != nil {
		return CanvasScheme{}, err
	}
	requested := map[string]bool{}
	for _, v := range input.BindingIDs {
		key := "binding:" + v
		if requested[key] {
			return CanvasScheme{}, errors.New("入口绑定重复")
		}
		requested[key] = true
	}
	for _, v := range input.TunnelIDs {
		key := "tunnel:" + v
		if requested[key] {
			return CanvasScheme{}, errors.New("通道重复")
		}
		requested[key] = true
	}
	selected := CanvasLegacyGroup{Bindings: []CanvasLegacyBinding{}, Tunnels: []CanvasLegacyTunnel{}}
	component := ""
	for _, g := range append(resources.Detached, resources.OrphanTunnels...) {
		matched := false
		for _, b := range g.Bindings {
			if requested["binding:"+b.ID] {
				selected.Bindings = append(selected.Bindings, b)
				delete(requested, "binding:"+b.ID)
				matched = true
			}
		}
		for _, t := range g.Tunnels {
			if requested["tunnel:"+t.ID] {
				selected.Tunnels = append(selected.Tunnels, t)
				delete(requested, "tunnel:"+t.ID)
				matched = true
			}
		}
		if matched {
			if component != "" {
				return CanvasScheme{}, errors.New("所选资源必须构成一个连通组")
			}
			component = g.ID
		}
	}
	if len(requested) > 0 {
		return CanvasScheme{}, &CanvasLegacyConflict{Reasons: []string{"所选资源已不存在、已挂接或仍被现存方案布局引用"}}
	}
	// A subset of a connected component can still have a gap. Check only the
	// selected persisted edges and binding identities, not unselected resources.
	if !canvasLegacyConnected(selected) {
		return CanvasScheme{}, errors.New("所选资源必须构成一个连通组")
	}
	layout, reasons, err := s.canvasLegacyLayout(ctx, selected)
	if err != nil {
		return CanvasScheme{}, err
	}
	if len(reasons) > 0 {
		return CanvasScheme{}, &CanvasLegacyConflict{Reasons: reasons}
	}
	if taken, e := s.Repository.canvasSchemeNameTaken(ctx, name, ""); e != nil {
		return CanvasScheme{}, e
	} else if taken {
		return CanvasScheme{}, ErrDuplicateSchemeName
	}
	schemeID, err := id.New()
	if err != nil {
		return CanvasScheme{}, err
	}
	raw, err := json.Marshal(layout)
	if err != nil {
		return CanvasScheme{}, err
	}
	tx, err := s.Repository.Database.BeginTx(ctx, nil)
	if err != nil {
		return CanvasScheme{}, err
	}
	defer tx.Rollback()
	stamp := timestamp()
	if _, err = tx.ExecContext(ctx, "INSERT INTO canvas_schemes(id,name,execution,layout_json,created_at,updated_at) VALUES(?,?,'server',?,?,?)", schemeID, name, string(raw), stamp, stamp); err != nil {
		return CanvasScheme{}, err
	}
	for _, b := range selected.Bindings {
		result, e := tx.ExecContext(ctx, "UPDATE canvas_entry_bindings SET scheme_id=?,state='applied',updated_at=? WHERE id=? AND scheme_id IS NULL", schemeID, stamp, b.ID)
		if e != nil {
			return CanvasScheme{}, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return CanvasScheme{}, e
		}
		if n != 1 {
			return CanvasScheme{}, &CanvasLegacyConflict{Reasons: []string{"入口绑定已被其他方案挂接：" + b.ID}}
		}
	}
	// Deleting a drawing cascades its publication association. Preserve known
	// published channel ownership for removal even if the new draft drops an edge.
	for _, t := range selected.Tunnels {
		if t.publishedVersion != "" {
			if _, err = tx.ExecContext(ctx, "INSERT INTO canvas_tunnel_publications(scheme_id,tunnel_id) VALUES(?,?)", schemeID, t.ID); err != nil {
				return CanvasScheme{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return CanvasScheme{}, err
	}
	return s.Repository.CanvasScheme(ctx, schemeID)
}

func canvasLegacyConnected(g CanvasLegacyGroup) bool {
	adj := map[string][]string{}
	link := func(a, b string) { adj[a] = append(adj[a], b); adj[b] = append(adj[b], a) }
	for _, t := range g.Tunnels {
		link(t.Source, t.Target)
	}
	for _, b := range g.Bindings {
		link("inbound:"+b.InboundID, "server:"+b.NodeID)
	}
	queue := []string{}
	for ref := range adj {
		queue = append(queue, ref)
		break
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if seen[ref] {
			continue
		}
		seen[ref] = true
		queue = append(queue, adj[ref]...)
	}
	return len(seen) == len(adj)
}
