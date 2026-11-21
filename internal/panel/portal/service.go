package portal

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription"
	"proxycontrol/internal/panel/subscription/availability"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/user"
)

type Service struct {
	Repository    *Repository
	Auth          *auth.Service
	Users         *user.Repository
	Nodes         *node.Repository
	Traffic       *access.Repository
	Subscriptions *subscription.Service
	Devices       *user.DeviceObserver
}

func (s *Service) Me(ctx context.Context, id string) (Profile, error) {
	u, err := s.Users.Get(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	p := Profile{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Status: u.Status, TrafficLimit: u.TrafficLimit, UsedTraffic: u.UsedTraffic, ExpiresAt: u.ExpiresAt, AllowedProtocols: u.AllowedProtocols}
	p.DeviceLimit = &u.DeviceLimit
	if s.Devices != nil {
		view, e := s.Devices.View(ctx, u)
		if e != nil {
			return Profile{}, e
		}
		p.OnlineDevices = view.OnlineDevices
		p.DeviceLimitEnforced = view.Enforced
		p.DeviceLimitUnsupportedNodes = view.UnsupportedNodes
		p.DeviceLimitUDPUnsupported = view.UDPUnsupported
	}
	p.TrafficResetCycle = u.TrafficResetCycle
	if p.TrafficResetCycle == "" {
		p.TrafficResetCycle = "none"
	}
	if s.Auth != nil && s.Auth.Settings != nil {
		location := time.UTC
		if _, zone, e := s.Auth.Settings.ReadSetup(ctx); e == nil {
			if loaded, e := time.LoadLocation(zone); e == nil {
				location = loaded
			}
		}
		if next := user.NextResetAt(u, time.Now(), location); next != "" {
			p.NextTrafficResetAt = &next
		}
	}
	p.Reason = user.InactivityReason(u, time.Now())
	for _, protocol := range u.AllowedProtocols {
		if protocol == "wireguard" {
			p.WireGuardNote = "WireGuard 一次只能在一台设备上使用，且不计入设备上限"
		}
	}
	return p, nil
}

func (s *Service) Session(ctx context.Context, token string) (Session, error) {
	if token == "" || len(token) > 128 {
		return Session{}, auth.ErrUnauthorized
	}
	id, err := s.Repository.SessionUser(ctx, auth.HashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, auth.ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	a, err := s.Repository.Account(ctx, id)
	if errors.Is(err, ErrNotFound) || err == nil && !a.LoginEnabled {
		return Session{}, auth.ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	p, err := s.Me(ctx, id)
	if err != nil {
		return Session{}, err
	}
	expires, err := s.Repository.SessionExpiry(ctx, auth.HashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, auth.ErrUnauthorized
	}
	return Session{IsAuthenticated: true, CurrentUser: p, CSRFToken: auth.CSRF(token), ExpiresAt: expires}, err
}

func (s *Service) Login(ctx context.Context, username, password, ip string) (string, Session, error) {
	username = strings.TrimSpace(username)
	if err := s.Auth.ReserveLogin(ctx, "portal:"+ip, "portal:"+username); err != nil {
		return "", Session{}, err
	}
	a, err := s.Repository.AccountByUsername(ctx, username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", Session{}, err
	}
	hash := auth.DummyPasswordHash()
	if err == nil && a.LoginEnabled && a.PasswordHash != "" {
		hash = a.PasswordHash
	}
	if len(password) > 512 || !auth.VerifyPassword(hash, password) || err != nil || !a.LoginEnabled {
		if auditErr := s.Auth.Audit.Record(ctx, username, "用户登录", "用户门户", "失败", ip); auditErr != nil {
			return "", Session{}, auditErr
		}
		return "", Session{}, auth.ErrCredentials
	}
	duration := s.Auth.SessionDuration
	if settings, ok := s.Auth.Settings.(interface {
		SessionLifetime(context.Context) (time.Duration, error)
	}); ok {
		duration, err = settings.SessionLifetime(ctx)
		if err != nil {
			return "", Session{}, err
		}
	}
	token, err := auth.NewToken()
	if err != nil {
		return "", Session{}, err
	}
	if err := s.Repository.createSession(ctx, auth.HashToken(token), a.ID, a.PasswordHash, time.Now().Add(duration)); err != nil {
		return "", Session{}, err
	}
	view, err := s.Session(ctx, token)
	if err == nil {
		err = s.Repository.TouchLogin(ctx, a.ID)
	}
	if err != nil {
		_ = s.Repository.DeleteSession(ctx, auth.HashToken(token))
		return "", Session{}, err
	}
	_ = s.Auth.ClearLoginAttempts(ctx, "portal:"+ip, "portal:"+username)
	return token, view, nil
}

// directProtocols groups the protocols of the user's own subscription endpoints
// by entry node; relayed and client-chain clones are listed separately.
func directProtocols(endpoints []proxy.Proxy) map[string][]string {
	direct := map[string][]string{}
	for _, endpoint := range endpoints {
		if endpoint.Origin.Relayed || endpoint.ClientChain != "" || endpoint.Protocol == "" {
			continue
		}
		direct[endpoint.Origin.Node.ID] = append(direct[endpoint.Origin.Node.ID], endpoint.Protocol)
	}
	return direct
}

func sortedProtocols(list []string) []string {
	out := append([]string{}, list...)
	sort.Strings(out)
	return slices.Compact(out)
}

func (s *Service) GrantedNodes(ctx context.Context, id string) ([]Node, error) {
	u, err := s.Users.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	nodes, err := s.Nodes.ExpandedNodes(ctx, u.NodeGroupIDs)
	if err != nil {
		return nil, err
	}
	var endpoints []proxy.Proxy
	subscribed := s.Subscriptions != nil
	if subscribed {
		endpoints, _, err = s.Subscriptions.Proxies(ctx, id, "")
		if errors.Is(err, user.ErrInactive) || errors.Is(err, user.ErrCredential) {
			endpoints, err = nil, nil
			subscribed = false
		}
		if err != nil {
			return nil, err
		}
	}
	direct := directProtocols(endpoints)
	now := time.Now()
	items := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		allowed, err := s.Nodes.UserHasNode(ctx, n.ID, id)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		meta, _, err := s.Nodes.Meta(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		status := node.DisplayStatus(n, now)
		items = append(items, Node{ID: n.ID, Name: n.Name, Region: meta.Region, Status: status, Online: displayOnline(status), Protocols: sortedProtocols(direct[n.ID])})
	}
	if subscribed {
		for _, endpoint := range endpoints {
			if !endpoint.Origin.Relayed {
				continue
			}
			status, err := s.relayDisplayStatus(ctx, endpoint, now)
			if err != nil {
				return nil, err
			}
			items = append(items, Node{ID: endpoint.Origin.Ref, Name: endpoint.Name, Status: status, Online: displayOnline(status), Protocols: sortedProtocols([]string{endpoint.Protocol})})
		}
	}
	return items, nil
}

func displayOnline(status string) bool {
	return status == "online" || status == "degraded" || status == "core_error"
}

// The entry and every managed dependency contribute their current display
// state; a recently disconnected downstream node must not leave a relay online.
func (s *Service) relayDisplayStatus(ctx context.Context, endpoint proxy.Proxy, now time.Time) (string, error) {
	dependencies, err := availability.Read(ctx, s.Nodes.Database, "relay:"+endpoint.Origin.Ref)
	if err != nil {
		return "", err
	}
	ids := append([]string{endpoint.Origin.Node.ID}, dependencies.PathNodeIDs...)
	if dependencies.TargetInboundID != "" {
		graph, err := availability.LoadGraph(ctx, s.Nodes.Database, s.Nodes.Vault)
		if err != nil {
			return "", err
		}
		targets, err := graph.Route(dependencies.TargetInboundID)
		if err != nil {
			return "", err
		}
		ids = append(ids, targets...)
	}
	priority := []string{"online", "degraded", "core_error", "error", "applying", "rollback", "pending", "reconnecting", "offline", "maintenance", "disabled", "deleted"}
	status := "online"
	for _, id := range availability.Unique(ids) {
		n, err := s.Nodes.GetForCoreSelection(ctx, id)
		if err != nil {
			return "", err
		}
		current := node.DisplayStatus(n, now)
		if slices.Index(priority, current) > slices.Index(priority, status) {
			status = current
		}
	}
	return status, nil
}
