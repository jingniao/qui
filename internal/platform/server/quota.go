package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"proxycontrol/internal/panel/deployment"
	"proxycontrol/internal/panel/node"
	"proxycontrol/internal/panel/proxy"
	"proxycontrol/internal/panel/user"
	"proxycontrol/pkg/contracts/desiredstate"
)

// quotaSnapshotter is the existing Owner-UI renderer. Automatic revisions
// reuse it and go through the Deployment module, never a parallel renderer.
type quotaSnapshotter interface {
	Snapshot(context.Context, string, proxy.AccessReader) (desiredstate.State, error)
}

const (
	defaultQuotaWindow = 60 * time.Second
	defaultQuotaTick   = time.Minute
	// quotaAttemptLimit is how many failed attempts one node's entry may make
	// before the deployer gives up on it and reports the failure. Waiting for a
	// busy node or for the agent's result never counts as an attempt.
	quotaAttemptLimit = 3
	// quotaStartupPageSize is the node page the startup pass walks; the panel
	// targets a handful of nodes.
	quotaStartupPageSize = 500
)

// quotaDeployer turns user activity transitions into debounced automatic
// revisions. Transitions affecting one node coalesce into a single deploy at
// the end of the debounce window, so a node receives at most one automatic
// deploy per window, and a steady state (no transitions) produces none.
//
// An entry is only forgotten once its work is really done: a revision exists
// and the node reported it applied. A queued deploy is therefore not the end of
// the entry, and a revision the node failed or rolled back is retried with a
// new revision built from the current published state. Work that keeps failing
// is reported to the operator instead of disappearing silently.
type quotaDeployer struct {
	logger      *slog.Logger
	users       *user.Repository
	quota       *user.QuotaState
	proxy       quotaSnapshotter
	nodes       *node.Repository
	deployments *deployment.Repository
	publish     func(string, any)
	// record writes the admin operation for a failure the Owner has to know
	// about; it is the system repository's Record, wired by the platform.
	record func(ctx context.Context, actor, action, target, result, ip string) error
	window time.Duration
	tick   time.Duration

	mu      sync.Mutex
	pending map[string]*quotaPending
	timers  map[string]*time.Timer
	// held keeps transitions whose node set could not be read, so a transient
	// read failure delays the work instead of dropping it.
	held []user.Transition
	// recovered records that the startup pass listed every node and read every
	// published state without error. Until it is set, each tick runs the pass
	// again, so a transient read failure at startup cannot skip a node for the
	// lifetime of the process.
	recovered bool
	// flushing counts the flushes that took their entry and are still
	// reconciling, so a caller can tell an idle node from one whose window just
	// fired.
	flushing int
	// stopped marks a deployer whose run has returned. Hooks such as
	// reconcileNode and accessRemoved are still called by other modules while
	// Serve shuts down, so enqueue and requeue drop their work instead of
	// scheduling a timer: nothing would ever flush it, and the next process
	// re-derives every entry in its startup pass.
	stopped bool
	// base is the run context the flushes derive from, so cancelling the panel's
	// background work also cuts short a reconcile that is already running. It is
	// nil before run starts (tests drive flush directly); the one-minute bound is
	// then all there is to stop a reconcile.
	base context.Context
	// stopRun cancels base. shutdown calls it so an in-flight flush always sees
	// the shutdown, whatever made run return.
	stopRun context.CancelFunc
	// idle is broadcast whenever a flush finishes, so shutdown can wait for the
	// flushes in flight without polling. It guards the same mutex as flushing.
	idle *sync.Cond
}

// quotaPending is the coalesced work for one node. revision records a revision
// that was created but whose fate is not known yet, so a retry never creates a
// duplicate; attempts counts failed attempts and stops the retries at
// quotaAttemptLimit; deferred marks an entry whose last render had to fall back
// to the published state's own user list.
type quotaPending struct {
	transitions []user.Transition
	revision    int64
	attempts    int
	deferred    bool
}

func newQuotaDeployer(logger *slog.Logger, users *user.Repository, quota *user.QuotaState, renderer quotaSnapshotter, nodes *node.Repository, deployments *deployment.Repository, publish func(string, any)) *quotaDeployer {
	deployer := &quotaDeployer{
		logger: logger, users: users, quota: quota, proxy: renderer, nodes: nodes, deployments: deployments, publish: publish,
		window: defaultQuotaWindow, tick: defaultQuotaTick,
		pending: map[string]*quotaPending{}, timers: map[string]*time.Timer{},
	}
	// The condition variable guards the same mutex as flushing, so the waiter
	// can never miss the broadcast of the last flush.
	deployer.idle = sync.NewCond(&deployer.mu)
	return deployer
}

