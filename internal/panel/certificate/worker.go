package certificate

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Windows the scheduler works with: a renewal starts 30 days before expiry, and a
// renewal that keeps failing becomes an Owner alert 7 days before expiry.
const (
	renewalWindow = 30 * 24 * time.Hour
	alertWindow   = 7 * 24 * time.Hour
)

// Run starts the single worker. It turns the run a previous process left in
// flight into a failed row, sweeps once right away, then sweeps hourly and
// processes one certificate at a time until the context is cancelled. The panel's
// wait group covers this call, so shutdown and in-process restore both end it.
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	if err := s.Repository.MarkInterrupted(ctx, s.now()); err != nil && ctx.Err() == nil {
		s.warn("certificate interruption sweep failed", err)
	}
	interval := s.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweep(ctx)
		case certificateID := <-s.jobs:
			s.process(ctx, certificateID)
		}
	}
}

// sweep queues every certificate that is due. The due check parses the stored
// timestamps instead of comparing their text, because RFC 3339 with nanoseconds
// is not fixed width.
func (s *Service) sweep(ctx context.Context) {
	items, err := s.Repository.List(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.warn("certificate sweep failed", err)
		}
		return
	}
	now := s.now()
	for _, item := range items {
		if due(item, now) {
			s.Enqueue(item.ID)
		}
	}
}

func due(item Certificate, now time.Time) bool {
	switch item.Status {
	case StatusPending:
		// A request that never reached the worker, for example because the panel
		// restarted between the insert and the enqueue.
		return true
	case StatusValid:
		expires, ok := item.ExpiresAt()
		if !ok || expires.Sub(now) >= renewalWindow {
			return false
		}
		// A renewal that failed keeps the row valid and records its backoff; the
		// hourly sweep must honour it or the CA's failed-validation limit is hit.
		next, scheduled := parseStamp(item.NextAttemptAt)
		return !scheduled || !next.After(now)
	case StatusFailed:
		// The attempt counter and its backoff are the only schedule a failed row
		// has. Material is not required: an interrupted first issuance has none
		// and still has to be retried.
		next, ok := parseStamp(item.NextAttemptAt)
		return ok && !next.After(now)
	}
	return false
}

// process runs one job under its own timeout and clears the queue mark first, so
// a request that arrives while the job runs is queued again instead of dropped.
func (s *Service) process(ctx context.Context, certificateID string) {
	s.mu.Lock()
	delete(s.pending, certificateID)
	s.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	job, cancel := context.WithTimeout(ctx, jobTimeout)
	defer cancel()
	item, err := s.Repository.Get(job, certificateID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			s.warn("certificate lookup failed", err)
		}
		return
	}
	s.run(job, item)
}

// persist returns a context for recording an outcome even when the job context
// was cancelled by shutdown or by its own timeout: a row must never be left in
// the issuing state.
func persist(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
}

// run is one issuance or renewal from start to finish.
func (s *Service) run(ctx context.Context, item Certificate) {
	now := s.now()
	// A renewal of a certificate that still serves valid material must not take
	// the certificate out of service when it fails.
	keepValid := item.Status == StatusValid && item.HasMaterial
	settings, err := s.Repository.ReadSettings(ctx)
	if err != nil {
		s.failed(ctx, item, keepValid, err, now)
		return
	}
	token, cloudflare, err := s.Repository.ReadCloudflare(ctx)
	if err != nil {
		s.failed(ctx, item, keepValid, err, now)
		return
	}
	switch {
	case settings.Email == "":
		// Without a contact address the CA cannot register an account at all.
		s.failed(ctx, item, keepValid, fail(CodeACMEAccountFailed), now)
		return
	case !cloudflare.Configured || token == "":
		s.failed(ctx, item, keepValid, fail(CodeCloudflareNotConfigured), now)
		return
	}
	if err := s.Repository.MarkIssuing(ctx, item.ID, now); err != nil {
		s.warn("certificate run could not be started", err)
		return
	}
	stored, err := s.Repository.ReadAccount(ctx)
	if err != nil {
		s.failed(ctx, item, keepValid, err, now)
		return
	}
	issued, err := s.issue(ctx, item, settings, token, accountFor(settings, stored))
	if err != nil {
		s.failed(ctx, item, keepValid, err, now)
		return
	}
	saved, cancel := persist(ctx)
	defer cancel()
	if err := s.Repository.SaveIssued(saved, item.ID, issued.Material, Stamp(issued.NotBefore), Stamp(issued.NotAfter), now); err != nil {
		s.failed(saved, item, keepValid, fail(CodeInternal), now)
		return
	}
	if s.Renewed != nil {
		s.Renewed(ctx, item.ID)
	}
}

