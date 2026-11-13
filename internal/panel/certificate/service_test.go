package certificate

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/platform/vault"
	"proxycontrol/internal/testutil/dbtest"
)

// fakeAuditor stands in for the operation log. RecordDaily keeps at most one entry
// per action, target and UTC day the way system.Repository.RecordDaily does; that
// rule itself is covered by the system package's own test, because importing it
// here would close a cycle through the backup package.
type fakeAuditor struct {
	mu      sync.Mutex
	entries []string
	daily   map[string]bool
	now     func() time.Time
}

func (a *fakeAuditor) Record(_ context.Context, _, action, target, _, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, action+"|"+target)
	return nil
}

func (a *fakeAuditor) RecordDaily(_ context.Context, _, action, target string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.daily == nil {
		a.daily = map[string]bool{}
	}
	key := action + "|" + target + "|" + a.now().UTC().Format("2006-01-02")
	if a.daily[key] {
		return false, nil
	}
	a.daily[key] = true
	a.entries = append(a.entries, action+"|"+target)
	return true, nil
}

// actions is how many audit entries of one action were written.
func (a *fakeAuditor) actions(action string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	total := 0
	for _, entry := range a.entries {
		if entry == action+"|" || len(entry) > len(action) && entry[:len(action)+1] == action+"|" {
			total++
		}
	}
	return total
}

// fakeDNS replaces Cloudflare in the orchestrator tests. It keeps every record it
// was asked to write and remove, which is how "the TXT records are always removed"
// is checked.
type fakeDNS struct {
	mu        sync.Mutex
	zones     map[string]Zone
	records   []string
	created   []string
	deleted   []string
	addresses []string
	fail      error
	verifyErr error
}

func (f *fakeDNS) VerifyToken(context.Context, string) error { return f.verifyErr }

func (f *fakeDNS) Zone(_ context.Context, _, domain string) (Zone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	labels := strings.Split(domain, ".")
	for index := 0; index < len(labels)-1; index++ {
		if zone, ok := f.zones[strings.Join(labels[index:], ".")]; ok {
			return zone, nil
		}
	}
	return Zone{}, fail(CodeCloudflareZoneNotFound)
}

func (f *fakeDNS) CreateTXT(_ context.Context, _, zoneID, name, value string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	id := zoneID + "/" + name + "/" + value
	f.records = append(f.records, id)
	f.created = append(f.created, id)
	return id, nil
}

func (f *fakeDNS) DeleteRecord(_ context.Context, _, _, recordID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, recordID)
	return nil
}

func (f *fakeDNS) UpsertAddress(_ context.Context, _, zoneID, name, address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addresses = append(f.addresses, zoneID+"/"+name+"/"+address)
	return nil
}

// fakePropagation stands in for the authoritative name-server poll.
type fakePropagation struct {
	waited []string
	err    error
}

func (p *fakePropagation) Wait(_ context.Context, _ Zone, name, value string) error {
	p.waited = append(p.waited, name+"/"+value)
	return p.err
}

// fakeOrder is one open order the fake issuer handed out.
type fakeOrder struct {
	authorizations []Authorization
	accepted       int
	waited         int
	issued         Issued
	failAccept     error
	failFinalize   error
}

func (o *fakeOrder) Authorizations() []Authorization { return o.authorizations }

func (o *fakeOrder) Accept(context.Context, Authorization) error {
	if o.failAccept != nil {
		return o.failAccept
	}
	o.accepted++
	return nil
}

func (o *fakeOrder) Wait(context.Context, Authorization) error {
	o.waited++
	return nil
}

func (o *fakeOrder) Finalize(context.Context) (Issued, error) {
	if o.failFinalize != nil {
		return Issued{}, o.failFinalize
	}
	return o.issued, nil
}

// fakeIssuer replaces the CA.
type fakeIssuer struct {
	order  *fakeOrder
	fail   error
	begins int
}

func (i *fakeIssuer) Begin(_ context.Context, request BeginRequest) (Account, Order, error) {
	i.begins++
	if i.fail != nil {
		return Account{}, nil, i.fail
	}
	return Account{KeyPEM: "account-key", URL: "https://acme.test/account/1", Directory: request.Directory, Email: request.Email}, i.order, nil
}