// evaluate is the single ingress: it reports the transitions for the given
// users, publishes one SSE alert per transition, and schedules the affected
// nodes.
func (d *quotaDeployer) evaluate(ctx context.Context, userIDs []string) {
	d.emitTransitions(ctx, d.evaluateTransitions(ctx, userIDs))
}

func (d *quotaDeployer) evaluateUser(ctx context.Context, userID string) {
	d.evaluate(ctx, []string{userID})
}

func (d *quotaDeployer) evaluateTransitions(ctx context.Context, userIDs []string) []user.Transition {
	transitions, err := d.quota.Evaluate(ctx, userIDs)
	if err != nil {
		d.logger.Error("quota evaluation failed", "error", err)
	}
	return transitions
}

func (d *quotaDeployer) emitTransitions(ctx context.Context, transitions []user.Transition) {
	for _, transition := range transitions {
		d.publish("system.alert", map[string]string{"alert": "user_access", "userId": transition.UserID, "username": transition.Username, "reason": transition.Reason})
		d.schedule(ctx, transition)
	}
}

// renewExpiry is the Owner-save hook for a Save that changed expires_at. The
// agent drops users whose own copy of ExpiresAt has passed, so a renewal that
// never reaches the nodes still cuts the user off on the old date. Renewing a
// user who stays active is not an activity transition, hence the refresh is
// scheduled directly and without a system.alert; when the edit did cross an
// activity boundary the ordinary alert and transition already cover it.
func (d *quotaDeployer) renewExpiry(ctx context.Context, userID string) {
	transitions := d.evaluateTransitions(ctx, []string{userID})
	if len(transitions) > 0 {
		d.emitTransitions(ctx, transitions)
		return
	}
	account, err := d.users.Get(ctx, userID)
	if err != nil {
		d.logger.Error("quota expiry refresh could not read user", "user", userID, "error", err)
		return
	}
	d.schedule(ctx, user.Transition{UserID: userID, Username: account.Username, Reason: "renewed"})
}

// accessRemoved is the Owner-delete hook for a proxy user and for one of its
// devices. A delete takes the user row, its devices, its credentials, its node
// group grants and its subscription token with it, so nothing an Owner can edit
// afterwards points at the nodes any more: the nodes would keep accepting the
// deleted credential until someone republished by hand. schedule still finds
// them, because every node whose published state carries one of the user's
// credentials is queued on its own, and reconcile renders the current — now
// smaller — user access surface onto that published state.
//
// The deletion is not an activity transition: QuotaState compares a user record
// against its previous reason, and the record is gone. The transition is
// therefore scheduled directly, like renewExpiry. The alert is not, though:
// renewExpiry stays silent because a renewal keeps the same access with a later
// deadline, while a deletion takes access away, which is exactly what the
// user_access alert of emitTransitions announces for every credential-affecting
// transition. It is published here with that same payload shape.
func (d *quotaDeployer) accessRemoved(ctx context.Context, userID, username, reason string) {
	if userID == "" {
		return
	}
	d.publish("system.alert", map[string]string{"alert": "user_access", "userId": userID, "username": username, "reason": reason})
	d.schedule(ctx, user.Transition{UserID: userID, Username: username, Reason: reason})
	// Only a deleted user is forgotten. A deleted device leaves its owner very
	// much alive, and dropping the owner's entry would swallow its next
	// transition: Evaluate reports changes only against a previous state it
	// still tracks.
	if _, err := d.users.Get(ctx, userID); errors.Is(err, user.ErrNotFound) {
		d.quota.Forget(userID)
	}
}

// accessChanged is the Owner-save hook for a user whose credentials or node
// group grants may have changed: a protocol change issues or revokes
// credentials, a credentials reset replaces all of them, a grant change adds or
// removes nodes. Like renewExpiry it is not an activity transition, so the
// user's nodes are scheduled directly and without an alert; the reconciliation
// creates a revision only for nodes whose user access surface differs.
func (d *quotaDeployer) accessChanged(ctx context.Context, userID, username, reason string) {
	if userID == "" {
		return
	}
	d.schedule(ctx, user.Transition{UserID: userID, Username: username, Reason: reason})
}

// reconcileNode queues one reconciliation of a node's user access surface. The
// Deployment module calls it after every successful deploy and rollback: both
// re-publish a stored revision verbatim, so a rollback to a revision rendered
// before a user was disabled (or re-enabled) would otherwise keep serving that
// credential until the next transition or a panel restart. Like any other
// transition the entry coalesces into the next window, and the reconciliation
// itself computes nothing when the node is already in sync, so an automatic
// revision's own success cannot start a chain of revisions.
func (d *quotaDeployer) reconcileNode(nodeID, reason string) {
	if nodeID == "" {
		return
	}
	d.enqueue(nodeID, user.Transition{Reason: reason})
}

