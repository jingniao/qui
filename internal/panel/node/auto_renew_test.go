package node

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestPutMetaPreservesOmittedAutoRenewAndRejectsConflicts(t *testing.T) {
	f := newMachineHTTPFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		body        string
		status      int
		date, cycle string
		enabled     bool
	}{
		{`{"renewDate":"2025-01-31","renewCycle":"/月","autoRenew":true}`, 200, "2025-01-31", "/月", true},
		{`{"renewDate":"2025-02-28","renewCycle":"/季"}`, 200, "2025-02-28", "/季", true},
		{`{"renewDate":"2025-02-28","renewCycle":"一次性"}`, 400, "2025-02-28", "/季", true},
		{`{"renewDate":"","renewCycle":"/季"}`, 400, "2025-02-28", "/季", true},
		{`{"renewDate":"2025-02-28","renewCycle":"一次性","autoRenew":false}`, 200, "2025-02-28", "一次性", false},
	} {
		response := f.do(t, "PUT", "/api/v1/nodes/"+f.nodeID+"/meta", tc.body, f.owner, f.ownerCSRF)
		if response.Code != tc.status {
			t.Fatal(tc.body, response.Code, response.Body.String())
		}
		m, _, err := f.repository.Meta(ctx, f.nodeID)
		if err != nil || m.AutoRenew != tc.enabled || m.RenewDate != tc.date || m.RenewCycle != tc.cycle {
			t.Fatal(m, err)
		}
	}
}

func TestAutoRenewCalendarCycles(t *testing.T) {
	for _, tc := range []struct{ name, cycle, date, today, want string }{
		{"monthly", "/月", "2025-01-15", "2025-01-16", "2025-02-15"},
		{"quarterly", "/季", "2025-01-15", "2025-01-16", "2025-04-15"},
		{"yearly", "/年", "2025-01-15", "2025-01-16", "2026-01-15"},
		{"catch_up", "/月", "2025-01-31", "2025-04-01", "2025-04-30"},
		{"month_end", "/月", "2025-01-31", "2025-02-01", "2025-02-28"},
		{"today", "/月", "2025-01-31", "2025-01-31", "2025-01-31"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, ctx := cancelTestRepository(t)
			n, _, _, err := r.CreateEnrollment(ctx, tc.name, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = r.Database.Exec("INSERT INTO node_meta(node_id,renew_date,renew_cycle,auto_renew) VALUES(?,?,?,1)", n.ID, tc.date, tc.cycle); err != nil {
				t.Fatal(err)
			}
			now, err := time.Parse("2006-01-02", tc.today)
			if err != nil {
				t.Fatal(err)
			}
			var recorded []RenewedNode
			items, err := r.AutoRenew(ctx, now, time.UTC, func(_ context.Context, tx *sql.Tx, item RenewedNode) error {
				var stored string
				if err := tx.QueryRow("SELECT renew_date FROM node_meta WHERE node_id=?", n.ID).Scan(&stored); err != nil {
					return err
				}
				if stored != tc.want {
					t.Fatalf("transaction date = %s, want %s", stored, tc.want)
				}
				recorded = append(recorded, item)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if tc.name == "today" {
				wantCount = 0
			}
			if len(items) != wantCount || len(recorded) != wantCount {
				t.Fatal(items, recorded)
			}
			if wantCount == 1 && (items[0].From != tc.date || items[0].To != tc.want || items[0].NodeID != n.ID) {
				t.Fatal(items)
			}
			m, _, err := r.Meta(ctx, n.ID)
			if err != nil || m.RenewDate != tc.want || !m.AutoRenew {
				t.Fatal(m, err)
			}
		})
	}
}

func TestAutoRenewValidation(t *testing.T) {
	for _, tc := range []struct {
		date, cycle string
		valid       bool
	}{
		{"2025-01-31", "/月", true}, {"2025-01-31", "/季", true},
		{"2025-01-31", "/半年", true}, {"2025-01-31", "/年", true},
		{"2025-01-31", "/2年", true}, {"2025-01-31", "/3年", true},
		{"2025-01-31", "一次性", false}, {"", "/月", false}, {"2025-01-31", "custom", false},
	} {
		m := Meta{AutoRenew: true, RenewDate: tc.date, RenewCycle: tc.cycle}
		err := m.Validate()
		if tc.valid && err != nil || !tc.valid && !errors.Is(err, errAutoRenewIneligible) {
			t.Fatalf("%+v: %v", m, err)
		}
	}
}
