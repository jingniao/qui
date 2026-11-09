package certificate

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"proxycontrol/internal/platform/id"
)

// Auditor is the panel operation log. RecordDaily writes at most one row for a
// repeating condition per UTC day and reports whether it wrote one, which is what
// makes the expiry alert a daily reminder instead of a per-attempt one.
type Auditor interface {
	Record(ctx context.Context, operator, action, target, result, ip string) error
	RecordDaily(ctx context.Context, operator, action, target string) (bool, error)
}

// Bounds of the worker. A single issuance has to finish well inside the renewal
// tick, and the queue only has to absorb the requests of one Owner session.
const (
	jobTimeout    = 10 * time.Minute
	queueCapacity = 64
)

// Service issues certificates and is the API's entry point. It queues work for
// one goroutine: Run owns that goroutine, the panel's wait group covers it, and a
// cancelled context stops it on shutdown and on in-process restore.
type Service struct {
	Repository  *Repository
	Issuer      Issuer
	DNS         DNS
	Propagation Propagation
	Audit       Auditor
	// Publish emits SSE events; only system.alert is ever published here.
	Publish func(kind string, data any)
	// Renewed runs after a successful issuance or renewal. Phase D2 wires it to
	// the deployment module so the affected nodes republish; in D1 it is nil.
	Renewed  func(ctx context.Context, certificateID string)
	Logger   *slog.Logger
	Now      func() time.Time
	Interval time.Duration

	jobs    chan string
	pending map[string]struct{}
	running bool
	mu      sync.Mutex
}

// NewService returns a service with an empty queue. Every dependency is set by
// the caller, so the platform wires the whole module in one place.
func NewService(repository *Repository) *Service {
	return &Service{Repository: repository, jobs: make(chan string, queueCapacity), pending: map[string]struct{}{}}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// warn logs one line without a payload: failures here must never mirror upstream
// text, and the code that reached the row already says what happened.
func (s *Service) warn(message string, err error) {
	if s.Logger != nil && err != nil {
		s.Logger.Warn(message, "error", err)
	}
}

// Enqueue asks the worker to process one certificate. It never blocks: a full
// queue just means the next sweep picks the certificate up again.
func (s *Service) Enqueue(certificateID string) {
	if s == nil || s.jobs == nil {
		return
	}
	s.mu.Lock()
	if _, queued := s.pending[certificateID]; queued {
		s.mu.Unlock()
		return
	}
	s.pending[certificateID] = struct{}{}
	s.mu.Unlock()
	select {
	case s.jobs <- certificateID:
	default:
		s.mu.Lock()
		delete(s.pending, certificateID)
		s.mu.Unlock()
	}
}

// SettingsView returns the stored settings with what may be said about the token.
func (s *Service) SettingsView(ctx context.Context) (SettingsView, error) {
	settings, err := s.Repository.ReadSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	_, cloudflare, err := s.Repository.ReadCloudflare(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{Email: settings.Email, Directory: settings.Directory, DeployStartHour: settings.DeployStartHour, DeployEndHour: settings.DeployEndHour, Revision: settings.Revision, Cloudflare: cloudflare}, nil
}

// SaveSettings verifies a replacement token with Cloudflare before storing it, so
// a token that can never publish a record is refused instead of saved.
func (s *Service) SaveSettings(ctx context.Context, input SettingsInput, actor, ip string) (SettingsView, bool, error) {
	verifiedAt := ""
	if input.CloudflareToken != nil && *input.CloudflareToken != "" {
		if err := s.DNS.VerifyToken(ctx, *input.CloudflareToken); err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				return SettingsView{}, false, err
			case failureCode(err) == CodeCloudflareTokenInvalid:
				return SettingsView{}, false, ErrTokenInvalid
			}
			return SettingsView{}, false, ErrTokenUnverified
		}
		verifiedAt = Stamp(s.now())
	}
	_, changed, err := s.Repository.SaveSettings(ctx, SaveSettingsInput{Email: input.Email, Directory: input.Directory,
		DeployStartHour: input.DeployStartHour, DeployEndHour: input.DeployEndHour, ExpectedRevision: input.ExpectedRevision, Token: input.CloudflareToken, VerifiedAt: verifiedAt, Actor: actor, IP: ip})
	if err != nil {
		return SettingsView{}, false, err
	}
	view, err := s.SettingsView(ctx)
	return view, changed, err
}