// tickOnce runs the startup pass again while it has not completed, retries the
// transitions whose node set could not be read, and then re-evaluates every
// user, so an idle expired user is still removed.
func (d *quotaDeployer) tickOnce(ctx context.Context) {
	d.mu.Lock()
	recovered := d.recovered
	d.mu.Unlock()
	if !recovered {
		d.recover(ctx)
	}
	d.retryHeld(ctx)
	userIDs, err := d.quota.AllUserIDs(ctx)
	if err != nil {
		d.logger.Error("quota tick failed", "error", err)
		return
	}
	d.evaluate(ctx, userIDs)
}

// retryHeld re-runs the transitions kept by holdTransition.
func (d *quotaDeployer) retryHeld(ctx context.Context) {
	d.mu.Lock()
	held := append([]user.Transition(nil), d.held...)
	d.mu.Unlock()
	for _, transition := range held {
		d.schedule(ctx, transition)
	}
}

// run drives the deployer until ctx is cancelled and then shuts it down, so that
// the panel's wait group covers the flushes still in flight (see shutdown).
// Serve starts exactly one run; a restarted panel builds a new deployer.
func (d *quotaDeployer) run(ctx context.Context) {
	// Flushes derive from a context run owns, so shutdown can cut a reconcile
	// short even when the cancellation arrives through some other path.
	base, stop := context.WithCancel(ctx)
	d.mu.Lock()
	// A deployer normally runs once, and a restarted panel builds a new one.
	// Clearing the flag keeps a second run working instead of silently dropping
	// every hook it receives.
	d.stopped = false
	d.base, d.stopRun = base, stop
	d.mu.Unlock()
	defer d.shutdown()
	// QuotaState.Seed only knows the state it was seeded with, so a user who
	// went over quota, expired or was disabled while the panel was down
	// produces no transition. Reconciling every node with a published state
	// once at startup catches those, and a node already in sync computes no
	// revision at all.
	d.recover(base)
	ticker := time.NewTicker(d.tick)
	defer ticker.Stop()
	for {
		select {
		case <-base.Done():
			return
		case <-ticker.C:
			d.tickOnce(base)
		}
	}
}

// shutdown ends the deployer. It marks itself stopped, so enqueue and requeue no
// longer schedule timers, stops every pending debounce timer, cancels the run
// context (which is what makes an in-flight reconcile give up), and then waits
// until no flush is in flight. Serve's wait group only covers run, so without
// that wait a flush would keep running after Serve returned and fail on the
// database the panel is closing: the 2026-10-25 in-process restore logged
// "sql: database is closed" once a minute for three minutes and then reported a
// failure the new instance had already handled. Work left in the queues is
// dropped on purpose; the next process re-derives everything in its startup
// pass, so nothing is lost.
func (d *quotaDeployer) shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	if d.stopRun != nil {
		d.stopRun()
	}
	for nodeID, timer := range d.timers {
		if timer != nil {
			timer.Stop()
		}
		d.timers[nodeID] = nil
	}
	for d.flushing > 0 {
		d.idle.Wait()
	}
}

// stopping reports whether the deployer is shutting down or its run context has
// been cancelled. A failure seen in that window is the shutdown cutting a read
// or a deploy short, not a node or data problem: it must not be counted as an
// attempt, reported to the Owner, or retried, because the next process's startup
// pass re-derives the work anyway.
func (d *quotaDeployer) stopping() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped || d.base != nil && d.base.Err() != nil
}

// recover enqueues one reconciliation per node that already runs a published
// revision, and records that the pass completed. A failed read leaves the pass
// unfinished so the next tick runs it again instead of skipping the node.
func (d *quotaDeployer) recover(ctx context.Context) {
	items, _, err := d.nodes.List(ctx, "", 1, quotaStartupPageSize, false)
	if err != nil {
		d.logger.Error("automatic quota startup pass could not list nodes", "error", err)
		return
	}
	complete := true
	for _, n := range items {
		if _, err := d.deployments.Published(ctx, n.ID); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				d.logger.Error("automatic quota startup pass could not read published state", "node", n.ID, "error", err)
				complete = false
			}
			continue
		}
		d.enqueue(n.ID, user.Transition{Reason: "startup"})
	}
	if complete {
		d.mu.Lock()
		d.recovered = true
		d.mu.Unlock()
	}
}

// schedule finds the nodes that currently run a deployed revision and are in
// one of the user's granted node groups, plus every node whose published state
// still carries one of the user's credentials, then queues the transition on
// each. A node set that cannot be read is not a decision: the transition is held
// and retried on the next tick.
func (d *quotaDeployer) schedule(ctx context.Context, transition user.Transition) {
	if err := d.scheduleTransition(ctx, transition); err != nil {
		d.logger.Error("quota redeploy could not resolve the node set", "user", transition.UserID, "error", err)
		d.holdTransition(transition)
		return
	}
	d.releaseTransition(transition.UserID)
}

