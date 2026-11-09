package server

import (
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"proxycontrol/internal/panel/certificate"
	"proxycontrol/internal/panel/deployment"
	"proxycontrol/pkg/contracts/desiredstate"
)

const (
	// certificateDeployInterval is how often the deployer looks for nodes that
	// still run an older certificate; outside the Owner's window it only waits.
	certificateDeployInterval = time.Hour
	// certificateAttemptLimit is how many failed deploys one node may have before
	// the deployer stops and tells the Owner. Waiting for a busy node or for the
	// Agent's answer is not an attempt.
	certificateAttemptLimit = 3
)

// certificateDeployer publishes a renewed certificate to the nodes that serve it
// (v1.4.7 section 4). A publication restarts every core of the node, so it only
// runs inside the Owner's deploy window.
//
// It keeps no queue of work. Each pass compares the certificate library with
// every node's published state and acts on the difference, so a renewal that
// happened while the panel was down, a manual publication that already carries
// the new pair, and a retry after a failure are all the same question. The only
// memory is the per-node attempt bookkeeping.
type certificateDeployer struct {
	logger       *slog.Logger
	certificates *certificate.Service
	deployments  *deployment.Repository
	publish      func(string, any)
	// record writes the admin operation of a failure the Owner has to know about.
	record func(ctx context.Context, actor, action, target, result, ip string) error
	// location is the panel timezone the window hours are read in.
	location func(context.Context) (*time.Location, error)
	now      func() time.Time
	interval time.Duration
	wake     chan struct{}

	mu      sync.Mutex
	entries map[string]*certificateEntry
}

// certificateEntry is one node's bookkeeping. revision is a revision this
// deployer created whose outcome is not known yet, so a retry never creates a
// duplicate; retryAt keeps a failed node out of the rest of the current window.
type certificateEntry struct {
	revision int64
	attempts int
	retryAt  time.Time
	gaveUp   bool
}

// staleInbound is a published inbound whose pair differs from its certificate;
// names is the stored certificate's DNS name set, parsed once.
type staleInbound struct {
	inboundID     string
	certificateID string
	certPEM       string
	names         string
	keyPEM        string
}

func newCertificateDeployer(logger *slog.Logger, certificates *certificate.Service, deployments *deployment.Repository, publish func(string, any)) *certificateDeployer {
	return &certificateDeployer{logger: logger, certificates: certificates, deployments: deployments, publish: publish,
		now: time.Now, interval: certificateDeployInterval, wake: make(chan struct{}, 1), entries: map[string]*certificateEntry{}}
}

// renewed is the certificate service's callback. It only wakes the loop: the pass
// decides what to publish and when. A fresh pair also ends a give-up, because the
// failure was about the previous one.
func (d *certificateDeployer) renewed(_ context.Context, _ string) {
	d.mu.Lock()
	for nodeID, entry := range d.entries {
		if entry.gaveUp {
			delete(d.entries, nodeID)
		}
	}
	d.mu.Unlock()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// run reconciles once at start, which is the startup pass that recovers a renewal
// made before a restart, and then on every tick and wake until ctx is cancelled.
func (d *certificateDeployer) run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		d.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.wake:
		}
	}
}

