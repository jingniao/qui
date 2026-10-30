package portal

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"proxycontrol/internal/panel/access"
	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/subscription"
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

func (s *Service) GrantedNodes(ctx context.Context, id string) ([]Node, error) {
	u, err := s.Users.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	nodes, err := s.Nodes.ExpandedNodes(ctx, u.NodeGroupIDs)
	if err != nil {
		return nil, err
	}
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
		items = append(items, Node{ID: n.ID, Name: n.Name, Region: meta.Region, Online: n.Status == "online"})
	}
	return items, nil
}