func (d *quotaDeployer) scheduleTransition(ctx context.Context, transition user.Transition) error {
	groups, err := d.users.Groups(ctx, transition.UserID)
	if err != nil {
		return err
	}
	queued := map[string]bool{}
	for _, groupID := range groups {
		group, err := d.nodes.Group(ctx, groupID)
		if errors.Is(err, node.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		for _, nodeID := range group.NodeIDs {
			// Only nodes with a published revision can host an automatic
			// revision. The Deployment module owns this read; the panel never
			// queries deployment tables directly.
			if _, err := d.deployments.Published(ctx, nodeID); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return err
			}
			queued[nodeID] = true
			d.enqueue(nodeID, transition)
		}
	}
	// A grant the Owner removed but did not publish leaves the node running the
	// user's credential while the user is no longer in any granted group, so a
	// later disable would never reach it. Every node whose published state still
	// holds one of the user's credentials is queued as well. A deleted node is
	// never queued: its Agent can no longer apply a revision.
	items, _, err := d.nodes.List(ctx, "", 1, quotaStartupPageSize, false)
	if err != nil {
		return err
	}
	for _, n := range items {
		if queued[n.ID] {
			continue
		}
		published, err := d.deployments.Published(ctx, n.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if !stateHoldsUser(published, transition.UserID) {
			continue
		}
		d.enqueue(n.ID, transition)
	}
	return nil
}

// stateHoldsUser reports whether a published state still carries a credential
// belonging to the user. Internal credentials carry no user, so an empty user ID
// never matches.
func stateHoldsUser(state desiredstate.State, userID string) bool {
	if userID == "" {
		return false
	}
	for _, credential := range state.Credentials {
		if credential.UserID == userID {
			return true
		}
	}
	return false
}

// holdTransition remembers a transition whose node set could not be read.
func (d *quotaDeployer) holdTransition(transition user.Transition) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for index := range d.held {
		if d.held[index].UserID == transition.UserID {
			d.held[index] = transition
			return
		}
	}
	d.held = append(d.held, transition)
}

// releaseTransition forgets a held transition once it has been scheduled.
func (d *quotaDeployer) releaseTransition(userID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for index := range d.held {
		if d.held[index].UserID == userID {
			d.held = append(d.held[:index], d.held[index+1:]...)
			return
		}
	}
}

func (d *quotaDeployer) enqueue(nodeID string, transition user.Transition) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		// The panel is shutting down. Recording the work would be pointless —
		// shutdown stops the timers and nothing would flush it — so it is
		// dropped, and the next process re-derives it in its startup pass.
		return
	}
	entry := d.pending[nodeID]
	if entry == nil {
		entry = &quotaPending{}
		d.pending[nodeID] = entry
	}
	mergeTransition(entry, transition)
	if d.timers[nodeID] == nil {
		d.timers[nodeID] = time.AfterFunc(d.window, func() { d.flush(nodeID) })
	}
}

// mergeTransition adds a transition to an entry, replacing that user's earlier
// one so a single user never occupies an entry twice.
func mergeTransition(entry *quotaPending, transition user.Transition) {
	for index := range entry.transitions {
		if entry.transitions[index].UserID == transition.UserID {
			entry.transitions[index] = transition
			return
		}
	}
	entry.transitions = append(entry.transitions, transition)
}

// requeue puts an entry back for another window. Work scheduled meanwhile is
// merged into it: transitions are keyed by user, and a revision the entry still
// tracks survives so the next window reads its status instead of creating a
// duplicate.
func (d *quotaDeployer) requeue(nodeID string, entry *quotaPending) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		// Shutdown has already stopped every timer; scheduling another one here
		// would flush into a database the panel is closing.
		return
	}
	current := d.pending[nodeID]
	if current == nil {
		d.pending[nodeID] = entry
	} else {
		for _, transition := range entry.transitions {
			mergeTransition(current, transition)
		}
		if current.revision == 0 {
			current.revision, current.attempts = entry.revision, entry.attempts
		}
		if entry.deferred {
			current.deferred = true
		}
	}
	if d.timers[nodeID] == nil {
		d.timers[nodeID] = time.AfterFunc(d.window, func() { d.flush(nodeID) })
	}
}

// quotaOutcome tells flush what to do with an entry after one attempt.
type quotaOutcome int

const (
	// quotaDone: the entry's work is finished or must not be retried.
	quotaDone quotaOutcome = iota
	// quotaRetry: the attempt failed; requeue the entry.
	quotaRetry
	// quotaWait: nothing is wrong, but the result is not in yet (the node is
	// busy, the agent has not answered, the render is deferred); requeue it
	// without counting an attempt.
	quotaWait
)

