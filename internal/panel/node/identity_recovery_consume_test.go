package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"proxycontrol/internal/platform/pki"
	pb "proxycontrol/pkg/contracts/agentpb"
	"strings"
	"testing"
	"time"
)

// recoveryPanel is one panel with a real node CA: recovery consumption issues
// and verifies certificates, so the test needs the authority the panel runs.
type recoveryPanel struct {
	*Repository
	authority *pki.Authority
	ctx       context.Context
}

func newRecoveryPanel(t *testing.T) *recoveryPanel {
	t.Helper()
	repository, ctx := deletionRepository(t)
	authority, err := pki.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &recoveryPanel{Repository: repository, authority: authority, ctx: ctx}
}

// signedRequest is a fresh ECDSA P-256 key, as the panel's ParseCSR requires,
// and the PEM an Agent sends with its enrollment.
func signedRequest(t *testing.T) ([]byte, *x509.CertificateRequest) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	csr, err := pki.ParseCSR(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, csr
}

// request is what the reinstalled Agent sends: a fresh key and the metadata the
// panel requires.
func (p *recoveryPanel) request(t *testing.T, token, hostname string) (*pb.EnrollRequest, *x509.CertificateRequest) {
	t.Helper()
	encoded, csr := signedRequest(t)
	return &pb.EnrollRequest{EnrollmentToken: token, Hostname: hostname, OperatingSystem: "Linux", AgentVersion: "1.4.0", CsrPem: encoded}, csr
}

// attempt is one enrollment the panel can consume: the request and the
// certificate issuance the original enrollment path performs.
type attempt struct {
	panel   *recoveryPanel
	request *pb.EnrollRequest
	csr     *x509.CertificateRequest
	encoded []byte
	leaf    *x509.Certificate
}