// inDeployWindow reports whether the hour of now, in the panel timezone, is
// inside [start,end). A window with end before start wraps midnight.
func inDeployWindow(now time.Time, location *time.Location, start, end int) bool {
	hour := now.In(location).Hour()
	if start < end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

// windowEnd is the next moment the window closes after now.
func windowEnd(now time.Time, location *time.Location, end int) time.Time {
	local := now.In(location)
	closing := time.Date(local.Year(), local.Month(), local.Day(), end, 0, 0, 0, location)
	if !closing.After(local) {
		closing = closing.AddDate(0, 0, 1)
	}
	return closing
}

// pass runs one reconciliation if the window is open.
func (d *certificateDeployer) pass(ctx context.Context) {
	settings, err := d.certificates.Repository.ReadSettings(ctx)
	if err != nil {
		d.logError(ctx, "automatic certificate deploy could not read its window", err)
		return
	}
	location, err := d.location(ctx)
	if err != nil {
		d.logError(ctx, "automatic certificate deploy could not read the panel timezone", err)
		return
	}
	now := d.now()
	if !inDeployWindow(now, location, settings.DeployStartHour, settings.DeployEndHour) {
		return
	}
	stale, complete := d.stale(ctx)
	// A node that no longer differs is finished; forgetting it resets its attempts.
	// Nothing is forgotten while a read failed, because a node missing from the
	// result may only be unreadable.
	if complete {
		d.mu.Lock()
		for nodeID := range d.entries {
			if _, ok := stale[nodeID]; !ok {
				delete(d.entries, nodeID)
			}
		}
		d.mu.Unlock()
	}
	nodeIDs := make([]string, 0, len(stale))
	for nodeID := range stale {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Strings(nodeIDs)
	for _, nodeID := range nodeIDs {
		if ctx.Err() != nil {
			return
		}
		node := stale[nodeID]
		work, cancel := context.WithTimeout(ctx, time.Minute)
		d.deploy(work, nodeID, node, settings.DeployEndHour, location)
		cancel()
	}
}

// staleNode is what one node needs: its published state and intent, and the
// inbounds whose pair has to change.
type staleNode struct {
	published desiredstate.State
	intent    []string
	inbounds  []staleInbound
}

// stale lists the nodes whose published state holds an acme inbound with a pair
// other than the one its certificate has now. complete is false when a read
// failed and the list may be short.
func (d *certificateDeployer) stale(ctx context.Context) (map[string]staleNode, bool) {
	usages, err := d.certificates.Repository.Usages(ctx)
	if err != nil {
		d.logError(ctx, "automatic certificate deploy could not read the certificate references", err)
		return nil, false
	}
	byNode := map[string][]staleInbound{}
	complete := true
	for certificateID, used := range usages {
		certPEM, keyPEM, exists, err := d.certificates.CertificateMaterial(ctx, certificateID)
		if err != nil {
			d.logError(ctx, "automatic certificate deploy could not read a certificate", err)
			complete = false
			continue
		}
		if !exists || certPEM == "" {
			continue
		}
		names, _ := sanNames(certPEM)
		for _, usage := range used {
			byNode[usage.NodeID] = append(byNode[usage.NodeID], staleInbound{inboundID: usage.InboundID, certificateID: certificateID, certPEM: certPEM, names: names, keyPEM: keyPEM})
		}
	}
	result := map[string]staleNode{}
	for nodeID, candidates := range byNode {
		published, intent, err := d.deployments.PublishedWithIntent(ctx, nodeID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			d.logError(ctx, "automatic certificate deploy could not read published state", err)
			complete = false
			continue
		}
		published.Inbounds = append([]desiredstate.Inbound(nil), published.Inbounds...)
		differing := []staleInbound{}
		for _, candidate := range candidates {
			for _, inbound := range published.Inbounds {
				if inbound.ID == candidate.inboundID && (inbound.Settings.CertificatePEM != candidate.certPEM || inbound.Settings.PrivateKeyPEM != candidate.keyPEM) && sameNames(inbound.Settings.CertificatePEM, candidate.names) {
					differing = append(differing, candidate)
					break
				}
			}
		}
		if len(differing) > 0 {
			result[nodeID] = staleNode{published: published, intent: intent, inbounds: differing}
		}
	}
	return result, complete
}

// deploy advances one node by one step: it reads the outcome of its earlier
// revision, then builds and deploys a new one if the node still differs.
func (d *certificateDeployer) deploy(ctx context.Context, nodeID string, node staleNode, closeHour int, location *time.Location) {
	d.mu.Lock()
	entry := d.entries[nodeID]
	if entry == nil {
		entry = &certificateEntry{}
		d.entries[nodeID] = entry
	}
	skip := entry.gaveUp || d.now().Before(entry.retryAt)
	revision := entry.revision
	d.mu.Unlock()
	if skip {
		return
	}
	failed := func(reason string) {
		d.fail(ctx, nodeID, entry, reason, windowEnd(d.now(), location, closeHour))
	}
	if revision != 0 {
		status, err := d.deployments.TargetStatus(ctx, nodeID, revision)
		if err != nil {
			failed("status_unreadable")
			return
		}
		switch status {
		case "pending":
			// The Agent has not answered; an offline node receives the revision when
			// it reconnects, so there is nothing to do but wait.
			return
		case "failed", "rollback":
			d.setRevision(entry, 0)
			failed("agent_" + status)
			return
		case "success":
			// The node runs it, yet it still differs: another renewal arrived.
			d.setRevision(entry, 0)
			revision = 0
			d.resetAttempts(entry)
		case "draft":
			// Created but never queued; reused below while it still matches.
		default:
			d.setRevision(entry, 0)
			failed("unknown_status")
			return
		}
	}
	next := withRenewedPairs(node.published, node.inbounds)
	if revision != 0 {
		draft, err := d.deployments.State(ctx, revision)
		if err != nil {
			failed("revision_unreadable")
			return
		}
		if !quotaStatesEqual(draft, next) {
			// Overtaken by a publication or a newer renewal; build a fresh one.
			revision = 0
		}
	}
	if revision == 0 {
		created, err := d.deployments.CreateWithInboundIntent(ctx, next, node.intent, "auto", "auto: certificate "+d.describe(ctx, node.inbounds)+" renewed")
		if err != nil {
			failed("revision_failed")
			return
		}
		revision = created.ID
		d.setRevision(entry, revision)
	}
	if err := d.deployments.Deploy(ctx, revision); err != nil {
		if errors.Is(err, deployment.ErrNodeBusy) {
			// Another deployment owns the node; the revision stays a draft and the
			// next pass retries it.
			return
		}
		failed("deploy_failed")
	}
	// A queued deploy is not an applied one: the next pass reads the Agent's
	// result, and a node that rolled back gets a new revision in the next window.
}

func (d *certificateDeployer) setRevision(entry *certificateEntry, revision int64) {
	d.mu.Lock()
	entry.revision = revision
	d.mu.Unlock()
}

func (d *certificateDeployer) resetAttempts(entry *certificateEntry) {
	d.mu.Lock()
	entry.attempts = 0
	d.mu.Unlock()
}

// fail counts one failed attempt and keeps the node out of the rest of this
// window. The third failure stops the node and reaches the Owner as a log line,
// one system.alert and one audit entry; a failure that is only the shutdown
// cutting the work short is not an attempt.
func (d *certificateDeployer) fail(ctx context.Context, nodeID string, entry *certificateEntry, reason string, retryAt time.Time) {
	if ctx.Err() != nil {
		return
	}
	d.mu.Lock()
	entry.attempts++
	entry.retryAt = retryAt
	gaveUp := entry.attempts >= certificateAttemptLimit
	entry.gaveUp = gaveUp
	d.mu.Unlock()
	if !gaveUp {
		return
	}
	d.logger.Error("automatic certificate deploy gave up", "node", nodeID, "reason", reason, "attempts", certificateAttemptLimit)
	d.publish("system.alert", map[string]string{"alert": "certificate_deploy_failed", "nodeId": nodeID, "reason": reason})
	if d.record != nil {
		if err := d.record(ctx, "system", "证书自动发布失败", nodeID, "失败", ""); err != nil {
			d.logger.Error("automatic certificate failure could not be recorded", "node", nodeID, "error", err)
		}
	}
}

// describe names the renewed certificates for the revision summary: the first
// domain of each, sorted, so the summary does not depend on map order.
func (d *certificateDeployer) describe(ctx context.Context, inbounds []staleInbound) string {
	names := map[string]bool{}
	for _, inbound := range inbounds {
		name := inbound.certificateID
		if item, err := d.certificates.Repository.Get(ctx, inbound.certificateID); err == nil && len(item.Domains) > 0 {
			name = item.Domains[0]
		}
		names[name] = true
	}
	list := make([]string, 0, len(names))
	for name := range names {
		list = append(list, name)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}

func (d *certificateDeployer) logError(ctx context.Context, message string, err error) {
	if ctx.Err() == nil {
		d.logger.Error(message, "error", err)
	}
}

// withRenewedPairs is the published state with only the named inbounds' pair
// replaced. Everything else, including every other inbound's settings, the users
// and the enabled flags, is carried over untouched.
func withRenewedPairs(published desiredstate.State, inbounds []staleInbound) desiredstate.State {
	pairs := map[string]staleInbound{}
	for _, inbound := range inbounds {
		pairs[inbound.inboundID] = inbound
	}
	next := published
	next.Inbounds = make([]desiredstate.Inbound, len(published.Inbounds))
	copy(next.Inbounds, published.Inbounds)
	for index := range next.Inbounds {
		if pair, ok := pairs[next.Inbounds[index].ID]; ok {
			next.Inbounds[index].Settings.CertificatePEM = pair.certPEM
			next.Inbounds[index].Settings.PrivateKeyPEM = pair.keyPEM
		}
	}
	return next
}

// sanNames is the sorted DNS name set of a PEM's leaf, as one comparable key.
func sanNames(certPEM string) (string, bool) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return "", false
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil || len(leaf.DNSNames) == 0 {
		return "", false
	}
	names := append([]string(nil), leaf.DNSNames...)
	sort.Strings(names)
	return strings.ToLower(strings.Join(names, "\n")), true
}

// sameNames reports whether the published certificate covers exactly the names
// of the stored one. A renewal keeps the names; an inbound the Owner repointed to
// another certificate does not, and that edit waits for a manual publication
// instead of riding along on an automatic revision (AGENTS.md section 22.1). An
// empty or unparsable published PEM is never stale.
func sameNames(publishedPEM, storedNames string) bool {
	published, ok := sanNames(publishedPEM)
	return ok && storedNames != "" && published == storedNames
}
