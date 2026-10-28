package subscription

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/clash"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/importer"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/rules"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
)

// timestampLayout is the panel's stored time format.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// defaultUpdateHours is the profile-update-interval a subscription reports until
// the Owner sets another value.
const defaultUpdateHours = 12

// Users resolves the account and the device a subscription token belongs to.
// Since v1.1 the device is the user's hidden access identity.
type Users interface {
	Get(context.Context, string) (user.User, error)
	Device(context.Context, string) (user.Device, error)
	AccessIdentity(context.Context, string) (user.Device, error)
}

// identity resolves the device a request means: the given one, or the user's
// access identity when none is given (every v1.1 caller).
func (s *Service) identity(ctx context.Context, userID, deviceID string) (string, error) {
	if deviceID != "" {
		return deviceID, nil
	}
	device, err := s.Users.AccessIdentity(ctx, userID)
	if err != nil {
		return "", err
	}
	return device.ID, nil
}

// Settings reports the Owner's subscription settings.
type Settings interface {
	SubscriptionPolicy(context.Context) (format.Policy, error)
}

// Traffic reports the recorded upload/download split of an account. It is
// optional: without it the subscription header reports no attributable traffic.
type Traffic interface {
	AccountUsage(ctx context.Context, userID string) (upload, download, samples int64, err error)
}

// ErrFormat reports a format ID that no exporter knows.
var ErrFormat = format.ErrUnknown

// ErrSettings reports that the subscription settings could not be read.
var ErrSettings = errors.New("subscription settings unavailable")

// DefaultFormats is the format registry of v1.1: URI list, Base64 URI list, the
// sing-box fragment and the two Clash profiles (mihomo and legacy Clash).
func DefaultFormats() *format.Registry {
	r := format.NewRegistry(uri.URI{}, uri.Base64{}, singbox.Exporter{}, clash.Meta{}, clash.Legacy{})
	for _, p := range importer.Parsers() {
		r.RegisterParser(p)
	}
	return r
}

// Service renders subscriptions.
type Service struct {
	Repository *Repository
	Users      Users
	Sources    *source.Registry
	Formats    *format.Registry
	Settings   Settings
	Traffic    Traffic
	// Rules supplies the client rule template exporters with rules render.
	Rules        RuleTemplates
	ClientChains interface {
		Chains(context.Context, []proxy.Proxy) ([]proxy.Proxy, []proxy.Warning)
	}
}

// RuleTemplates reads the client rule template in force.
type RuleTemplates interface {
	Template(context.Context) (rules.Template, error)
}

// Rendered is one rendered subscription plus everything the HTTP layer needs to
// describe it.
type Rendered struct {
	Body        []byte
	ContentType string
	FileName    string
	Usage       format.Usage
	UpdateHours int
	Warnings    []proxy.Warning
	Token       Token
}

func (s *Service) sources() *source.Registry {
	if s.Sources != nil {
		return s.Sources
	}
	return source.NewRegistry()
}

func (s *Service) formats() *format.Registry {
	if s.Formats != nil {
		return s.Formats
	}
	return DefaultFormats()
}

// Rotate issues the user's one subscription token and revokes the previous one.
// An empty deviceID means the user's access identity.
func (s *Service) Rotate(ctx context.Context, userID, deviceID string) (Token, string, error) {
	u, err := s.Users.Get(ctx, userID)
	if err != nil {
		return Token{}, "", err
	}
	if !u.Active(time.Now()) {
		return Token{}, "", user.ErrInactive
	}
	deviceID, err = s.identity(ctx, userID, deviceID)
	if err != nil {
		return Token{}, "", err
	}
	device, err := s.Users.Device(ctx, deviceID)
	if err != nil {
		return Token{}, "", err
	}
	if device.UserID != userID || device.Status != "active" {
		return Token{}, "", user.ErrCredential
	}
	return s.Repository.Rotate(ctx, userID, deviceID)
}

// Proxies returns the client proxies of one account, together with the warnings of
// every source.
func (s *Service) Proxies(ctx context.Context, userID, deviceID string) ([]proxy.Proxy, []proxy.Warning, error) {
	_, viewer, err := s.account(ctx, userID, deviceID)
	if err != nil {
		return nil, nil, err
	}
	return s.collect(ctx, viewer)
}

func (s *Service) collect(ctx context.Context, viewer source.Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	p, w, err := s.sources().Collect(ctx, viewer)
	if err == nil && s.ClientChains != nil {
		var raised []proxy.Warning
		p, raised = s.ClientChains.Chains(ctx, p)
		w = append(w, raised...)
	}
	return p, w, err
}

// account validates the account and its device and reports the source view.
func (s *Service) account(ctx context.Context, userID, deviceID string) (user.User, source.Viewer, error) {
	u, err := s.Users.Get(ctx, userID)
	if err != nil {
		return user.User{}, source.Viewer{}, err
	}
	if !u.Active(time.Now()) {
		return user.User{}, source.Viewer{}, user.ErrInactive
	}
	deviceID, err = s.identity(ctx, userID, deviceID)
	if err != nil {
		return user.User{}, source.Viewer{}, err
	}
	device, err := s.Users.Device(ctx, deviceID)
	if err != nil {
		return user.User{}, source.Viewer{}, err
	}
	if device.UserID != userID || device.Status != "active" {
		return user.User{}, source.Viewer{}, user.ErrCredential
	}
	return u, source.Viewer{UserID: u.ID, DeviceID: device.ID, AllowedProtocols: u.AllowedProtocols, NodeGroupIDs: u.NodeGroupIDs}, nil
}