func (p *recoveryPanel) attempt(t *testing.T, token, nodeID, hostname string) attempt {
	t.Helper()
	request, csr := p.request(t, token, hostname)
	encoded, leaf, err := p.authority.IssueNode(csr, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	return attempt{panel: p, request: request, csr: csr, encoded: encoded, leaf: leaf}
}

func (a attempt) fingerprint() string { return pki.Fingerprint(a.leaf) }

// consume runs the original enrollment transaction, which is the branch the
// panel chooses from the stored token purpose.
func (a attempt) consume(t *testing.T, nodeID, purpose string) error {
	t.Helper()
	return a.panel.ConsumeEnrollmentOriginal(a.panel.ctx, Hash(a.request.EnrollmentToken), nodeID, purpose, a.request, a.csr, a.encoded, a.panel.authority.PEM, a.leaf)
}

// seededRowCounts are the business rows a recovery must leave alone.
func seededRecoveryRows(t *testing.T, r *Repository, ctx context.Context, nodeID string) {
	t.Helper()
	for _, query := range []string{
		"INSERT INTO inbounds(id,name,node_id,protocol,listen,port,enabled,subscription_visible,internal_relay,default_outbound,allowed_users_json,encrypted_settings,created_at) VALUES('in-recovery','Entry',?,'socks5','0.0.0.0',1080,1,1,0,'direct','[]',x'00','now')",
		"INSERT INTO outbounds(id,name,node_id,protocol,enabled,encrypted_settings,support_tcp,support_udp,failure_policy,created_at) VALUES('out-recovery','Direct',?,'direct',1,x'00',1,1,'Block','now')",
		"INSERT INTO node_groups(id,name,purpose,enabled,subscription_visible,created_at) VALUES('g-recovery','Recovery','subscription',1,1,'now')",
		"INSERT INTO node_group_members(group_id,node_id) VALUES('g-recovery',?)",
		"INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(1,'owner','published','success','now',x'00')",
	} {
		if _, err := r.Database.ExecContext(ctx, query, nodeID); err != nil {
			t.Fatal(err)
		}
	}
}

func recoveryRowCounts(t *testing.T, r *Repository, ctx context.Context, nodeID string) []int {
	t.Helper()
	counts := []int{}
	for _, item := range []struct {
		query string
		arg   bool
	}{
		{"SELECT count(*) FROM nodes", false},
		{"SELECT count(*) FROM inbounds WHERE node_id=?", true},
		{"SELECT count(*) FROM outbounds WHERE node_id=?", true},
		{"SELECT count(*) FROM node_group_members WHERE node_id=?", true},
		{"SELECT count(*) FROM config_revisions", false},
	} {
		var count int
		var err error
		if item.arg {
			err = r.Database.QueryRowContext(ctx, item.query, nodeID).Scan(&count)
		} else {
			err = r.Database.QueryRowContext(ctx, item.query).Scan(&count)
		}
		if err != nil {
			t.Fatal(err)
		}
		counts = append(counts, count)
	}
	return counts
}

func recoveryIdentity(t *testing.T, r *Repository, ctx context.Context, nodeID string) string {
	t.Helper()
	var fingerprint string
	if err := r.Database.QueryRowContext(ctx, "SELECT fingerprint FROM node_identities WHERE node_id=?", nodeID).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func recoveryCount(t *testing.T, r *Repository, ctx context.Context, query string, args ...any) int {
	t.Helper()
	var count int
	if err := r.Database.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// A consumed recovery code replaces the certificate in place: the node ID and
// every business row stay, the replaced fingerprint is retired forever and only
// the new certificate authenticates.
func TestRecoveryConsumptionKeepsNodeAndBusinessRows(t *testing.T) {
	p := newRecoveryPanel(t)
	n := recoveryTestNode(t, p.Repository, p.ctx)
	previous := strings.Repeat("a", 64)
	seededRecoveryRows(t, p.Repository, p.ctx, n.ID)
	before := recoveryRowCounts(t, p.Repository, p.ctx, n.ID)
	_, token, _, err := p.IssueRecovery(p.ctx, n.ID, "owner", 15*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	replacement := p.attempt(t, token, n.ID, "reinstalled")
	if err := replacement.consume(t, n.ID, "recover"); err != nil {
		t.Fatalf("recovery consumption failed: %v", err)
	}
	if after := recoveryRowCounts(t, p.Repository, p.ctx, n.ID); fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("recovery changed business rows: %v then %v", before, after)
	}
	if current := recoveryIdentity(t, p.Repository, p.ctx, n.ID); current != replacement.fingerprint() {
		t.Fatalf("identity after recovery is %q, want %q", current, replacement.fingerprint())
	}
	if retired := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM node_identity_retirements WHERE node_id=? AND fingerprint=?", n.ID, previous); retired != 1 {
		t.Fatalf("previous fingerprint was not retired: %d", retired)
	}
	if err := p.ValidateIdentity(p.ctx, n.ID, previous); !errors.Is(err, ErrIdentity) {
		t.Fatalf("retired certificate still authenticates: %v", err)
	}
	if err := p.ValidateIdentity(p.ctx, n.ID, replacement.fingerprint()); err != nil {
		t.Fatalf("new certificate does not authenticate: %v", err)
	}
	var status, fingerprint, state string
	if err := p.Database.QueryRowContext(p.ctx, "SELECT status,new_fingerprint,redeploy_state FROM node_identity_recoveries WHERE token_hash=?", Hash(token)).Scan(&status, &fingerprint, &state); err != nil {
		t.Fatal(err)
	}
	if status != "consumed" || fingerprint != replacement.fingerprint() || state != "not_needed" {
		t.Fatalf("recovery record: %q %q %q", status, fingerprint, state)
	}
}

// Every refusal keeps the previous identity: the code either never applied or
// applied and rolled back completely.
func TestRecoveryConsumptionRefusalsKeepPreviousIdentity(t *testing.T) {
	for _, item := range []struct {
		name    string
		prepare func(*testing.T, *recoveryPanel, string, string)
		want    func(*testing.T, error)
	}{
		{
			name: "expired code",
			prepare: func(t *testing.T, p *recoveryPanel, _, token string) {
				if _, err := p.Database.ExecContext(p.ctx, "UPDATE node_enrollment_tokens SET expires_at=? WHERE token_hash=?", timestamp(time.Now().Add(-time.Minute)), Hash(token)); err != nil {
					t.Fatal(err)
				}
			},
			want: wantsTokenRefusal,
		},
		{
			name: "certificate replaced after the code was issued",
			prepare: func(t *testing.T, p *recoveryPanel, nodeID, _ string) {
				if _, err := p.Database.ExecContext(p.ctx, "UPDATE node_identities SET fingerprint=? WHERE node_id=?", strings.Repeat("b", 64), nodeID); err != nil {
					t.Fatal(err)
				}
			},
			want: wantsTokenRefusal,
		},
		{
			name: "deployment started after the code was issued",
			prepare: func(t *testing.T, p *recoveryPanel, nodeID, _ string) {
				for _, query := range []string{
					"INSERT INTO config_revisions(id,author,summary,status,created_at,encrypted_state) VALUES(2,'owner','pending','draft','now',x'00')",
					"INSERT INTO deployment_targets(revision,node_id,status,updated_at) VALUES(2,'" + nodeID + "','pending','now')",
				} {
					if _, err := p.Database.ExecContext(p.ctx, query); err != nil {
						t.Fatal(err)
					}
				}
			},
			want: func(t *testing.T, err error) {
				var blocked *BlockedError
				if !errors.As(err, &blocked) || blocked.Code != "node_recovery_blocked" {
					t.Fatalf("blocked recovery: %v", err)
				}
			},
		},
	} {
		t.Run(item.name, func(t *testing.T) {
			p := newRecoveryPanel(t)
			n := recoveryTestNode(t, p.Repository, p.ctx)
			_, token, _, err := p.IssueRecovery(p.ctx, n.ID, "owner", 15*time.Minute, false)
			if err != nil {
				t.Fatal(err)
			}
			item.prepare(t, p, n.ID, token)
			current := recoveryIdentity(t, p.Repository, p.ctx, n.ID)
			replacement := p.attempt(t, token, n.ID, "reinstalled")
			item.want(t, replacement.consume(t, n.ID, "recover"))
			if identity := recoveryIdentity(t, p.Repository, p.ctx, n.ID); identity != current || identity == replacement.fingerprint() {
				t.Fatalf("refused recovery changed the identity: %q", identity)
			}
			if retired := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM node_identity_retirements"); retired != 0 {
				t.Fatalf("refused recovery retired a fingerprint: %d", retired)
			}
			if used := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM node_enrollment_tokens WHERE token_hash=? AND consumed_at IS NOT NULL", Hash(token)); used != 0 {
				t.Fatal("refused recovery consumed the code")
			}
			if status := recoveryStatusValue(t, p, token); status != "issued" {
				t.Fatalf("refused recovery left the code %q", status)
			}
			if err := p.ValidateIdentity(p.ctx, n.ID, current); err != nil {
				t.Fatalf("previous certificate no longer authenticates: %v", err)
			}
			if enabled := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM nodes WHERE id=? AND enabled=1", n.ID); enabled != 1 {
				t.Fatal("refused recovery disabled the node")
			}
		})
	}
}

