package user

import (
	"context"
	"errors"

	"proxycontrol/internal/panel/connections"
	"proxycontrol/internal/panel/node"
	"proxycontrol/pkg/contracts/traffic"
)

// ConnectionObserver reads live per-user connection counts.
type ConnectionObserver struct {
	Repository *Repository
	Nodes      interface {
		Group(context.Context, string) (node.Group, error)
	}
	Store *connections.Store
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
	return o.forUser(ctx, account, map[string]node.Group{})
}

func (o *ConnectionObserver) forUser(ctx context.Context, account User, cache map[string]node.Group) (*int, error) {
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
		group, found := cache[groupID]
		var err error
		if !found {
			group, err = o.Nodes.Group(ctx, groupID)
		}
		if errors.Is(err, node.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		cache[groupID] = group
		for _, nodeID := range group.AccessNodeIDs() {
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
