package user

import (
	"context"
	"errors"
	"sync"
	"time"
)

// InactivityReason reports why a user must be excluded from rendered node
// configuration, or "" while the user is active. It checks the same conditions
// in the same order as the former User.Active body, so User.Active is now
// simply "no inactivity reason".
func InactivityReason(u User, at time.Time) string {
	if u.Status != "active" {
		return "disabled"
	}
	if u.TrafficLimit > 0 && u.UsedTraffic >= u.TrafficLimit {
		return "quota_exceeded"
	}
	if u.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339Nano, u.ExpiresAt)
		if err != nil || !expiry.After(at) {
			return "expired"
		}
	}
	return ""
}

// Transition is a change of a user's active state. Reason is one of
// quota_exceeded, expired, disabled or reactivated.
type Transition struct {
	UserID   string
	Username string
	Reason   string
}

// QuotaState remembers each user's previous inactivity reason so that only a
// change (a transition) triggers work; a steady state reports nothing. The
// previous state is seeded from the database, so restarting the panel
// reproduces the exact pre-restart view and a restart alone produces no
// transition storm. A user who became inactive while the panel was down is
// still removed: the deployer reconciles every node with a published state once
// at startup, and that pass publishes only the nodes whose user access actually
// differs from what they run.
type QuotaState struct {
	Repository *Repository
	Now        func() time.Time

	mu       sync.Mutex
	previous map[string]string
}

func NewQuotaState(repository *Repository) *QuotaState {
	return &QuotaState{Repository: repository, Now: time.Now, previous: map[string]string{}}
}

func (q *QuotaState) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

// Seed records the current reason of every user. Call it once at startup,
// before the first Evaluate.
func (q *QuotaState) Seed(ctx context.Context) error {
	userIDs, err := q.AllUserIDs(ctx)
	if err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, userID := range userIDs {
		u, err := q.Repository.Get(ctx, userID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		q.previous[userID] = InactivityReason(u, q.now())
	}
	return nil
}

// Forget drops a user from the tracked state. A deleted user has no record left
// to evaluate, so the entry could only be leaked memory; the next Evaluate would
// remove it as well, but a deletion must not wait for a tick to clean up after
// itself.
func (q *QuotaState) Forget(userID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.previous, userID)
}

func (q *QuotaState) AllUserIDs(ctx context.Context) ([]string, error) {
	rows, err := q.Repository.Database.QueryContext(ctx, "SELECT id FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	userIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, id)
	}
	return userIDs, rows.Err()
}

// Evaluate recomputes the requested users and returns only those whose active
// state changed since the previous evaluation (or since Seed). Deleted users
// are forgotten without a transition.
func (q *QuotaState) Evaluate(ctx context.Context, userIDs []string) ([]Transition, error) {
	now := q.now()
	transitions := []Transition{}
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, userID := range userIDs {
		u, err := q.Repository.Get(ctx, userID)
		if errors.Is(err, ErrNotFound) {
			delete(q.previous, userID)
			continue
		}
		if err != nil {
			return transitions, err
		}
		reason := InactivityReason(u, now)
		previous, tracked := q.previous[userID]
		q.previous[userID] = reason
		if !tracked || previous == reason {
			continue
		}
		if reason == "" {
			reason = "reactivated"
		}
		transitions = append(transitions, Transition{UserID: userID, Username: u.Username, Reason: reason})
	}
	return transitions, nil
}
