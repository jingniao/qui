package backup

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestK2bArchiveExcludesMetricsPreservesTuning(t *testing.T) {
	source, directory, _ := sourceFixture(t)
	ctx := context.Background()
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for i, q := range []string{
		"INSERT INTO node_metric_samples(node_id,at,cpu_percent) SELECT id,?,0 FROM nodes",
		`INSERT INTO node_tuning(node_id,desired_json,request_id,status,requested_at,updated_at) SELECT id,'{"net.core.somaxconn":"128"}','request','pending',?,? FROM nodes`,
	} {
		args := []any{stamp}
		if i == 1 {
			args = append(args, stamp)
		}
		if _, err := source.Database.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	result, err := source.Create(ctx, directory, "metrics_tuning_archive", "owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Examine(ctx, directory, filepath.Join(directory, result.Manifest.ID+".pcb"), testPassword, func(_ Result, path string) error {
		database, err := readonlyDatabase(filepath.Join(path, "database", "panel.db"))
		if err != nil {
			return err
		}
		defer database.Close()
		for _, tc := range []struct {
			query string
			want  int
		}{
			{"SELECT count(*) FROM node_metric_samples", 0},
			{"SELECT count(*) FROM node_tuning WHERE request_id='request' AND status='pending'", 1},
		} {
			var count int
			if err = database.QueryRow(tc.query).Scan(&count); err != nil || count != tc.want {
				t.Fatal(tc.query, count, err)
			}
		}
		return validateMetricsTuning(ctx, database)
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = source.Database.QueryRow("SELECT count(*) FROM node_metric_samples").Scan(&count); err != nil || count != 1 {
		t.Fatal("live samples stripped", count, err)
	}
}

func TestK2bArchiveRejectsInvalidTuningReceipt(t *testing.T) {
	source, _, _ := sourceFixture(t)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := source.Database.Exec(`INSERT INTO node_tuning SELECT id,'{}','request','failed','{"requestId":"request","status":"failed","keys":[{"key":"unknown","applied":true}]}',?,? FROM nodes`, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if validateMetricsTuning(context.Background(), source.Database) == nil {
		t.Fatal("invalid receipt accepted")
	}
}