// policy reads the Owner's subscription settings and applies the defaults.
func (s *Service) policy(ctx context.Context) (format.Policy, error) {
	policy := format.Policy{Default: "uri", UpdateHours: defaultUpdateHours}
	if s.Settings == nil {
		return policy, nil
	}
	value, err := s.Settings.SubscriptionPolicy(ctx)
	if err != nil {
		return format.Policy{}, err
	}
	if value.Default != "" {
		policy.Default = value.Default
	}
	if value.UpdateHours > 0 {
		policy.UpdateHours = value.UpdateHours
	}
	policy.DetectUserAgent = value.DetectUserAgent
	return policy, nil
}

// Render resolves a token and renders the subscription for the requested format,
// the client User-Agent or the Owner default, in that order. providerURL builds
// the link a client fetches a panel service rule from; it carries the token, so
// only the HTTP layer knows it.
func (s *Service) Render(ctx context.Context, plaintext, requested, userAgent string, providerURL func(serviceSetID string) string) (Rendered, error) {
	token, err := s.Repository.Resolve(ctx, plaintext)
	if err != nil {
		return Rendered{}, err
	}
	rendered, err := s.render(ctx, token.UserID, token.DeviceID, requested, userAgent, providerURL)
	if err != nil {
		return Rendered{}, err
	}
	s.markFetched(ctx, token.ID)
	rendered.Token = token
	return rendered, nil
}

// Preview renders the subscription of one account in one format for the Owner,
// with every warning, without a token and without marking any token fetched.
// Panel service rules are listed without a fetchable link, because a link
// would need the user's token.
func (s *Service) Preview(ctx context.Context, userID, deviceID, requested string) (Rendered, error) {
	return s.render(ctx, userID, deviceID, requested, "", nil)
}

func (s *Service) render(ctx context.Context, userID, deviceID, requested, userAgent string, providerURL func(string) string) (Rendered, error) {
	account, viewer, err := s.account(ctx, userID, deviceID)
	if err != nil {
		return Rendered{}, err
	}
	proxies, sourceWarnings, err := s.collect(ctx, viewer)
	if err != nil {
		return Rendered{}, err
	}
	policy, err := s.policy(ctx)
	if err != nil {
		// Keep the cause for the log while the handler answers 503.
		return Rendered{}, fmt.Errorf("%w: %v", ErrSettings, err)
	}
	exporter, resolveWarnings, err := s.formats().Resolve(requested, userAgent, policy)
	if err != nil {
		return Rendered{}, err
	}
	usage := s.usage(ctx, account)
	profile := format.Profile{Proxies: proxies, Usage: usage, ProviderURL: providerURL}
	warnings := append(sourceWarnings, resolveWarnings...)
	if exporter.UsesRules() {
		template, err := s.template(ctx)
		if err != nil {
			return Rendered{}, fmt.Errorf("%w: %v", ErrSettings, err)
		}
		resolved := rules.Expand(template, proxies)
		profile.Rules = &resolved
		warnings = append(warnings, resolved.Warnings...)
	}
	output, err := exporter.Render(profile)
	if err != nil {
		return Rendered{}, err
	}
	warnings = append(warnings, output.Warnings...)
	return Rendered{Body: output.Body, ContentType: exporter.ContentType(), FileName: exporter.FileName(), Usage: usage, UpdateHours: policy.UpdateHours, Warnings: warnings}, nil
}

// template reads the client rule template, or the default preset when the
// panel has no store wired (tests that never save one).
func (s *Service) template(ctx context.Context) (rules.Template, error) {
	if s.Rules == nil {
		return rules.Default(), nil
	}
	return s.Rules.Template(ctx)
}

// usage builds the account usage the headers report.
func (s *Service) usage(ctx context.Context, account user.User) format.Usage {
	usage := format.Usage{Total: account.TrafficLimit}
	if account.ExpiresAt != "" {
		if expires, err := time.Parse(timestampLayout, account.ExpiresAt); err == nil {
			usage.ExpiresAt = expires.Unix()
		}
	}
	if s.Traffic == nil || account.UsedTraffic <= 0 {
		return usage
	}
	upload, download, samples, err := s.Traffic.AccountUsage(ctx, account.ID)
	if err != nil || samples == 0 || upload+download <= 0 {
		return usage
	}
	usage.Upload = splitShare(account.UsedTraffic, upload, upload+download)
	usage.Download = account.UsedTraffic - usage.Upload
	return usage
}

// splitShare returns used*part/whole without overflowing: byte counts of a few
// hundred gigabytes already overflow int64 when multiplied with each other.
func splitShare(used, part, whole int64) int64 {
	product := new(big.Int).Mul(big.NewInt(used), big.NewInt(part))
	return product.Quo(product, big.NewInt(whole)).Int64()
}

// fetchMarkTimeout bounds the last-fetched marker. The marker is informational,
// so a busy writer (a long maintenance transaction, for example) must not delay
// or fail the subscription itself.
const fetchMarkTimeout = 250 * time.Millisecond

// markFetched records that the token was used. It is best effort: the
// subscription is already rendered and the marker only feeds the last-fetched
// column of the token list.
func (s *Service) markFetched(ctx context.Context, tokenID string) {
	markCtx, cancel := context.WithTimeout(ctx, fetchMarkTimeout)
	defer cancel()
	_ = s.Repository.Fetched(markCtx, tokenID)
}
