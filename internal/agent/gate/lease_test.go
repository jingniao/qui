package gate

import (
	"testing"
	"time"
)

func TestLeaseTTLRefreshExpiryAndInvalidFailOpen(t *testing.T) {
	now := time.Unix(100, 0)
	var l Lease
	data := []byte(`{"ttl_seconds":180,"entries":[{"credential_id":"u","keys":["198.51.100.1"]}]}`)
	if l.Set(data, now) != nil || len(l.Entries(now)) != 1 {
		t.Fatal("lease missing")
	}
	if l.Set(data, now.Add(time.Minute)) != nil || len(l.Entries(now.Add(3*time.Minute))) != 1 {
		t.Fatal("refresh failed")
	}
	if len(l.Entries(now.Add(4*time.Minute))) != 0 {
		t.Fatal("TTL failed open")
	}
	_ = l.Set(data, now)
	if l.Set([]byte(`{"ttl_seconds":181}`), now) == nil || len(l.Entries(now)) != 0 {
		t.Fatal("invalid command retained gates")
	}
	var restarted Lease
	if len(restarted.Entries(now)) != 0 {
		t.Fatal("restart retained gates")
	}
}
