package node

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"proxycontrol/internal/testutil/dbtest"
)

type orderAudit struct{ actions []string }

func (a *orderAudit) RecordTx(_ context.Context, _ *sql.Tx, _, action, _, _, _ string) error {
	a.actions = append(a.actions, action)
	return nil
}

func TestServerOrderSaveValidatesAndKeepsRevision(t *testing.T) {
	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		n, _, _, err := r.CreateEnrollment(ctx, name, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	if _, err := database.ExecContext(ctx, "UPDATE nodes SET deleted_at=? WHERE id=?", "2026-11-13T00:00:00Z", ids[2]); err != nil {
		t.Fatal(err)
	}
	audit := &orderAudit{}

	initial, err := r.ReadServerOrder(ctx)
	if err != nil || initial.Revision != 0 || len(initial.IDs) != 0 {
		t.Fatalf("initial order = %+v, %v", initial, err)
	}
	for name, input := range map[string]ServerOrderInput{
		"unknown":   {IDs: []string{ids[0], "missing"}},
		"duplicate": {IDs: []string{ids[0], ids[0]}},
		"deleted":   {IDs: []string{ids[0], ids[2]}},
		"nil":       {},
	} {
		if _, err := r.SaveServerOrder(ctx, input, "owner", "", audit); !errors.Is(err, ErrServerOrderInvalid) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}

	saved, err := r.SaveServerOrder(ctx, ServerOrderInput{IDs: []string{ids[1], ids[0]}}, "owner", "", audit)
	if err != nil || saved.Revision != 1 || !reflect.DeepEqual(saved.IDs, []string{ids[1], ids[0]}) || saved.UpdatedBy != "owner" {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	again, err := r.SaveServerOrder(ctx, ServerOrderInput{IDs: []string{ids[1], ids[0]}, ExpectedRevision: 0}, "owner", "", audit)
	if err != nil || again.Revision != 1 {
		t.Fatalf("unchanged save = %+v, %v", again, err)
	}
	if _, err := r.SaveServerOrder(ctx, ServerOrderInput{IDs: []string{ids[0], ids[1]}, ExpectedRevision: 0}, "owner", "", audit); !errors.Is(err, ErrServerOrderConflict) {
		t.Fatalf("stale save err = %v", err)
	}
	if got, err := r.ReadServerOrder(ctx); err != nil || !reflect.DeepEqual(got.IDs, []string{ids[1], ids[0]}) {
		t.Fatalf("read = %+v, %v", got, err)
	}
	if !reflect.DeepEqual(audit.actions, []string{"调整服务器顺序"}) {
		t.Fatalf("audit = %v", audit.actions)
	}
}

func TestNodeTagsNormalizeSaveAndList(t *testing.T) {
	got, err := NormalizeTags([]string{" 香港 ", "CN2", "cn2", "家宽"})
	if err != nil || !reflect.DeepEqual(got, []string{"香港", "CN2", "家宽"}) {
		t.Fatalf("normalize = %v, %v", got, err)
	}
	for _, bad := range [][]string{{""}, {"a,b"}, {"tab\there"}, {"一二三四五六七八九十一二三四五六七八九十一"}, {"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}} {
		if _, err := NormalizeTags(bad); !errors.Is(err, ErrNodeTagsInvalid) {
			t.Fatalf("%q accepted", bad)
		}
	}
	if _, err := DecodeTags(`["a","A"]`); err == nil {
		t.Fatal("non-normalized stored tags accepted")
	}

	ctx := context.Background()
	database, err := dbtest.Open(ctx, filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	r := &Repository{Database: database}
	n, _, _, err := r.CreateEnrollment(ctx, "tagged", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	save := func(m Meta, tags *[]string) {
		t.Helper()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := SaveMeta(ctx, tx, n.ID, m, tags, false); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	tags := []string{"香港", "CN2"}
	save(Meta{Region: "HK", RenewNoticeDays: 7}, &tags)
	// An older client omits tags: the stored tags stay.
	save(Meta{Region: "HK2", RenewNoticeDays: 7}, nil)
	m, present, err := r.Meta(ctx, n.ID)
	if err != nil || !present || m.Region != "HK2" || !reflect.DeepEqual(m.Tags, tags) {
		t.Fatalf("meta = %+v, %v, %v", m, present, err)
	}
	listed, _, err := r.List(ctx, "", 1, 20, false)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].Tags, tags) {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	empty := []string{}
	save(Meta{Region: "HK2", RenewNoticeDays: 7}, &empty)
	if m, _, err = r.Meta(ctx, n.ID); err != nil || len(m.Tags) != 0 {
		t.Fatalf("cleared tags = %v, %v", m.Tags, err)
	}
	other, _, _, err := r.CreateEnrollment(ctx, "untagged", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := r.Get(ctx, other.ID); err != nil || got.Tags == nil || len(got.Tags) != 0 {
		t.Fatalf("untagged node tags = %#v, %v", got.Tags, err)
	}
}