// flush builds and deploys one revision for a node at the end of the window. Its
// context derives from the run context, so a shutdown cuts a reconcile short
// instead of letting it hit the closing database, and the result of such a
// cancelled reconcile is not counted as an attempt (see stopping).
func (d *quotaDeployer) flush(nodeID string) {
	d.mu.Lock()
	if d.stopped {
		// The timer fired while shutdown was stopping it; the work goes away
		// with the rest of the queued state.
		d.mu.Unlock()
		return
	}
	entry := d.pending[nodeID]
	delete(d.pending, nodeID)
	d.timers[nodeID] = nil
	d.flushing++
	base := d.base
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.flushing--
		d.idle.Broadcast()
		d.mu.Unlock()
	}()
	if entry == nil || len(entry.transitions) == 0 {
		return
	}
	if base == nil {
		// No run owns this flush (the tests drive flush directly, and a hook
		// may fire before run started): the one-minute bound is all there is.
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, time.Minute)
	defer cancel()
	if d.reconcile(ctx, nodeID, entry) == quotaDone || d.stopping() {
		// The shutdown cut the reconcile short. The entry is dropped instead of
		// retried: its attempt was not counted, requeue would schedule a timer
		// on a deployer that is going away, and the next process re-derives the
		// work in its startup pass.
		return
	}
	d.requeue(nodeID, entry)
}

// reconcile advances one node's entry by one step and reports what flush should
// do with it.
func (d *quotaDeployer) reconcile(ctx context.Context, nodeID string, entry *quotaPending) quotaOutcome {
	if entry.revision != 0 {
		status, err := d.deployments.TargetStatus(ctx, nodeID, entry.revision)
		if err != nil {
			d.logger.Error("automatic quota revision could not read its target status", "node", nodeID, "revision", entry.revision, "error", err)
			return d.fail(ctx, nodeID, entry, "status_unreadable")
		}
		switch status {
		case "pending":
			// The agent has not answered yet; wait another window.
			return quotaWait
		case "success":
			// The node runs the automatic revision. Recomputing below picks up
			// transitions queued while it was applying; that is usually a no-op
			// and drops the entry.
			entry.revision, entry.attempts = 0, 0
		case "failed", "rollback":
			// The node refused the revision and kept its previous
			// configuration. Deploying the same revision again is a no-op, so a
			// new one is created below.
			entry.revision = 0
			if outcome := d.fail(ctx, nodeID, entry, "agent_"+status); outcome == quotaDone {
				return quotaDone
			}
		case "draft":
			// Created but never queued; it may be reused below.
		default:
			d.logger.Error("automatic quota revision has an unknown target status", "node", nodeID, "revision", entry.revision, "status", status)
			entry.revision = 0
			if outcome := d.fail(ctx, nodeID, entry, "unknown_status"); outcome == quotaDone {
				return quotaDone
			}
		}
	}
	// Base the automatic revision on what the node is currently running. The
	// fresh render is used only as the current user/credential view, so an Owner
	// edit that is saved but not yet published can never ride along on a quota
	// transition (AGENTS.md §22).
	published, intent, err := d.deployments.PublishedWithIntent(ctx, nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		// Nothing published yet: there is no reviewed state to adjust.
		return quotaDone
	}
	if err != nil {
		d.logger.Error("automatic quota revision could not read published state", "node", nodeID, "error", err)
		return d.fail(ctx, nodeID, entry, "published_unreadable")
	}
	fresh, err := d.proxy.Snapshot(ctx, nodeID, d.users)
	var next desiredstate.State
	if err != nil {
		// The Owner's saved draft may not render at all (an unrelated invalid
		// inbound, for example). That must not keep an inactive user authorized
		// on the node, so the published state's own user list is evaluated
		// against the current user records instead. The entry is deferred so the
		// regular render is retried once the draft renders again.
		d.logger.Warn("automatic quota revision rendered from the published state alone", "node", nodeID, "error", err)
		next, err = d.publishedFallback(ctx, published)
		if err != nil {
			d.logger.Error("automatic quota revision could not read user state", "node", nodeID, "error", err)
			return d.fail(ctx, nodeID, entry, "user_read_failed")
		}
		entry.deferred = true
	} else {
		entry.deferred = false
		next = mergeUserAccess(published, fresh, intent)
	}
	if quotaStatesEqual(published, next) {
		if !entry.deferred {
			// The node's current configuration already reflects the change.
			return quotaDone
		}
		return quotaWait
	}
	if entry.revision != 0 {
		draft, err := d.deployments.State(ctx, entry.revision)
		if err != nil {
			d.logger.Error("automatic quota revision could not read its revision", "node", nodeID, "revision", entry.revision, "error", err)
			return d.fail(ctx, nodeID, entry, "revision_unreadable")
		}
		if !quotaStatesEqual(draft, next) {
			// The revision was overtaken: an Owner revision was published
			// meanwhile, so deploying this one would revert that change. A new
			// revision is created from the current published state instead.
			entry.revision = 0
		}
	}
	if entry.revision == 0 {
		// The intent is the published revision's, unchanged: an automatic
		// revision never adopts the draft's idea of which inbounds should be
		// enabled, and a later automatic revision inherits it again.
		revision, err := d.deployments.CreateWithInboundIntent(ctx, next, intent, "auto", "auto: "+quotaSummary(entry.transitions))
		if err != nil {
			d.logger.Error("automatic quota revision failed", "node", nodeID, "error", err)
			return d.fail(ctx, nodeID, entry, "revision_failed")
		}
		entry.revision = revision.ID
	}
	if err := d.deployments.Deploy(ctx, entry.revision); err != nil {
		if errors.Is(err, deployment.ErrNodeBusy) {
			// Another deployment owns the node; the revision stays a draft and
			// the next window queues it.
			return quotaWait
		}
		d.logger.Error("automatic quota deploy failed", "node", nodeID, "revision", entry.revision, "error", err)
		return d.fail(ctx, nodeID, entry, "deploy_failed")
	}
	// A queued deploy is not an applied one: the next window reads the agent's
	// result and retries with a new revision when the node rolled back.
	return quotaWait
}