func wantsTokenRefusal(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrToken) {
		t.Fatalf("refusal: %v", err)
	}
}

func recoveryStatusValue(t *testing.T, p *recoveryPanel, token string) string {
	t.Helper()
	var status string
	if err := p.Database.QueryRowContext(p.ctx, "SELECT status FROM node_identity_recoveries WHERE token_hash=?", Hash(token)).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// A code that is already consumed cannot enroll a second machine, and the
// replacement the first consumption installed stays in place.
func TestRecoveryConsumptionRefusesUsedCode(t *testing.T) {
	p := newRecoveryPanel(t)
	n := recoveryTestNode(t, p.Repository, p.ctx)
	_, token, _, err := p.IssueRecovery(p.ctx, n.ID, "owner", 15*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	first := p.attempt(t, token, n.ID, "reinstalled")
	if err := first.consume(t, n.ID, "recover"); err != nil {
		t.Fatal(err)
	}
	second := p.attempt(t, token, n.ID, "second attempt")
	if err := second.consume(t, n.ID, "recover"); !errors.Is(err, ErrToken) {
		t.Fatalf("used code consumed twice: %v", err)
	}
	if identity := recoveryIdentity(t, p.Repository, p.ctx, n.ID); identity != first.fingerprint() {
		t.Fatalf("second attempt replaced the identity with %q", identity)
	}
	if retired := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM node_identity_retirements"); retired != 1 {
		t.Fatalf("retirements: %d", retired)
	}
	if err := p.ValidateIdentity(p.ctx, n.ID, second.fingerprint()); err == nil {
		t.Fatal("the refused certificate authenticates")
	}
}

// The two token purposes stay apart: a recovery code never enrolls through the
// plain transaction, and an enrollment code never replaces an identity.
func TestRecoveryAndEnrollmentCodesKeepTheirOwnPath(t *testing.T) {
	p := newRecoveryPanel(t)
	n := recoveryTestNode(t, p.Repository, p.ctx)
	previous := strings.Repeat("a", 64)
	_, token, _, err := p.IssueRecovery(p.ctx, n.ID, "owner", 15*time.Minute, false)
	if err != nil {
		t.Fatal(err)
	}
	recovery := p.attempt(t, token, n.ID, "reinstalled")
	if err := p.ConsumeEnrollment(p.ctx, Hash(token), n.ID, "reinstalled", "Linux", "1.4.0", string(recovery.encoded), recovery.fingerprint(), recovery.leaf.NotAfter); !errors.Is(err, ErrToken) {
		t.Fatalf("the enrollment path accepted a recovery code: %v", err)
	}
	if identity := recoveryIdentity(t, p.Repository, p.ctx, n.ID); identity != previous {
		t.Fatalf("refused code changed the identity to %q", identity)
	}
	if retired := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM node_identity_retirements"); retired != 0 {
		t.Fatalf("refused code retired a fingerprint: %d", retired)
	}
	fresh, enrollToken, _, err := p.CreateEnrollment(p.ctx, "Fresh", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	enrollment := p.attempt(t, enrollToken, fresh.ID, "first install")
	if err := enrollment.consume(t, fresh.ID, "enroll"); err != nil {
		t.Fatalf("enrollment rejected: %v", err)
	}
	if identity := recoveryIdentity(t, p.Repository, p.ctx, fresh.ID); identity != enrollment.fingerprint() {
		t.Fatalf("enrollment identity %q, want %q", identity, enrollment.fingerprint())
	}
	if retired := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM node_identity_retirements"); retired != 0 {
		t.Fatalf("enrollment retired a fingerprint: %d", retired)
	}
	if consumed := recoveryCount(t, p.Repository, p.ctx, "SELECT count(*) FROM node_identity_recoveries WHERE status='consumed'"); consumed != 0 {
		t.Fatalf("enrollment consumed a recovery record: %d", consumed)
	}
}

// An unknown code is a refusal rather than an internal failure.
func TestConsumeEnrollmentRejectsUnknownCode(t *testing.T) {
	p := newRecoveryPanel(t)
	n := recoveryTestNode(t, p.Repository, p.ctx)
	if err := p.ConsumeEnrollment(p.ctx, Hash("missing-code"), n.ID, "host", "Linux", "1.4.0", "pem", strings.Repeat("e", 64), time.Now().Add(time.Hour)); !errors.Is(err, ErrToken) {
		t.Fatalf("unknown code: %v", err)
	}
}
