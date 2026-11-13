package user

import (
	"context"
	"time"
)

// boundaryAt is the k-th reset boundary after anchor (k >= 1). A monthly
// boundary keeps the anchor's day of month and clamps only that month's: a
// February reset must not move a January 31 anchor. A 30-day boundary uses
// AddDate, which follows local calendar days through daylight saving changes.
func boundaryAt(anchor time.Time, cycle string, k int) time.Time {
	if cycle == "30days" {
		return anchor.AddDate(0, 0, 30*k)
	}
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(k), 1, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), anchor.Location())
	day := min(anchor.Day(), first.AddDate(0, 1, -1).Day())
	return first.AddDate(0, 0, day-1)
}

// boundaryIndex returns the index of the last boundary at or before now (0 when
// none has passed yet).
func boundaryIndex(anchor, now time.Time, cycle string) int {
	if now.Before(anchor) {
		return 0
	}
	k := (now.Year()-anchor.Year())*12 + int(now.Month()-anchor.Month())
	if cycle == "30days" {
		k = int(now.Sub(anchor)/(24*time.Hour)) / 30
	}
	for boundaryAt(anchor, cycle, k+1).Compare(now) <= 0 {
		k++
	}
	for k > 0 && boundaryAt(anchor, cycle, k).After(now) {
		k--
	}
	return k
}

// resetBoundary is the last reset boundary at or before now, or the zero time
// when none has passed yet.
func resetBoundary(anchor, now time.Time, cycle string) time.Time {
	k := boundaryIndex(anchor, now, cycle)
	if k == 0 {
		return time.Time{}
	}
	return boundaryAt(anchor, cycle, k)
}

// nextBoundary is the first reset boundary strictly after now.
func nextBoundary(anchor, now time.Time, cycle string) time.Time {
	return boundaryAt(anchor, cycle, boundaryIndex(anchor, now, cycle)+1)
}

// NextResetAt returns the next traffic reset instant (UTC, same format as
// TrafficResetAt) for the user, or "" when the cycle never resets or the
// anchor is unusable. location is the panel time zone used by the scheduler.
func NextResetAt(u User, now time.Time, location *time.Location) string {
	if u.TrafficResetCycle != "monthly" && u.TrafficResetCycle != "30days" {
		return ""
	}
	start, err := time.Parse(time.RFC3339Nano, u.TrafficResetAnchor)
	if err != nil {
		return ""
	}
	if location == nil {
		location = time.UTC
	}
	return timestamp(nextBoundary(start.In(location), now.In(location), u.TrafficResetCycle))
}

// ResetDue commits usage and its durable boundary together. Historical traffic
// tables and cumulative Agent cursors are intentionally untouched.
func (r *Repository) ResetDue(ctx context.Context, now time.Time, location *time.Location) ([]string, error) {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id,traffic_reset_cycle,traffic_reset_anchor,traffic_reset_at FROM users WHERE traffic_reset_cycle<>'none'")
	if err != nil {
		return nil, err
	}
	type due struct{ id, at string }
	pending := []due{}
	for rows.Next() {
		var id, cycle, anchor, last string
		if err = rows.Scan(&id, &cycle, &anchor, &last); err != nil {
			rows.Close()
			return nil, err
		}
		start, parseErr := time.Parse(time.RFC3339Nano, anchor)
		if parseErr != nil {
			rows.Close()
			return nil, parseErr
		}
		boundary := resetBoundary(start.In(location), now.In(location), cycle)
		if !boundary.IsZero() && (last == "" || timestamp(boundary) > last) {
			pending = append(pending, due{id, timestamp(boundary)})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, item := range pending {
		if _, err = tx.ExecContext(ctx, "UPDATE users SET used_traffic=0,traffic_reset_at=? WHERE id=?", item.at, item.id); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE devices SET used_traffic=0 WHERE user_id=?", item.id); err != nil {
			return nil, err
		}
		ids = append(ids, item.id)
	}
	return ids, tx.Commit()
}