// publishedFallback derives the user-access surface from the published state
// alone. A user whose current record is inactive or gone is disabled here, and
// RuntimeAt then drops that user's credentials, inbound bindings and WireGuard
// peers. A published credential that no longer exists or now belongs to another
// user is dropped the same way: deleting one device of an active user is not an
// activity transition, its credential is cascade-deleted from the credentials
// table, and only this check takes the credential off the node. Either way
// the result is a valid revision that takes access away without touching
// anything else the node runs.
func (d *quotaDeployer) publishedFallback(ctx context.Context, published desiredstate.State) (desiredstate.State, error) {
	next := published
	next.Users = append([]desiredstate.User(nil), published.Users...)
	now := d.now()
	for index := range next.Users {
		account, err := d.users.Get(ctx, next.Users[index].ID)
		if errors.Is(err, user.ErrNotFound) {
			next.Users[index].Enabled = false
			continue
		}
		if err != nil {
			return desiredstate.State{}, err
		}
		if user.InactivityReason(account, now) != "" {
			next.Users[index].Enabled = false
		}
	}
	publishedIDs := []string{}
	for _, credential := range published.Credentials {
		if !credential.Internal && credential.UserID != "" {
			publishedIDs = append(publishedIDs, credential.ID)
		}
	}
	owners, err := d.users.CredentialOwners(ctx, publishedIDs)
	if err != nil {
		return desiredstate.State{}, err
	}
	removed := map[string]bool{}
	for _, credential := range published.Credentials {
		if credential.Internal || credential.UserID == "" {
			continue
		}
		if owner, exists := owners[credential.ID]; !exists || owner != credential.UserID {
			removed[credential.ID] = true
		}
	}
	if len(removed) > 0 {
		next, err = next.WithoutCredentials(removed)
		if err != nil {
			return desiredstate.State{}, err
		}
	}
	next, err = d.filterPublishedGroupAccess(ctx, next)
	if err != nil {
		return desiredstate.State{}, err
	}
	return next.RuntimeAt(now)
}

// now is the deployer's clock; tests drive it through the QuotaState.
func (d *quotaDeployer) now() time.Time {
	if d.quota != nil && d.quota.Now != nil {
		return d.quota.Now()
	}
	return time.Now()
}

// fail counts one failed attempt. Reaching quotaAttemptLimit stops the retries
// for the entry and tells the operator through a log line, one SSE alert and
// one admin operation, so an over-quota user is never silently left authorized.
// A failure that is only the shutdown cutting the work short is not an attempt
// at all: counting it would make a restart look like a node that keeps failing
// and could publish a give-up alert for work the next process handles.
func (d *quotaDeployer) fail(ctx context.Context, nodeID string, entry *quotaPending, reason string) quotaOutcome {
	if d.stopping() {
		return quotaWait
	}
	entry.attempts++
	if entry.attempts < quotaAttemptLimit {
		return quotaRetry
	}
	d.logger.Error("automatic quota revision gave up", "node", nodeID, "reason", reason, "attempts", entry.attempts, "users", quotaSummary(entry.transitions))
	d.publish("system.alert", map[string]string{"alert": "user_access_failed", "nodeId": nodeID, "reason": reason})
	if d.record != nil {
		if err := d.record(ctx, "system", "自动额度发布失败", nodeID, "失败", ""); err != nil {
			d.logger.Error("automatic quota failure could not be recorded", "node", nodeID, "error", err)
		}
	}
	return quotaDone
}

