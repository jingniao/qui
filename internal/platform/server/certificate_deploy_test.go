package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/certificate"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/proxy"
	pb "proxycontrol/pkg/contracts/agentpb"
	"proxycontrol/pkg/contracts/desiredstate"
)

// issuedPair is a certificate and key the library can hold.
func issuedPair(t *testing.T, name string) (certPEM, keyPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(90 * 24 * time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))
}

// certificateDeployFixture is a published node whose acme inbound serves a stored
// certificate, with the deployer's clock and timezone under the test's control.
type certificateDeployFixture struct {
	*quotaFixture
	deployer      *certificateDeployer
	certificateID string
	inboundID     string
	clock         time.Time
	alerts        *alertRecorder
}

func newCertificateDeployFixture(t *testing.T) *certificateDeployFixture {
	t.Helper()
	f := newQuotaFixture(t)
	c := &certificateDeployFixture{quotaFixture: f, certificateID: "certificate-edge", clock: time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC), alerts: &alertRecorder{}}
	stamp := certificate.Stamp(c.clock)
	repository := f.app.certificates.Repository
	if err := repository.Create(f.ctx, certificate.Certificate{ID: c.certificateID, Domains: []string{"edge.example.com"}, Status: certificate.StatusPending, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	c.renew(t)
	response := f.owner.call("POST", "/api/v1/inbounds", proxy.InboundInput{Name: "ACME VLESS", NodeID: f.nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: 24444, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", TLSSource: proxy.TLSSourceACME, CertificateID: c.certificateID, Settings: desiredstate.ProtocolSettings{Security: "tls"}})
	if response.Code != 201 {
		t.Fatalf("acme inbound: %d %s", response.Code, response.Body.String())
	}
	var created proxy.Inbound
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	c.inboundID = created.ID
	c.deployer = f.app.certificateDeployer
	c.deployer.now = func() time.Time { return c.clock }
	c.deployer.location = func(context.Context) (*time.Location, error) { return time.UTC, nil }
	c.deployer.publish = func(kind string, data any) {
		if payload, ok := data.(map[string]string); ok && kind == "system.alert" {
			c.alerts.record(payload)
		}
	}
	f.publish(t)
	return c
}

// renew stores a new pair the way a finished renewal does and returns its
// certificate.
func (c *certificateDeployFixture) renew(t *testing.T) string {
	t.Helper()
	certPEM, keyPEM := issuedPair(t, "edge.example.com")
	now := c.clock
	if err := c.app.certificates.Repository.SaveIssued(c.ctx, c.certificateID, certificate.Material{CertificatePEM: certPEM, PrivateKeyPEM: keyPEM}, certificate.Stamp(now), certificate.Stamp(now.Add(90*24*time.Hour)), now); err != nil {
		t.Fatal(err)
	}
	return certPEM
}

// at moves the clock to an hour of the given day after the fixture's first.
func (c *certificateDeployFixture) at(day, hour int) {
	c.clock = time.Date(2026, 11, 10+day, hour, 0, 0, 0, time.UTC)
}

func (c *certificateDeployFixture) pass(t *testing.T) {
	t.Helper()
	c.deployer.pass(c.ctx)
}

func (c *certificateDeployFixture) certificateRevisions(t *testing.T) []deployment.Revision {
	t.Helper()
	result := []deployment.Revision{}
	for _, revision := range c.autoRevisions(t) {
		if strings.HasPrefix(revision.Summary, "auto: certificate ") {
			result = append(result, revision)
		}
	}
	return result
}

func (c *certificateDeployFixture) inboundOf(t *testing.T, state desiredstate.State) desiredstate.Inbound {
	t.Helper()
	for _, inbound := range state.Inbounds {
		if inbound.ID == c.inboundID {
			return inbound
		}
	}
	t.Fatal("the acme inbound is missing from the state")
	return desiredstate.Inbound{}
}

func (c *certificateDeployFixture) publishedState(t *testing.T) desiredstate.State {
	t.Helper()
	state, err := c.app.deployments.Published(c.ctx, c.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRenewedCertificateDeploysOnlyItsPair(t *testing.T) {
	c := newCertificateDeployFixture(t)
	before := c.publishedState(t)
	if c.app.certificates.Renewed == nil {
		t.Fatal("the certificate service does not report renewals to the deployer")
	}
	// Nothing differs yet, so a pass does nothing.
	c.pass(t)
	if len(c.certificateRevisions(t)) != 0 {
		t.Fatal("a pass without a renewal created a revision")
	}

	renewed := c.renew(t)
	c.pass(t)
	revisions := c.certificateRevisions(t)
	if len(revisions) != 1 || revisions[0].Summary != "auto: certificate edge.example.com renewed" || revisions[0].NodeID != c.nodeID {
		t.Fatalf("revisions: %+v", revisions)
	}
	state := c.state(t, revisions[0].ID)
	pair := c.inboundOf(t, state).Settings
	if pair.CertificatePEM != renewed || pair.PrivateKeyPEM == "" || pair.PrivateKeyPEM == c.inboundOf(t, before).Settings.PrivateKeyPEM {
		t.Fatal("the revision does not carry the renewed pair")
	}
	// Everything else is the published state byte for byte.
	restored := state
	restored.Inbounds = append([]desiredstate.Inbound(nil), state.Inbounds...)
	for index := range restored.Inbounds {
		if restored.Inbounds[index].ID == c.inboundID {
			restored.Inbounds[index].Settings.CertificatePEM = c.inboundOf(t, before).Settings.CertificatePEM
			restored.Inbounds[index].Settings.PrivateKeyPEM = c.inboundOf(t, before).Settings.PrivateKeyPEM
		}
	}
	if !quotaStatesEqual(restored, before) {
		t.Fatal("the revision changed more than the certificate pair")
	}
	status, err := c.app.deployments.TargetStatus(c.ctx, c.nodeID, revisions[0].ID)
	if err != nil || status != "pending" {
		t.Fatalf("the revision was not deployed: %q %v", status, err)
	}
	// While the Agent has not answered, further passes wait.
	c.at(0, 5)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 1 {
		t.Fatal("a pass created a second revision while the first was pending")
	}
	c.apply(t, revisions[0].ID)
	c.idle(t)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 1 || len(c.autoRevisions(t)) != 1 {
		t.Fatalf("a node that runs the new pair got more revisions: %d", len(c.autoRevisions(t)))
	}
}

func TestRenewedCertificateWaitsForTheDeployWindow(t *testing.T) {
	c := newCertificateDeployFixture(t)
	c.renew(t)
	c.at(0, 12)
	c.pass(t)
	c.at(0, 2)
	c.pass(t)
	c.at(0, 6)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 0 {
		t.Fatal("the deployer ran outside its window (3 to 6, end exclusive)")
	}
	c.at(1, 3)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 1 {
		t.Fatal("the deployer did not run when the window opened")
	}
}

func TestCertificateDeployWindowFollowsTheSettings(t *testing.T) {
	c := newCertificateDeployFixture(t)
	start, end := 22, 2
	if _, _, err := c.app.certificates.Repository.SaveSettings(c.ctx, certificate.SaveSettingsInput{Email: "owner@example.com", Directory: certificate.DirectoryStaging, DeployStartHour: &start, DeployEndHour: &end, Actor: "owner"}); err != nil {
		t.Fatal(err)
	}
	c.renew(t)
	c.at(0, 4)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 0 {
		t.Fatal("the default window was used instead of the saved one")
	}
	c.at(0, 23)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 1 {
		t.Fatal("a window that wraps midnight did not open at its start hour")
	}
}

func TestCertificateDeployRetriesABusyNode(t *testing.T) {
	c := newCertificateDeployFixture(t)
	// A manual deployment the Agent has not answered keeps the node busy.
	manual := c.owner.call("POST", "/api/v1/revisions/proxy", map[string]any{"nodeId": c.nodeID, "summary": "Manual"})
	if manual.Code != 201 {
		t.Fatal(manual.Body.String())
	}
	var held deployment.Revision
	if err := json.Unmarshal(manual.Body.Bytes(), &held); err != nil {
		t.Fatal(err)
	}
	if err := c.app.deployments.Deploy(c.ctx, held.ID); err != nil {
		t.Fatal(err)
	}
	c.renew(t)
	c.pass(t)
	revisions := c.certificateRevisions(t)
	if len(revisions) != 1 {
		t.Fatalf("revisions while busy: %+v", revisions)
	}
	if status, err := c.app.deployments.TargetStatus(c.ctx, c.nodeID, revisions[0].ID); err != nil || status != "draft" {
		t.Fatalf("the busy node's revision: %q %v", status, err)
	}
	if entry := c.deployer.entries[c.nodeID]; entry == nil || entry.attempts != 0 || entry.gaveUp {
		t.Fatalf("waiting for a busy node counted as an attempt: %+v", entry)
	}
	c.at(0, 5)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 1 {
		t.Fatal("a second pass on a busy node created another revision")
	}
	// The held deployment finishes; the same draft is deployed on the next pass.
	c.apply(t, held.ID)
	c.idle(t)
	c.at(0, 5)
	c.pass(t)
	revisions = c.certificateRevisions(t)
	if len(revisions) != 1 {
		t.Fatalf("revisions after the node freed up: %+v", revisions)
	}
	if status, err := c.app.deployments.TargetStatus(c.ctx, c.nodeID, revisions[0].ID); err != nil || status != "pending" {
		t.Fatalf("the draft was not deployed: %q %v", status, err)
	}
}

func TestCertificateDeployGivesUpAfterThreeFailedWindows(t *testing.T) {
	c := newCertificateDeployFixture(t)
	c.renew(t)
	fail := func(revision int64) {
		t.Helper()
		if err := c.app.deployments.Result(c.ctx, c.nodeID, &pb.ApplyResult{Revision: revision, RolledBack: true}); err != nil {
			t.Fatal(err)
		}
		c.idle(t)
	}
	for day := 0; day < 3; day++ {
		c.at(day, 4)
		c.pass(t)
		revisions := c.certificateRevisions(t)
		if len(revisions) != day+1 {
			t.Fatalf("day %d: %d revisions", day, len(revisions))
		}
		fail(revisions[day].ID)
		// The Agent's answer is read in the same window, which counts the failure
		// and keeps the node out of the rest of it.
		c.at(day, 5)
		c.pass(t)
		c.pass(t)
		if len(c.certificateRevisions(t)) != day+1 {
			t.Fatalf("day %d: a failed node was retried inside the same window", day)
		}
	}
	if alerts := c.alerts.payloads("certificate_deploy_failed"); len(alerts) != 1 || alerts[0]["nodeId"] != c.nodeID {
		t.Fatalf("alerts: %v", c.alerts.alerts)
	}
	if countAdminOperations(t, c.quotaFixture, "证书自动发布失败") != 1 {
		t.Fatal("the give-up was not recorded in the operation log")
	}
	c.at(3, 4)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 3 {
		t.Fatal("the deployer kept trying after giving up")
	}
	// A new renewal is a new pair and starts over.
	c.at(3, 4)
	c.renew(t)
	c.deployer.renewed(c.ctx, c.certificateID)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 4 {
		t.Fatal("a fresh renewal did not end the give-up")
	}
}

func TestManualPublicationAlreadyCarriesTheRenewedPair(t *testing.T) {
	c := newCertificateDeployFixture(t)
	c.renew(t)
	c.publish(t)
	c.pass(t)
	if len(c.certificateRevisions(t)) != 0 {
		t.Fatal("the deployer published a pair the node already runs")
	}
}

func TestCertificateDeployerReconcilesAtStartAndOnRenewal(t *testing.T) {
	c := newCertificateDeployFixture(t)
	// A renewal made while the panel was down: the restarted deployer finds the
	// difference in its first pass.
	c.renew(t)
	restarted := newCertificateDeployer(c.deployer.logger, c.app.certificates, c.app.deployments, c.deployer.publish)
	restarted.now, restarted.location, restarted.record = c.deployer.now, c.deployer.location, c.deployer.record
	ctx, stop := context.WithCancel(c.ctx)
	done := make(chan struct{})
	go func() { restarted.run(ctx); close(done) }()
	defer func() { stop(); <-done }()
	flowWait(t, "the startup pass to queue the stale node", func() bool { return len(c.certificateRevisions(t)) == 1 })
	c.apply(t, c.certificateRevisions(t)[0].ID)
	c.idle(t)
	// A later renewal wakes the loop without waiting for the hourly check.
	c.renew(t)
	restarted.renewed(ctx, c.certificateID)
	flowWait(t, "the renewal to be deployed", func() bool { return len(c.certificateRevisions(t)) == 2 })
}

// The deletion guard reads the reference the inbound save writes: the certificate
// is held while an acme inbound uses it and released when the inbound moves on.
func TestCertificateInUseFollowsTheInboundReference(t *testing.T) {
	c := newCertificateDeployFixture(t)
	held := c.owner.call("DELETE", "/api/v1/certificates/"+c.certificateID, nil)
	if held.Code != 409 || !strings.Contains(held.Body.String(), "certificate_in_use") || !strings.Contains(held.Body.String(), c.inboundID) {
		t.Fatalf("delete while referenced: %d %s", held.Code, held.Body.String())
	}
	pastedCert, pastedKey := issuedPair(t, "edge.example.com")
	move := proxy.InboundInput{Name: "ACME VLESS", NodeID: c.nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: 24444, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", TLSSource: proxy.TLSSourcePaste, Settings: desiredstate.ProtocolSettings{Security: "tls", CertificatePEM: pastedCert, PrivateKeyPEM: pastedKey}}
	if response := c.owner.call("PUT", "/api/v1/inbounds/"+c.inboundID, move); response.Code != 200 {
		t.Fatalf("move to a pasted certificate: %d %s", response.Code, response.Body.String())
	}
	if released := c.owner.call("DELETE", "/api/v1/certificates/"+c.certificateID, nil); released.Code != 204 {
		t.Fatalf("delete after release: %d %s", released.Code, released.Body.String())
	}
}

// An inbound the Owner repointed to another certificate but has not published is
// an unreviewed edit: the deployer must not push it, even when the new
// certificate is renewed. Only a certificate that keeps the published names is a
// renewal.
func TestRepointedInboundWaitsForAManualPublication(t *testing.T) {
	c := newCertificateDeployFixture(t)
	other := "certificate-other"
	stamp := certificate.Stamp(c.clock)
	repository := c.app.certificates.Repository
	if err := repository.Create(c.ctx, certificate.Certificate{ID: other, Domains: []string{"other.example.com"}, Status: certificate.StatusPending, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		certPEM, keyPEM := issuedPair(t, "other.example.com")
		if err := repository.SaveIssued(c.ctx, other, certificate.Material{CertificatePEM: certPEM, PrivateKeyPEM: keyPEM}, stamp, certificate.Stamp(c.clock.Add(90*24*time.Hour)), c.clock); err != nil {
			t.Fatal(err)
		}
		if round == 0 {
			move := proxy.InboundInput{Name: "ACME VLESS", NodeID: c.nodeID, Protocol: "vless", Listen: "0.0.0.0", Port: 24444, Enabled: true, SubscriptionVisible: true, DefaultOutbound: "direct", TLSSource: proxy.TLSSourceACME, CertificateID: other, Settings: desiredstate.ProtocolSettings{Security: "tls", ServerName: "other.example.com"}}
			if response := c.owner.call("PUT", "/api/v1/inbounds/"+c.inboundID, move); response.Code != 200 {
				t.Fatalf("repoint: %d %s", response.Code, response.Body.String())
			}
		}
		c.pass(t)
		if len(c.certificateRevisions(t)) != 0 {
			t.Fatalf("round %d: the deployer published an unreviewed repoint", round)
		}
	}
}
