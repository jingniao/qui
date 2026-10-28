package user

import (
	"context"
	"time"
)

// resetBoundary retains the original day of month, clamping only the current
// month's boundary. A February reset must not move a January 31 anchor.
func resetBoundary(anchor, now time.Time, cycle string) time.Time {
	if now.Before(anchor) {
		return time.Time{}
	}
	if cycle == "30days" {
		days := int(now.Sub(anchor)/(24*time.Hour)) / 30 * 30
		// AddDate follows local calendar days through daylight saving changes.
		for anchor.AddDate(0, 0, days+30).Compare(now) <= 0 {
			days += 30
		}
		for days > 0 && anchor.AddDate(0, 0, days).After(now) {
			days -= 30
		}
		if days == 0 {
			return time.Time{}
		}
		return anchor.AddDate(0, 0, days)
	}
	months := (now.Year()-anchor.Year())*12 + int(now.Month()-anchor.Month())
	for months > 0 {
		first := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), anchor.Location())
		day := min(anchor.Day(), first.AddDate(0, 1, -1).Day())
		boundary := first.AddDate(0, 0, day-1)
		if !boundary.After(now) {
			return boundary
		}
		months--
	}
	return time.Time{}
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
