package collector

import (
	"testing"
	"time"
)

func TestSingboxSourceJoinExpiryAndMalformedSource(t *testing.T) {
	now := time.Unix(1000, 0)
	w := NewSingboxWriter(singboxFixture(), nil, nil)
	w.now = func() time.Time { return now }
	w.Write([]byte(nativeLine(7, "INFO", "inbound/http[http-in]", "inbound connection from 198.51.100.8:54321")))
	w.Write([]byte(nativeLine(7, "INFO", "inbound/http[http-in]", "[device-login] inbound connection to example.com:443")))
	for i := 0; i < 4; i++ {
		now = now.Add(time.Minute)
		users, ok := w.SourceUsers(map[string]bool{"198.51.100.8:54321": true})
		if !ok || users["198.51.100.8:54321"] != "credential" {
			t.Fatal("open source join expired")
		}
	}
	now = now.Add(2 * time.Minute)
	users, _ := w.SourceUsers(map[string]bool{})
	if len(users) != 0 || len(w.sources) != 0 {
		t.Fatal("closed source retained")
	}
	w.Write([]byte(nativeLine(8, "INFO", "inbound/http[http-in]", "inbound connection from invalid-secret")))
	if _, ok := w.SourceUsers(nil); ok {
		t.Fatal("malformed source not rejected")
	}
}
