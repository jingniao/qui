package user

import (
	"testing"
	"time"
)

func TestNextResetAtFixedClock(t *testing.T) {
	utc := time.UTC
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct{ cycle, anchor, now, want string }{
		{"monthly", "2024-01-31T00:00:00Z", "2024-02-15T00:00:00Z", "2024-02-29T00:00:00.000000000Z"},
		{"monthly", "2024-01-31T00:00:00Z", "2024-02-29T00:00:00Z", "2024-03-31T00:00:00.000000000Z"},
		{"monthly", "2024-01-31T00:00:00Z", "2023-12-01T00:00:00Z", "2024-02-29T00:00:00.000000000Z"},
		{"30days", "2024-01-01T00:00:00Z", "2024-01-20T00:00:00Z", "2024-01-31T00:00:00.000000000Z"},
		{"30days", "2024-01-01T00:00:00Z", "2024-01-31T00:00:00Z", "2024-03-01T00:00:00.000000000Z"},
	} {
		got := NextResetAt(User{TrafficResetCycle: c.cycle, TrafficResetAnchor: c.anchor}, at(c.now), utc)
		if got != c.want {
			t.Fatalf("%s %s now %s: got %s want %s", c.cycle, c.anchor, c.now, got, c.want)
		}
	}
	for _, cycle := range []string{"none", ""} {
		if got := NextResetAt(User{TrafficResetCycle: cycle, TrafficResetAnchor: "2024-01-01T00:00:00Z"}, at("2024-01-20T00:00:00Z"), utc); got != "" {
			t.Fatalf("cycle %q: %q", cycle, got)
		}
	}
}

// The scheduler resets exactly at the boundary NextResetAt promises: just
// before it nothing is due, at it resetBoundary returns it.
func TestNextResetAtMatchesSchedulerBoundary(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		zone = time.UTC
	}
	anchor := time.Date(2024, 1, 31, 10, 30, 0, 0, time.UTC)
	for _, cycle := range []string{"monthly", "30days"} {
		for _, location := range []*time.Location{time.UTC, zone} {
			for d := 0; d < 500; d += 7 {
				now := anchor.AddDate(0, 0, d).Add(13 * time.Hour)
				text := NextResetAt(User{TrafficResetCycle: cycle, TrafficResetAnchor: anchor.Format(time.RFC3339Nano)}, now, location)
				next, err := time.Parse(time.RFC3339Nano, text)
				if err != nil {
					t.Fatal(err)
				}
				if !next.After(now) {
					t.Fatalf("%s d=%d: %s not after now", cycle, d, text)
				}
				if got := resetBoundary(anchor.In(location), next.In(location), cycle); !got.Equal(next) {
					t.Fatalf("%s d=%d: scheduler boundary at %s is %s", cycle, d, text, got)
				}
				if got := resetBoundary(anchor.In(location), next.Add(-time.Nanosecond).In(location), cycle); got.Equal(next) {
					t.Fatalf("%s d=%d: boundary %s already due before next", cycle, d, text)
				}
			}
		}
	}
}