// inboundEnabledByOwner reports whether the recorded intent of the published
// revision names this inbound. An unknown intent (nil) answers no.
func inboundEnabledByOwner(ownerEnabled []string, inboundID string) bool {
	for _, id := range ownerEnabled {
		if id == inboundID {
			return true
		}
	}
	return false
}

// quotaSummary describes the work of one entry for the revision summary. The
// startup pass has no user, so its summary reads "startup reconcile".
func quotaSummary(transitions []user.Transition) string {
	ordered := append([]user.Transition(nil), transitions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Username < ordered[j].Username })
	parts := make([]string, 0, len(ordered))
	for _, transition := range ordered {
		if transition.Username == "" {
			parts = append(parts, transition.Reason+" reconcile")
			continue
		}
		parts = append(parts, "user "+transition.Username+" "+transition.Reason)
	}
	return strings.Join(parts, "; ")
}

// quotaStatesEqual compares two rendered states ignoring the revision label.
func quotaStatesEqual(left, right desiredstate.State) bool {
	left.Revision, right.Revision = 0, 0
	first, err := json.Marshal(left)
	if err != nil {
		return false
	}
	second, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return bytes.Equal(first, second)
}

// mergeUserAccess returns a copy of the published state whose only difference
// is its user-access surface, taken from the fresh render:
//
//   - Users: exactly the fresh view's users. The agent enforces ExpiresAt from
//     this entry itself, so a renewal must reach the nodes even though it is
//     not an activity transition; keeping the published entry would freeze the
//     old deadline on the node.
//   - Credentials: the published internal credentials exactly as published,
//     followed by the fresh view's user credentials. A relay or outbound
//     credential belongs to the published business configuration, so an
//     unpublished Owner edit must neither drop a credential a published
//     outbound still references nor add one; a user credential still follows
//     the fresh view, so a removed user's credential disappears and a
//     reactivated one reappears. Published credentials keep their order and
//     fresh-only ones are appended, so an unchanged user surface merges back
//     byte-for-byte.
//   - Each published inbound's CredentialIDs: its published internal IDs plus
//     the fresh state's user assignment for the inbound with the same ID,
//     keeping only credentials whose protocol matches the published inbound.
//     When the inbound is absent from the fresh state its published user IDs
//     are retained only while they still exist in the merged credential set.
//     Published IDs keep their position and fresh-only ones are appended.
//   - Each WireGuard inbound's peers: peers without a credential (literal keys)
//     and peers bound to an internal credential are carried over as published;
//     user peers follow the fresh inbound with the same ID, so a peer whose
//     user became inactive disappears and a rotated public key is picked up.
//     Without such a fresh inbound the published user peers are retained only
//     while their credential survives. A credential-bound peer always has its
//     CredentialID in the inbound's merged CredentialIDs, because the cores
//     refuse to render a peer whose credential is gone. The WireGuard settings
//     value and its slices are copied, never edited in place: the published
//     state has to stay untouched.
//
// The user bindings inside a fresh inbound are taken from the current draft:
// binding an active user's credential to a published inbound is
// indistinguishable from a published binding, so such an unpublished edit rides
// along. That is the only accepted exception.
//
// ownerEnabled is the inbound intent of the published revision: the inbounds the
// Owner had enabled when it was created, before the renderer disabled the ones no
// user credential reached. An inbound the published state leaves disabled is
// re-enabled only when that intent names it, so an inbound the Owner disabled on
// purpose stays off even while the draft enables it again. An unknown intent
// (a nil slice, from a revision published before the intent was recorded) never
// re-enables anything: the conservative reading cannot publish an administrative
// disable, and the Owner's next publication settles it.
//
// Every other field of the published state (the inbounds' other fields,
// outbounds, groups, chains, policies and logging) is carried over byte-for-byte
// so an automatic revision can never publish an unreviewed Owner edit.
func mergeUserAccess(published, fresh desiredstate.State, ownerEnabled []string) desiredstate.State {
	next := published
	next.Users = make([]desiredstate.User, len(fresh.Users))
	copy(next.Users, fresh.Users)

	freshCredentials := map[string]desiredstate.Credential{}
	for _, credential := range fresh.Credentials {
		freshCredentials[credential.ID] = credential
	}
	credentials := []desiredstate.Credential{}
	seen := map[string]bool{}
	for _, credential := range published.Credentials {
		if !credential.Internal {
			updated, exists := freshCredentials[credential.ID]
			if !exists || updated.Internal {
				continue
			}
			credential = updated
		}
		credentials = append(credentials, credential)
		seen[credential.ID] = true
	}
	for _, credential := range fresh.Credentials {
		if credential.Internal || seen[credential.ID] {
			continue
		}
		credentials = append(credentials, credential)
		seen[credential.ID] = true
	}
	next.Credentials = credentials

	byID := map[string]desiredstate.Credential{}
	for _, credential := range credentials {
		byID[credential.ID] = credential
	}
	freshByID := map[string]desiredstate.Inbound{}
	for _, inbound := range fresh.Inbounds {
		freshByID[inbound.ID] = inbound
	}
	inbounds := make([]desiredstate.Inbound, len(published.Inbounds))
	copy(inbounds, published.Inbounds)
	for index := range inbounds {
		inbound := &inbounds[index]
		// Forward listeners carry no user identity. Automatic user-access
		// revisions preserve their published state, including enabled intent.
		if inbound.Protocol == "forward" {
			continue
		}
		hadCredentials := len(inbound.CredentialIDs) > 0
		source, inFresh := freshByID[inbound.ID]
		freshIDs := map[string]bool{}
		for _, id := range source.CredentialIDs {
			freshIDs[id] = true
		}
		ids := []string{}
		bound := map[string]bool{}
		for _, id := range inbound.CredentialIDs {
			credential, exists := byID[id]
			if !exists || credential.Protocol != inbound.Protocol {
				continue
			}
			// An internal credential belongs to the published configuration
			// and stays attached; a user credential must still be authorized
			// on this exact inbound. Deleting/moving one inbound must not retain
			// its binding merely because another inbound uses the same secret.
			if !credential.Internal && (!inFresh || !freshIDs[id]) {
				continue
			}
			ids = append(ids, id)
			bound[id] = true
		}
		if inFresh {
			for _, id := range source.CredentialIDs {
				credential, exists := byID[id]
				if !exists || credential.Internal || bound[id] || credential.Protocol != inbound.Protocol {
					continue
				}
				ids = append(ids, id)
				bound[id] = true
			}
		}
		inbound.CredentialIDs = ids
		if inbound.Settings.WireGuard != nil {
			settings := *inbound.Settings.WireGuard
			settings.Addresses = append([]string(nil), settings.Addresses...)
			// The peer list is rebuilt, never edited in place, and an absent
			// published list stays absent so an unchanged inbound marshals back
			// to the same bytes.
			settings.Peers = nil
			if inbound.Settings.WireGuard.Peers != nil {
				settings.Peers = []desiredstate.WireGuardPeer{}
			}
			var freshPeers []desiredstate.WireGuardPeer
			freshPeerByID := map[string]desiredstate.WireGuardPeer{}
			if source.Settings.WireGuard != nil {
				freshPeers = source.Settings.WireGuard.Peers
				for _, peer := range freshPeers {
					if peer.CredentialID != "" {
						freshPeerByID[peer.CredentialID] = peer
					}
				}
			}
			kept := map[string]bool{}
			for _, peer := range inbound.Settings.WireGuard.Peers {
				credential, exists := byID[peer.CredentialID]
				if peer.CredentialID != "" && (!exists || !credential.Internal) {
					// A user peer survives only while its credential is still
					// bound to this inbound, and a compatible fresh inbound
					// supplies the current public key and allowed addresses.
					if !exists || !bound[peer.CredentialID] {
						continue
					}
					if source.Settings.WireGuard != nil {
						updated, listed := freshPeerByID[peer.CredentialID]
						if !listed {
							continue
						}
						peer = updated
					}
				}
				copied := peer
				copied.AllowedIPs = append([]string(nil), peer.AllowedIPs...)
				settings.Peers = append(settings.Peers, copied)
				kept[peer.CredentialID] = true
			}
			for _, peer := range freshPeers {
				credential, exists := byID[peer.CredentialID]
				if peer.CredentialID == "" || kept[peer.CredentialID] || !exists || credential.Internal || !bound[peer.CredentialID] {
					continue
				}
				copied := peer
				copied.AllowedIPs = append([]string(nil), peer.AllowedIPs...)
				settings.Peers = append(settings.Peers, copied)
				kept[peer.CredentialID] = true
			}
			inbound.Settings.WireGuard = &settings
		}
		switch {
		case len(ids) == 0:
			// Mirror the renderer: an inbound left without credentials is
			// disabled, which also satisfies DesiredState validation.
			inbound.Enabled = false
		case !hadCredentials && source.Enabled && inboundEnabledByOwner(ownerEnabled, inbound.ID):
			// The published inbound held no credentials, so the renderer had
			// disabled it; credentials are back and the Owner wants the inbound
			// enabled, so it is enabled again. Without that recorded intent the
			// fresh view cannot be trusted: the inbound may be disabled because
			// the Owner disabled it, and only the draft enables it now.
			inbound.Enabled = true
		}
	}
	next.Inbounds = inbounds
	return next
}