// List renders every certificate with the inbounds that reference it.
func (s *Service) List(ctx context.Context) ([]View, error) {
	items, err := s.Repository.List(ctx)
	if err != nil {
		return nil, err
	}
	usages, err := s.Repository.Usages(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(items))
	for _, item := range items {
		views = append(views, NewView(item, usages[item.ID]))
	}
	return views, nil
}

// Create stores a pending request and queues it. The settings must already carry
// an email and a token, because a job can neither register an account nor write a
// challenge record without them.
func (s *Service) Create(ctx context.Context, input CreateInput, actor, ip string) (View, error) {
	domains, err := NormalizeDomains(input.Domains)
	if err != nil {
		return View{}, err
	}
	nodeID := ""
	if input.AutoDNS {
		nodeID = strings.TrimSpace(input.DNSNodeID)
		if nodeID == "" {
			return View{}, ErrDomains
		}
		if _, err := s.Repository.NodeAddress(ctx, nodeID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return View{}, ErrNodeUnknown
			}
			return View{}, err
		}
	}
	settings, err := s.Repository.ReadSettings(ctx)
	if err != nil {
		return View{}, err
	}
	token, cloudflare, err := s.Repository.ReadCloudflare(ctx)
	if err != nil {
		return View{}, err
	}
	if settings.Email == "" || !cloudflare.Configured || token == "" {
		return View{}, ErrSettingsMissing
	}
	identifier, err := id.New()
	if err != nil {
		return View{}, err
	}
	stamp := Stamp(s.now())
	item := Certificate{ID: identifier, Domains: domains, Status: StatusPending, AutoDNS: input.AutoDNS, DNSNodeID: nodeID, CreatedAt: stamp, UpdatedAt: stamp}
	if err := s.Repository.Create(ctx, item); err != nil {
		return View{}, err
	}
	s.record(ctx, actor, "申请证书", identifier, ip)
	s.Enqueue(identifier)
	return NewView(item, nil), nil
}

// Renew queues a renewal now. The row keeps its status until the worker starts,
// so a certificate that is still serving its material never looks broken while it
// waits for a free worker.
func (s *Service) Renew(ctx context.Context, certificateID, actor, ip string) (View, error) {
	item, err := s.Repository.Get(ctx, certificateID)
	if err != nil {
		return View{}, err
	}
	usages, err := s.Repository.Usages(ctx)
	if err != nil {
		return View{}, err
	}
	s.record(ctx, actor, "续期证书", certificateID, ip)
	s.Enqueue(certificateID)
	return NewView(item, usages[certificateID]), nil
}

// Delete removes a certificate no inbound references. Phase D2 makes the
// reference real; until then the check keeps the endpoint honest.
func (s *Service) Delete(ctx context.Context, certificateID, actor, ip string) error {
	if _, err := s.Repository.Get(ctx, certificateID); err != nil {
		return err
	}
	usages, err := s.Repository.Usages(ctx)
	if err != nil {
		return err
	}
	if used := usages[certificateID]; len(used) > 0 {
		return &InUseError{Inbounds: used}
	}
	if err := s.Repository.Delete(ctx, certificateID); err != nil {
		return err
	}
	s.record(ctx, actor, "删除证书", certificateID, ip)
	return nil
}

// record writes one audit row. A failing audit must not undo a committed change,
// so the error only reaches the log.
func (s *Service) record(ctx context.Context, actor, action, target, ip string) {
	if s.Audit == nil {
		return
	}
	if err := s.Audit.Record(ctx, actor, action, target, "成功", ip); err != nil {
		s.warn("certificate audit entry failed", err)
	}
}

// CertificateMaterial is the proxy module's view of the store: the PEM pair a
// certificate holds, whether the row exists, and an empty pair while nothing has
// been issued. A renewal in flight keeps the previous pair, so an inbound never
// loses its certificate while the CA works.
func (s *Service) CertificateMaterial(ctx context.Context, certificateID string) (certPEM, keyPEM string, exists bool, err error) {
	material, err := s.Repository.Material(ctx, certificateID)
	if errors.Is(err, ErrNotFound) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return material.CertificatePEM, material.PrivateKeyPEM, true, nil
}