// newIssuerOrder builds the default one-name successful order.
func newIssuerOrder() *fakeOrder {
	return &fakeOrder{
		authorizations: []Authorization{{URL: "https://acme.test/authz/1", Domain: "edge.example.com", Token: "token-1", Record: "record-1"}},
		issued: Issued{Material: Material{CertificatePEM: "-----BEGIN CERTIFICATE-----\nleaf\n-----END CERTIFICATE-----\n", PrivateKeyPEM: "-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----\n"},
			NotBefore: time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2027, 2, 7, 0, 0, 0, 0, time.UTC)},
	}
}

// issuanceFixture is a panel database with the certificate settings already
// saved, one node with a public address, and fakes for everything outside the
// process.
type issuanceFixture struct {
	service     *Service
	repository  *Repository
	dns         *fakeDNS
	issuer      *fakeIssuer
	propagation *fakePropagation
	database    *sql.DB
	nodeID      string
	events      []string
	now         time.Time
}

const testToken = "cloudflare-token-abcd"

func newIssuanceFixture(t *testing.T) *issuanceFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	database, err := dbtest.Open(ctx, filepath.Join(root, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	key, err := vault.Open(filepath.Join(root, "configuration.key"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := &node.Repository{Database: database}
	created, _, _, err := nodes.CreateEnrollment(ctx, "edge", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE nodes SET public_ip='203.0.113.7' WHERE id=?", created.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 9, 4, 0, 0, 0, time.UTC)
	fixture := &issuanceFixture{repository: &Repository{Database: database, Vault: key}, database: database, nodeID: created.ID, now: now,
		dns:    &fakeDNS{zones: map[string]Zone{"example.com": {ID: "zone-example", Name: "example.com", NameServers: []string{"ns1.example.com"}}}},
		issuer: &fakeIssuer{order: newIssuerOrder()}, propagation: &fakePropagation{}}
	fixture.service = NewService(fixture.repository)
	fixture.service.Issuer = fixture.issuer
	fixture.service.DNS = fixture.dns
	fixture.service.Propagation = fixture.propagation
	fixture.service.Audit = &fakeAuditor{now: func() time.Time { return fixture.now }}
	fixture.service.Now = func() time.Time { return fixture.now }
	fixture.service.Publish = func(kind string, _ any) { fixture.events = append(fixture.events, kind) }
	token := testToken
	if _, _, err := fixture.repository.SaveSettings(ctx, SaveSettingsInput{Email: "owner@example.com", Directory: DirectoryStaging, Token: &token, VerifiedAt: Stamp(now), Actor: "owner"}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// store inserts one certificate row the way the API does.
func (f *issuanceFixture) store(t *testing.T, domains []string, autoDNS bool) Certificate {
	t.Helper()
	stamp := Stamp(f.now)
	item := Certificate{ID: "certificate-" + domains[0], Domains: domains, Status: StatusPending, AutoDNS: autoDNS, CreatedAt: stamp, UpdatedAt: stamp}
	if autoDNS {
		item.DNSNodeID = f.nodeID
	}
	if err := f.repository.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	return item
}

// reload reads a row back.
func (f *issuanceFixture) reload(t *testing.T, id string) Certificate {
	t.Helper()
	item, err := f.repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestIssuanceStoresMaterialAndRemovesChallengeRecords(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	item := fixture.store(t, []string{"edge.example.com"}, true)
	fixture.service.run(ctx, item)
	saved := fixture.reload(t, item.ID)
	if saved.Status != StatusValid || !saved.HasMaterial || saved.LastErrorCode != "" || saved.Attempts != 0 {
		t.Fatalf("saved row: %+v", saved)
	}
	if saved.NotAfter != "2027-02-07T00:00:00Z" {
		t.Fatalf("notAfter = %q", saved.NotAfter)
	}
	if len(fixture.dns.created) != 1 || len(fixture.dns.deleted) != 1 || fixture.dns.created[0] != fixture.dns.deleted[0] {
		t.Fatalf("challenge records: created %v deleted %v", fixture.dns.created, fixture.dns.deleted)
	}
	if len(fixture.dns.addresses) != 1 || fixture.dns.addresses[0] != "zone-example/edge.example.com/203.0.113.7" {
		t.Fatalf("address records: %v", fixture.dns.addresses)
	}
	if len(fixture.propagation.waited) != 1 || fixture.propagation.waited[0] != "_acme-challenge.edge.example.com/record-1" {
		t.Fatalf("propagation waits: %v", fixture.propagation.waited)
	}
	material, err := fixture.repository.Material(ctx, item.ID)
	if err != nil || material.CertificatePEM == "" || material.PrivateKeyPEM == "" {
		t.Fatalf("stored material: %+v %v", material, err)
	}
	// The account the issuer registered is sealed and reused by the next order.
	account, err := fixture.repository.ReadAccount(ctx)
	if err != nil || account.URL != "https://acme.test/account/1" || account.Directory != DirectoryStaging || account.Email != "owner@example.com" {
		t.Fatalf("stored account: %+v %v", account, err)
	}
}

func TestIssuanceFailureRemovesChallengeRecordsAndUsesBackoff(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	fixture.issuer.order.failFinalize = fail(CodeACMEFinalizeFailed)
	item := fixture.store(t, []string{"edge.example.com"}, false)
	for index, delay := range []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour} {
		fixture.service.run(ctx, fixture.reload(t, item.ID))
		saved := fixture.reload(t, item.ID)
		if saved.Status != StatusFailed || saved.LastErrorCode != CodeACMEFinalizeFailed || saved.Attempts != index+1 {
			t.Fatalf("attempt %d: %+v", index+1, saved)
		}
		next, ok := parseStamp(saved.NextAttemptAt)
		if !ok || next.Sub(fixture.now) != delay {
			t.Fatalf("attempt %d next attempt %q, want %s", index+1, saved.NextAttemptAt, delay)
		}
		// Every attempt published and removed exactly one record.
		if len(fixture.dns.created) != index+1 || len(fixture.dns.deleted) != index+1 {
			t.Fatalf("attempt %d records: created %v deleted %v", index+1, fixture.dns.created, fixture.dns.deleted)
		}
	}
}

func TestFailedRenewalKeepsValidCertificateUsable(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	item := fixture.store(t, []string{"edge.example.com"}, false)
	fixture.service.run(ctx, fixture.reload(t, item.ID))
	if saved := fixture.reload(t, item.ID); saved.Status != StatusValid {
		t.Fatalf("first issuance: %+v", saved)
	}
	fixture.issuer.order.failFinalize = fail(CodeACMERateLimited)
	fixture.now = fixture.now.Add(48 * time.Hour)
	fixture.service.run(ctx, fixture.reload(t, item.ID))
	saved := fixture.reload(t, item.ID)
	if saved.Status != StatusValid || saved.LastErrorCode != CodeACMERateLimited || saved.Attempts != 1 {
		t.Fatalf("failed renewal: %+v", saved)
	}
	if _, err := fixture.repository.Material(ctx, item.ID); err != nil {
		t.Fatalf("material lost: %v", err)
	}
}

func TestAutoDNSWithoutNodeAddressFails(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	if _, err := fixture.database.Exec("UPDATE nodes SET public_ip='' WHERE id=?", fixture.nodeID); err != nil {
		t.Fatal(err)
	}
	item := fixture.store(t, []string{"edge.example.com"}, true)
	fixture.service.run(ctx, item)
	saved := fixture.reload(t, item.ID)
	if saved.Status != StatusFailed || saved.LastErrorCode != CodeDNSNodeAddressMissing {
		t.Fatalf("missing address: %+v", saved)
	}
	if len(fixture.dns.created) != 0 {
		t.Fatalf("a record was written without an address: %v", fixture.dns.created)
	}
	if fixture.issuer.order.accepted != 0 {
		t.Fatal("a challenge was accepted without an address")
	}
}

func TestPropagationTimeoutFailsAndStillCleansUp(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	fixture.propagation.err = fail(CodeDNSPropagationTimeout)
	item := fixture.store(t, []string{"edge.example.com"}, false)
	fixture.service.run(ctx, item)
	saved := fixture.reload(t, item.ID)
	if saved.Status != StatusFailed || saved.LastErrorCode != CodeDNSPropagationTimeout {
		t.Fatalf("propagation failure: %+v", saved)
	}
	if fixture.issuer.order.accepted != 0 {
		t.Fatal("a challenge was accepted without a visible record")
	}
	if len(fixture.dns.created) != 1 || len(fixture.dns.deleted) != 1 {
		t.Fatalf("cleanup after a propagation failure: created %v deleted %v", fixture.dns.created, fixture.dns.deleted)
	}
}

func TestStartupTurnsIssuingRowsIntoInterruptedFailures(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	item := fixture.store(t, []string{"edge.example.com"}, false)
	if err := fixture.repository.MarkIssuing(context.Background(), item.ID, fixture.now); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fixture.service.Run(ctx)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		saved := fixture.reload(t, item.ID)
		if saved.Status != StatusIssuing {
			if saved.Status != StatusFailed || saved.LastErrorCode != CodeInterrupted || saved.Attempts != 1 {
				t.Fatalf("interrupted row: %+v", saved)
			}
			next, ok := parseStamp(saved.NextAttemptAt)
			if !ok || next.Sub(fixture.now) != time.Hour {
				t.Fatalf("interrupted row retry: %q", saved.NextAttemptAt)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the interrupted row was never reconciled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestExpiryAlertIsPublishedOncePerDay(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	item := fixture.store(t, []string{"edge.example.com"}, false)
	fixture.service.run(ctx, item)
	if _, err := fixture.database.Exec("UPDATE certificates SET not_after=? WHERE id=?", Stamp(fixture.now.Add(5*24*time.Hour)), item.ID); err != nil {
		t.Fatal(err)
	}
	fixture.issuer.order.failFinalize = fail(CodeACMEOrderFailed)
	for attempt := 0; attempt < 2; attempt++ {
		fixture.service.run(ctx, fixture.reload(t, item.ID))
	}
	if alerts := countEvents(fixture.events, "system.alert"); alerts != 1 {
		t.Fatalf("alerts on one day: %d", alerts)
	}
	// The next day the reminder is due again.
	fixture.now = fixture.now.Add(24 * time.Hour)
	fixture.service.run(ctx, fixture.reload(t, item.ID))
	if alerts := countEvents(fixture.events, "system.alert"); alerts != 2 {
		t.Fatalf("alerts on the next day: %d", alerts)
	}
	if writes := fixture.service.Audit.(*fakeAuditor).actions("证书续期失败"); writes != 2 {
		t.Fatalf("audit rows: %d", writes)
	}
}

func countEvents(events []string, kind string) int {
	total := 0
	for _, event := range events {
		if event == kind {
			total++
		}
	}
	return total
}

func TestSweepQueuesOnlyDueCertificates(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()
	pending := fixture.store(t, []string{"pending.example.com"}, false)
	soon := fixture.store(t, []string{"soon.example.com"}, false)
	far := fixture.store(t, []string{"far.example.com"}, false)
	backoff := fixture.store(t, []string{"backoff.example.com"}, false)
	for _, update := range []struct {
		id, status, notAfter, next string
	}{
		{soon.ID, StatusValid, Stamp(fixture.now.Add(20 * 24 * time.Hour)), ""},
		{far.ID, StatusValid, Stamp(fixture.now.Add(60 * 24 * time.Hour)), ""},
		{backoff.ID, StatusFailed, "", Stamp(fixture.now.Add(24 * time.Hour))},
	} {
		if _, err := fixture.database.Exec("UPDATE certificates SET status=?,not_after=?,next_attempt_at=? WHERE id=?", update.status, update.notAfter, update.next, update.id); err != nil {
			t.Fatal(err)
		}
	}
	fixture.service.sweep(ctx)
	queued := map[string]bool{}
	for len(fixture.service.jobs) > 0 {
		queued[<-fixture.service.jobs] = true
	}
	if len(queued) != 2 || !queued[pending.ID] || !queued[soon.ID] {
		t.Fatalf("queued: %v", queued)
	}
}

func TestCoveringAndCloudflareConfigured(t *testing.T) {
	fixture := newIssuanceFixture(t)
	ctx := context.Background()

	configured, err := fixture.service.CloudflareConfigured(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !configured {
		t.Fatal("expected CloudflareConfigured to be true")
	}

	c1 := fixture.store(t, []string{"*.example.com"}, false)
	fixture.store(t, []string{"other.com"}, false)
	notAfter := Stamp(fixture.now.Add(30 * 24 * time.Hour))
	if _, err := fixture.database.Exec("UPDATE certificates SET status=?,not_after=? WHERE id=?", StatusValid, notAfter, c1.ID); err != nil {
		t.Fatal(err)
	}

	matches, err := fixture.service.Covering(ctx, "a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].ID != c1.ID {
		t.Fatalf("expected 1 match for c1, got %v", matches)
	}

	noMatches, err := fixture.service.Covering(ctx, "nomatch.org")
	if err != nil {
		t.Fatal(err)
	}
	if len(noMatches) != 0 {
		t.Fatalf("expected 0 matches, got %v", noMatches)
	}
}
