package access

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestReadNodeTrafficMixedPrecisionBoundaries(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	stamps := []string{
		"2026-10-02T09:59:59.999999999Z",
		"2026-10-02T10:00:00Z",
		"2026-10-02T10:00:00.000000001Z",
		"2026-10-02T10:00:00.1Z",
		"2026-10-02T10:00:00.100000001Z",
		"2026-10-02T10:00:01Z",
		"2026-10-02T10:00:01.000000001Z",
		"2026-10-02T10:00:01.5Z",
		"2026-10-02T10:00:02Z",
	}
	for i, stamp := range stamps {
		if _, err := r.Database.ExecContext(ctx, "INSERT INTO core_traffic_samples(node_id,core,epoch,observed_at,upload_bytes,download_bytes) VALUES(?,'xray','precision',?,?,?)", record.NodeID, stamp, i+1, (i+1)*10); err != nil {
			t.Fatal(err)
		}
	}
	parse := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, bounds := range [][2]string{
		{"2026-10-02T10:00:00Z", "2026-10-02T10:00:02Z"},
		{"2026-10-02T10:00:00.1Z", "2026-10-02T10:00:01.000000001Z"},
		{"2026-10-02T10:00:00.000000001Z", "2026-10-02T10:00:00.100000001Z"},
		{"2026-10-02T10:00:00.1Z", "2026-10-02T10:00:00.1Z"},
		{"2026-10-02T10:00:00Z", "2026-10-02T10:00:01.5Z"},
		{"2026-10-02T10:00:00.1Z", "2026-10-02T10:00:02Z"},
	} {
		t.Run(bounds[0]+"/"+bounds[1], func(t *testing.T) {
			from, to := parse(bounds[0]), parse(bounds[1])
			var up, down, samples int64
			for i, stamp := range stamps {
				at := parse(stamp)
				if !at.Before(from) && at.Before(to) {
					up += int64(i + 1)
					down += int64((i + 1) * 10)
					samples++
				}
			}
			for _, filter := range []string{"", record.NodeID} {
				for _, hourly := range []bool{false, true} {
					got, err := ReadNodeTraffic(ctx, r.Database, from, to, filter, hourly)
					if err != nil {
						t.Fatal(err)
					}
					want := []NodeTrafficTotal{}
					if samples > 0 {
						bucket := ""
						if hourly {
							bucket = "2026-10-02T10:00:00Z"
						}
						want = append(want, NodeTrafficTotal{NodeID: record.NodeID, Bucket: bucket, Upload: up, Download: down, Samples: samples})
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("filter=%q hourly=%v: got %+v want %+v", filter, hourly, got, want)
					}
				}
			}
		})
	}
}

func TestReadNodeTrafficHourlyPrecisionAndUnknownNode(t *testing.T) {
	r, _, record := fixture(t)
	ctx := context.Background()
	from := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	if _, err := r.Database.ExecContext(ctx, "INSERT INTO core_traffic_hourly(node_id,core,bucket,upload_bytes,download_bytes,samples) VALUES(?,'xray',?,23,47,3)", record.NodeID, timestamp(from)); err != nil {
		t.Fatal(err)
	}
	got, err := ReadNodeTraffic(ctx, r.Database, from, from.Add(time.Hour), "", true)
	want := []NodeTrafficTotal{{NodeID: record.NodeID, Bucket: timestamp(from), Upload: 23, Download: 47, Samples: 3}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v %v want %+v", got, err, want)
	}
	for _, bounds := range [][2]time.Time{{from.Add(time.Nanosecond), from.Add(time.Hour)}, {from, from.Add(time.Hour - time.Nanosecond)}} {
		if _, err := ReadNodeTraffic(ctx, r.Database, bounds[0], bounds[1], record.NodeID, false); !errors.Is(err, ErrTrafficResolution) {
			t.Fatalf("partial aggregate: %v", err)
		}
	}
	if got, err := ReadNodeTraffic(ctx, r.Database, from.Add(time.Nanosecond), from.Add(time.Hour), "unknown", false); err != nil || len(got) != 0 {
		t.Fatalf("unrelated node: %+v %v", got, err)
	}
}
