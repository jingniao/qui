package user

import (
	"context"
	"errors"
	"sort"
	"sync"

	"proxycontrol/internal/panel/connections"
	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/traffic"
)

// ConnectionObserver turns the panel's in-memory live connection snapshots into
// two derived facts: the observed connection count shown for a user, and the
// "user is over their soft connection limit" alert. The limit is soft: it only
// warns, it never disconnects anyone.
type ConnectionObserver struct {
	Repository *Repository
	Nodes      interface {
		Group(context.Context, string) (node.Group, error)
	}
	Store   *connections.Store
	Publish func(string, any)

	mu   sync.Mutex
	over map[string]bool
}

// ForUser returns the live connection count for a user, or nil when it is
// unknown. The value is the sum of the per-credential, per-node distinct-IP
// counts the reporting nodes sent, so it is not deduplicated across nodes. It
// is unknown when no fresh snapshot covering the user names one of their active
// credentials: a user who only has WireGuard devices never gets an online
// entry, and a missing entry is not a report of zero. An explicit zero entry is
// a report and yields 0. Only the nodes in the user's granted groups count, and
// only while their snapshot is fresh.
func (o *ConnectionObserver) ForUser(ctx context.Context, account User) (*int, error) {
	if o == nil || o.Store == nil {
		return nil, nil
	}
	groups, err := o.Repository.Groups(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	snapshots := []traffic.ConnectionSnapshot{}
	for _, groupID := range groups {
		group, err := o.Nodes.Group(ctx, groupID)
		if errors.Is(err, node.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, nodeID := range group.NodeIDs {
			if seen[nodeID] {
				continue
			}
			seen[nodeID] = true
			if snapshot, ok := o.Store.Snapshot(nodeID); ok {
				snapshots = append(snapshots, snapshot)
			}
		}
	}
	if len(snapshots) == 0 {
		return nil, nil
	}
	credentials, err := o.Repository.ActiveCredentialIDs(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(credentials))
	for _, credentialID := range credentials {
		owned[credentialID] = true
	}
	// A snapshot that names none of the user's credentials says nothing about
	// them, so the count stays unknown instead of becoming an invented zero.
	matched := false
	for _, snapshot := range snapshots {
		for _, entry := range snapshot.Accounts {
			if owned[entry.CredentialID] {
				matched = true
				break
			}
		}
		if matched {
			break
		}
	}
	if !matched {
		return nil, nil
	}
	count := int(connections.Sum(snapshots, owned))
	return &count, nil
}

// Evaluate re-checks the users affected by a node's fresh snapshot and raises
// one system.alert per upward crossing of the limit. It is the Control
// notification hook, so it must never block or fail the agent stream: every
// error is swallowed.
func (o *ConnectionObserver) Evaluate(ctx context.Context, nodeID string) {
	if o == nil || o.Store == nil {
		return
	}
	snapshot, ok := o.Store.Snapshot(nodeID)
	if !ok {
		return
	}
	credentialIDs := make([]string, 0, len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		credentialIDs = append(credentialIDs, account.CredentialID)
	}
	owners, err := o.Repository.CredentialOwners(ctx, credentialIDs)
	if err != nil {
		return
	}
	candidates := map[string]bool{}
	for _, userID := range owners {
		candidates[userID] = true
	}
	// Users already over the limit stay candidates even when this snapshot no
	// longer names their credentials, so their state can return to normal.
	o.mu.Lock()
	for userID, over := range o.over {
		if over {
			candidates[userID] = true
		}
	}
	o.mu.Unlock()
	ordered := make([]string, 0, len(candidates))
	for userID := range candidates {
		ordered = append(ordered, userID)
	}
	sort.Strings(ordered)
	for _, userID := range ordered {
		account, err := o.Repository.Get(ctx, userID)
		if errors.Is(err, ErrNotFound) {
			o.retire(userID)
			continue
		}
		if err != nil {
			continue
		}
		count, err := o.ForUser(ctx, account)
		if err != nil || count == nil {
			continue
		}
		// v1.1 retired the per-user connection soft limit (plan C10); the column
		// stays for older backups, but no user is ever over it any more. The
		// online count is still reported.
		over := false
		if o.crossedIntoOver(userID, over) {
			o.alert(account, *count)
		}
	}
}

// crossedIntoOver records the new state and reports whether it is a fresh
// upward crossing.
func (o *ConnectionObserver) crossedIntoOver(userID string, over bool) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.over == nil {
		o.over = map[string]bool{}
	}
	previous := o.over[userID]
	o.over[userID] = over
	return over && !previous
}

func (o *ConnectionObserver) retire(userID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.over, userID)
}

func (o *ConnectionObserver) alert(account User, count int) {
	if o.Publish == nil {
		return
	}
	o.Publish("system.alert", map[string]any{
		"alert":             "connection_limit",
		"userId":            account.ID,
		"username":          account.Username,
		"onlineConnections": count,
		"maxConnections":    account.MaxConnections,
	})
}

// ActiveCredentialIDs returns the user's live credential IDs.
func (r *Repository) ActiveCredentialIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.Database.QueryContext(ctx, "SELECT id FROM credentials WHERE user_id=? AND status='active' ORDER BY id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CredentialOwners maps each requested credential ID to its user. It reads the
// whole credentials table instead of an IN list, so it is not bounded by the
// SQLite variable limit.
func (r *Repository) CredentialOwners(ctx context.Context, credentialIDs []string) (map[string]string, error) {
	wanted := map[string]bool{}
	for _, credentialID := range credentialIDs {
		if credentialID != "" {
			wanted[credentialID] = true
		}
	}
	owners := map[string]string{}
	if len(wanted) == 0 {
		return owners, nil
	}
	rows, err := r.Database.QueryContext(ctx, "SELECT id,user_id FROM credentials")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, userID string
		if err := rows.Scan(&id, &userID); err != nil {
			return nil, err
		}
		if wanted[id] {
			owners[id] = userID
		}
	}
	return owners, rows.Err()
}