// issue publishes the challenge records, waits for them to appear on the zone's
// name servers, then accepts the challenges and finalizes the order. The records
// it created are removed on every path, including failure.
func (s *Service) issue(ctx context.Context, item Certificate, settings Settings, token string, account Account) (Issued, error) {
	returned, order, err := s.Issuer.Begin(ctx, BeginRequest{Domains: item.Domains, Email: settings.Email, Directory: settings.Directory, Account: account})
	if err != nil {
		return Issued{}, err
	}
	if err := s.Repository.SaveAccount(ctx, returned); err != nil {
		s.warn("ACME account could not be stored", err)
		return Issued{}, fail(CodeACMEAccountFailed)
	}
	created := &challengeRecords{dns: s.DNS, token: token, onError: s.warn}
	defer created.delete(ctx)
	zones := map[string]Zone{}
	findZone := func(domain string) (Zone, error) {
		if cached, ok := zones[domain]; ok {
			return cached, nil
		}
		found, err := s.DNS.Zone(ctx, token, domain)
		if err != nil {
			return Zone{}, err
		}
		zones[domain] = found
		return found, nil
	}
	if item.AutoDNS {
		address, err := s.Repository.NodeAddress(ctx, item.DNSNodeID)
		if err != nil || address == "" {
			return Issued{}, fail(CodeDNSNodeAddressMissing)
		}
		for _, domain := range item.Domains {
			// A wildcard has no address of its own; a client resolves the base
			// name it stands for.
			if strings.HasPrefix(domain, "*.") {
				continue
			}
			zone, err := findZone(domain)
			if err != nil {
				return Issued{}, err
			}
			if err := s.DNS.UpsertAddress(ctx, token, zone.ID, domain, address); err != nil {
				return Issued{}, err
			}
		}
	}
	authorizations := order.Authorizations()
	published := make([]challenge, 0, len(authorizations))
	for _, authorization := range authorizations {
		if authorization.Valid {
			continue
		}
		zone, err := findZone(authorization.Domain)
		if err != nil {
			return Issued{}, err
		}
		name := "_acme-challenge." + authorization.Domain
		recordID, err := s.DNS.CreateTXT(ctx, token, zone.ID, name, authorization.Record)
		if err != nil {
			return Issued{}, err
		}
		created.add(zone.ID, recordID)
		published = append(published, challenge{zone: zone, name: name, value: authorization.Record})
	}
	for _, entry := range published {
		if err := s.Propagation.Wait(ctx, entry.zone, entry.name, entry.value); err != nil {
			return Issued{}, err
		}
	}
	for _, authorization := range authorizations {
		if authorization.Valid {
			continue
		}
		if err := order.Accept(ctx, authorization); err != nil {
			return Issued{}, err
		}
		if err := order.Wait(ctx, authorization); err != nil {
			return Issued{}, err
		}
	}
	return order.Finalize(ctx)
}

type challenge struct {
	zone  Zone
	name  string
	value string
}

// challengeRecords collects the TXT records one run created, so the deferred
// cleanup removes exactly those and nothing an Owner wrote.
type challengeRecords struct {
	dns     DNS
	token   string
	onError func(string, error)
	created []struct{ zoneID, recordID string }
}

func (c *challengeRecords) add(zoneID, recordID string) {
	c.created = append(c.created, struct{ zoneID, recordID string }{zoneID: zoneID, recordID: recordID})
}

func (c *challengeRecords) delete(ctx context.Context) {
	cleanup, cancel := persist(ctx)
	defer cancel()
	for _, record := range c.created {
		if err := c.dns.DeleteRecord(cleanup, c.token, record.zoneID, record.recordID); err != nil && c.onError != nil {
			c.onError("challenge record could not be removed", err)
		}
	}
}

// failed stores the fixed code, counts the attempt and schedules the retry, then
// reminds the Owner when a certificate that still serves material is about to
// expire.
func (s *Service) failed(ctx context.Context, item Certificate, keepValid bool, err error, now time.Time) {
	code := failureCode(err)
	attempts := item.Attempts + 1
	saved, cancel := persist(ctx)
	defer cancel()
	if recordErr := s.Repository.RecordFailure(saved, item.ID, code, attempts, NextAttempt(now, attempts), keepValid, now); recordErr != nil {
		s.warn("certificate failure could not be recorded", recordErr)
		return
	}
	s.alertIfExpiring(saved, item, now)
}

// alertIfExpiring publishes one system.alert and one audit row per certificate
// per day once the failing renewal has less than a week left. The audit row is
// what makes "once per day" survive a panel restart.
func (s *Service) alertIfExpiring(ctx context.Context, item Certificate, now time.Time) {
	if !item.HasMaterial || s.Audit == nil {
		return
	}
	expires, ok := item.ExpiresAt()
	if !ok || expires.Sub(now) >= alertWindow {
		return
	}
	written, err := s.Audit.RecordDaily(ctx, "system", "证书续期失败", item.ID)
	if err != nil {
		s.warn("certificate alert could not be recorded", err)
		return
	}
	if written && s.Publish != nil {
		s.Publish("system.alert", map[string]string{"alert": "certificate_renewal", "certificateId": item.ID})
	}
}
