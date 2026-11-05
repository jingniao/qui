package proxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription/format/uri"
	subproxy "proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source/published"
	"proxycontrol/internal/panel/user"
	"proxycontrol/internal/platform/httpapi"
)

type NodeRelay struct {
	Rule      TunnelRule `json:"rule"`
	ShareLink *string    `json:"shareLink"`
}
type NodeRelayInput struct {
	TunnelID            string `json:"tunnelId"`
	InboundID           string `json:"inboundId"`
	PortMode            string `json:"portMode"`
	ListenPort          *int   `json:"listenPort,omitempty"`
	SubscriptionVisible bool   `json:"subscriptionVisible"`
	Name                string `json:"name,omitempty"`
}

// RelayProxies is wired to the same source used by subscriptions. It never
// invents credentials for an Owner preview.
type RelayProxies interface {
	Proxies(context.Context, string, string) ([]subproxy.Proxy, []subproxy.Warning, error)
}

func (r *Repository) NodeRelayRules(ctx context.Context, nodeID string) ([]TunnelRule, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT "+managedRuleColumns+managedRuleFrom+" JOIN inbounds i ON i.id=r.target_inbound_id WHERE i.node_id=? ORDER BY r.created_at,r.id", nodeID)
	if err != nil {
		return nil, err
	}
	items := []TunnelRule{}
	for rows.Next() {
		item, e := scanManagedRule(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Target, err = deriveTunnelTarget(ctx, r.Database, items[i].Target, items[i].TunnelID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) CreateNodeRelay(ctx context.Context, nodeID string, input NodeRelayInput) (TunnelRule, error) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	n, err := s.Nodes.Get(ctx, nodeID)
	if errors.Is(err, node.ErrNotFound) {
		return TunnelRule{}, ErrNotFound
	}
	if err != nil {
		return TunnelRule{}, err
	}
	tunnel, err := s.Repository.Tunnel(ctx, input.TunnelID)
	if err != nil {
		return TunnelRule{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = n.Name + " 经 " + tunnel.Name
	}
	return s.saveTunnelRule(ctx, "", TunnelRuleInput{Name: name, EntryNodeID: tunnel.Hops[0].NodeID, PortMode: input.PortMode, ListenPort: input.ListenPort, Network: "both", TunnelID: &tunnel.ID, Target: TunnelTarget{Mode: "linked", InboundID: input.InboundID}, SubscriptionVisible: input.SubscriptionVisible, Enabled: true}, &nodeID)
}

func (r *Repository) relaySnapshot(ctx context.Context, q portDatabase, rule TunnelRule) (published.Relay, error) {
	relay := published.Relay{ID: rule.ID, InboundID: rule.Target.InboundID, Entry: subproxy.Ref{ID: rule.EntryNodeID, Name: rule.EntryNodeName}, Port: *rule.ListenPort, Network: rule.Network, DestinationPort: rule.Target.Port, Enabled: rule.Enabled, Visible: rule.SubscriptionVisible}
	err := q.QueryRowContext(ctx, "SELECT public_ip FROM nodes WHERE id=?", rule.EntryNodeID).Scan(&relay.Server)
	if err == nil && rule.Target.Mode == "linked" {
		err = q.QueryRowContext(ctx, "SELECT n.public_ip FROM inbounds i JOIN nodes n ON n.id=i.node_id WHERE i.id=?", rule.Target.InboundID).Scan(&relay.DestinationAddress)
	}
	return relay, err
}
func (r *Repository) saveRelaySnapshot(ctx context.Context, tx *sql.Tx, relay published.Relay) error {
	data, err := json.Marshal(relay)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE tunnel_rules SET published_snapshot=? WHERE id=?", string(data), relay.ID)
	return err
}

func (h *Handler) nodeRelay(ctx context.Context, rule TunnelRule, owner bool) (NodeRelay, error) {
	result := NodeRelay{Rule: rule}
	if !owner || h.RelayProxies == nil {
		return result, nil
	}
	rows, err := h.Service.Repository.Database.QueryContext(ctx, "SELECT id FROM users ORDER BY id")
	if err != nil {
		return result, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	relay, err := h.Service.Repository.relaySnapshot(ctx, h.Service.Repository.Database, rule)
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		endpoints, _, e := h.RelayProxies.Proxies(ctx, id, "")
		if errors.Is(e, user.ErrInactive) || errors.Is(e, user.ErrCredential) || errors.Is(e, user.ErrNotFound) {
			continue
		}
		if e != nil {
			return result, e
		}
		for _, endpoint := range endpoints {
			if endpoint.Origin.Source != "published" || endpoint.Origin.Ref != rule.Target.InboundID {
				continue
			}
			endpoint, ok := published.RelayEndpoint(endpoint, relay)
			if !ok {
				return result, nil
			}
			link, e := uri.Link(endpoint)
			if errors.Is(e, uri.ErrUnrepresentable) {
				return result, nil
			}
			if e != nil {
				return result, e
			}
			result.ShareLink = &link
			return result, nil
		}
	}
	return result, nil
}

func (h *Handler) listNodeRelays(w http.ResponseWriter, r *http.Request) {
	h.Service.canvasMu.Lock()
	defer h.Service.canvasMu.Unlock()
	nodeID := r.PathValue("id")
	if err := portNode(r.Context(), h.Service.Repository.Database, nodeID); err != nil {
		h.failTunnel(w, err)
		return
	}
	rules, err := h.Service.Repository.NodeRelayRules(r.Context(), nodeID)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	items := []NodeRelay{}
	for _, rule := range rules {
		item, e := h.nodeRelay(r.Context(), rule, auth.Actor(r.Context()).Role == "Owner")
		if e != nil {
			h.failTunnel(w, e)
			return
		}
		items = append(items, item)
	}
	httpapi.JSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) createNodeRelay(w http.ResponseWriter, r *http.Request) {
	var input NodeRelayInput
	if err := httpapi.Decode(w, r, &input); err != nil {
		h.fail(w, err)
		return
	}
	rule, err := h.Service.CreateNodeRelay(r.Context(), r.PathValue("id"), input)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	if !h.tunnelAudit(w, r, "创建节点中转", rule.ID) {
		return
	}
	item, err := h.nodeRelay(r.Context(), rule, true)
	if err != nil {
		h.failTunnel(w, err)
		return
	}
	httpapi.JSON(w, 201, item)
}
