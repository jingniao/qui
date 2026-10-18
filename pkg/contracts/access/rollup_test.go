package access

import (
	"math"
	"testing"
	"time"
)

func record() Rollup {
	return Rollup{Bucket: time.Date(2026, 10, 2, 1, 2, 38, 0, time.FixedZone("local", 8*3600)), NodeID: "node-a", Revision: 1, UserID: "user-a", DeviceID: "device-a", InboundProtocol: "vless", Domain: "NETFLIX.COM.", TargetPort: 443, Network: "tcp", Action: "Proxy", Allowed: true, Connections: 1}
}

func TestNormalizeMinutePrivacyAndUnknownMeasurements(t *testing.T) {
	r := record()
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
	if r.Domain != "netflix.com" || r.Bucket.Location() != time.UTC || r.Bucket.Second() != 0 {
		t.Fatal("not normalized", r)
	}
	for _, domain := range []string{"https://example.com/private?password=secret", "example.com/path", "Cookie:password", "example.com?token=secret"} {
		r := record()
		r.Domain = domain
		if err := r.Normalize(); err == nil {
			t.Fatal("content-bearing domain accepted")
		}
	}
	r = record()
	r.DownloadBytes = 1
	if err := r.Normalize(); err == nil {
		t.Fatal("invented unknown traffic accepted")
	}
	r = record()
	r.Domain = ""
	r.TargetIP = "2001:db8::1"
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
}

func TestMergeIdentityRuleMinuteAndOverflow(t *testing.T) {
	a := record()
	b := record()
	merged, err := Merge(a, b)
	if err != nil || merged.Connections != 2 {
		t.Fatal(merged, err)
	}
	b.DeviceID = "device-b"
	if _, err := Merge(a, b); err == nil {
		t.Fatal("different device merged")
	}
	b = record()
	b.RuleID = "rule-2"
	if _, err := Merge(a, b); err == nil {
		t.Fatal("different rule merged")
	}
	b = record()
	b.Bucket = b.Bucket.Add(time.Minute)
	if _, err := Merge(a, b); err == nil {
		t.Fatal("different minute merged")
	}
	a.Connections = math.MaxInt64
	b = record()
	if _, err := Merge(a, b); err == nil {
		t.Fatal("counter overflow hidden")
	}
}
