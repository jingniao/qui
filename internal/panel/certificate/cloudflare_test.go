package certificate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeCloudflare is the smallest Cloudflare the client can talk to: token verify,
// the zone lookup, and DNS record create/update/delete. It records what the client
// wrote so the tests can assert on the request bodies rather than on the client's
// own bookkeeping.
type fakeCloudflare struct {
	mu      sync.Mutex
	url     string
	token   string
	zones   map[string]fakeZone
	records []fakeRecord
	bodies  []map[string]any
	nextID  int
	// reject, when set, answers every request with it, which is how the failure
	// paths are exercised.
	reject *fakeRejection
}

type fakeZone struct {
	ID          string
	NameServers []string
}

type fakeRecord struct {
	ZoneID, ID, Type, Name, Content string
	Proxied                         bool
	TTL                             int
}

type fakeRejection struct {
	Status int
	Body   string
}

func newFakeCloudflare(t *testing.T, token string, zones map[string]fakeZone) *fakeCloudflare {
	t.Helper()
	fake := &fakeCloudflare{token: token, zones: zones}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user/tokens/verify", func(w http.ResponseWriter, r *http.Request) {
		if fake.writeRejection(w) || !fake.authorized(w, r) {
			return
		}
		writeCloudflareResult(w, map[string]any{"status": "active"})
	})
	mux.HandleFunc("GET /zones", func(w http.ResponseWriter, r *http.Request) {
		if fake.writeRejection(w) || !fake.authorized(w, r) {
			return
		}
		name := r.URL.Query().Get("name")
		zone, ok := fake.zones[name]
		if !ok {
			writeCloudflareResult(w, []map[string]any{})
			return
		}
		writeCloudflareResult(w, []map[string]any{{"id": zone.ID, "name": name, "name_servers": zone.NameServers}})
	})
	mux.HandleFunc("GET /zones/{zone}/dns_records", func(w http.ResponseWriter, r *http.Request) {
		if fake.writeRejection(w) || !fake.authorized(w, r) {
			return
		}
		recordType, name := r.URL.Query().Get("type"), r.URL.Query().Get("name")
		result := []map[string]any{}
		for _, record := range fake.list() {
			if record.ZoneID == r.PathValue("zone") && record.Type == recordType && record.Name == name {
				result = append(result, map[string]any{"id": record.ID, "content": record.Content})
			}
		}
		writeCloudflareResult(w, result)
	})
	mux.HandleFunc("POST /zones/{zone}/dns_records", func(w http.ResponseWriter, r *http.Request) {
		if fake.writeRejection(w) || !fake.authorized(w, r) {
			return
		}
		record, ok := fake.body(t, r)
		if !ok {
			return
		}
		created := fake.create(r.PathValue("zone"), record)
		writeCloudflareResult(w, map[string]any{"id": created.ID})
	})
	mux.HandleFunc("PUT /zones/{zone}/dns_records/{record}", func(w http.ResponseWriter, r *http.Request) {
		if fake.writeRejection(w) || !fake.authorized(w, r) {
			return
		}
		record, ok := fake.body(t, r)
		if !ok {
			return
		}
		if !fake.update(r.PathValue("record"), record) {
			http.Error(w, "missing record", http.StatusNotFound)
			return
		}
		writeCloudflareResult(w, map[string]any{"id": r.PathValue("record")})
	})
	mux.HandleFunc("DELETE /zones/{zone}/dns_records/{record}", func(w http.ResponseWriter, r *http.Request) {
		if fake.writeRejection(w) || !fake.authorized(w, r) {
			return
		}
		fake.remove(r.PathValue("record"))
		writeCloudflareResult(w, map[string]any{"id": r.PathValue("record")})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	fake.url = server.URL
	return fake
}

func (f *fakeCloudflare) client() *Cloudflare { return NewCloudflare(f.url) }

func (f *fakeCloudflare) writeRejection(w http.ResponseWriter) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reject == nil {
		return false
	}
	w.WriteHeader(f.reject.Status)
	_, _ = w.Write([]byte(f.reject.Body))
	return true
}

func (f *fakeCloudflare) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") == "Bearer "+f.token {
		return true
	}
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1000,"message":"Invalid token"}]}`))
	return false
}

func (f *fakeCloudflare) body(t *testing.T, r *http.Request) (map[string]any, bool) {
	t.Helper()
	var decoded map[string]any
	if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
		t.Errorf("request body: %v", err)
		return nil, false
	}
	f.mu.Lock()
	f.bodies = append(f.bodies, decoded)
	f.mu.Unlock()
	return decoded, true
}

func (f *fakeCloudflare) list() []fakeRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeRecord(nil), f.records...)
}

func (f *fakeCloudflare) create(zoneID string, body map[string]any) fakeRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	record := fakeRecord{ZoneID: zoneID, ID: "record-" + strconv.Itoa(f.nextID), Type: text(body["type"]), Name: text(body["name"]), Content: text(body["content"]), Proxied: body["proxied"] == true, TTL: intValue(body["ttl"])}
	f.records = append(f.records, record)
	return record
}

func (f *fakeCloudflare) update(recordID string, body map[string]any) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.records {
		if f.records[index].ID != recordID {
			continue
		}
		f.records[index].Content = text(body["content"])
		f.records[index].Proxied = body["proxied"] == true
		f.records[index].TTL = intValue(body["ttl"])
		return true
	}
	return false
}

func (f *fakeCloudflare) remove(recordID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.records {
		if f.records[index].ID == recordID {
			f.records = append(f.records[:index], f.records[index+1:]...)
			return
		}
	}
}

func text(value any) string {
	decoded, _ := value.(string)
	return decoded
}

func intValue(value any) int {
	number, _ := value.(float64)
	return int(number)
}

func writeCloudflareResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
}

func newTestCloudflare(t *testing.T) (*fakeCloudflare, *Cloudflare) {
	t.Helper()
	fake := newFakeCloudflare(t, "live-token", map[string]fakeZone{
		"example.com":     {ID: "zone-example", NameServers: []string{"ns1.example.com", "ns2.example.com"}},
		"sub.example.net": {ID: "zone-sub", NameServers: []string{"ns1.example.net"}},
	})
	return fake, fake.client()
}

func TestCloudflareVerifiesToken(t *testing.T) {
	_, client := newTestCloudflare(t)
	ctx := context.Background()
	if err := client.VerifyToken(ctx, "live-token"); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if code := failureCode(client.VerifyToken(ctx, "revoked-token")); code != CodeCloudflareTokenInvalid {
		t.Fatalf("invalid token code = %q", code)
	}
}

func TestCloudflareWalksZoneLabels(t *testing.T) {
	fake, client := newTestCloudflare(t)
	zone, err := client.Zone(context.Background(), "live-token", "a.sub.example.net")
	if err != nil || zone.ID != "zone-sub" || len(zone.NameServers) != 1 {
		t.Fatalf("zone: %+v %v", zone, err)
	}
	// The walk asks for each suffix from the left until one is hosted.
	if _, err := client.Zone(context.Background(), "live-token", "www.example.com"); err != nil {
		t.Fatal(err)
	}
	if code := failureCode(secondError(client.Zone(context.Background(), "live-token", "nowhere.test"))); code != CodeCloudflareZoneNotFound {
		t.Fatalf("missing zone code = %q", code)
	}
	if len(fake.list()) != 0 {
		t.Fatal("a zone lookup wrote a record")
	}
}

func TestCloudflareTXTRecordLifecycle(t *testing.T) {
	fake, client := newTestCloudflare(t)
	ctx := context.Background()
	id, err := client.CreateTXT(ctx, "live-token", "zone-example", "_acme-challenge.example.com", "challenge-value")
	if err != nil {
		t.Fatal(err)
	}
	records := fake.list()
	if len(records) != 1 || records[0].Type != "TXT" || records[0].Name != "_acme-challenge.example.com" || records[0].Content != "challenge-value" || records[0].TTL != 60 {
		t.Fatalf("created record: %+v", records)
	}
	if err := client.DeleteRecord(ctx, "live-token", "zone-example", id); err != nil {
		t.Fatal(err)
	}
	if records := fake.list(); len(records) != 0 {
		t.Fatalf("record survived deletion: %+v", records)
	}
	// Removing a record that is already gone is what a retried cleanup does.
	if err := client.DeleteRecord(ctx, "live-token", "zone-example", id); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

func TestCloudflareUpsertAddressCreatesThenUpdates(t *testing.T) {
	fake, client := newTestCloudflare(t)
	ctx := context.Background()
	if err := client.UpsertAddress(ctx, "live-token", "zone-example", "edge.example.com", "203.0.113.10"); err != nil {
		t.Fatal(err)
	}
	records := fake.list()
	if len(records) != 1 || records[0].Type != "A" || records[0].Content != "203.0.113.10" || records[0].Proxied || records[0].TTL != 1 {
		t.Fatalf("created record: %+v", records)
	}
	// The same address writes nothing; a new one updates the record in place.
	if err := client.UpsertAddress(ctx, "live-token", "zone-example", "edge.example.com", "203.0.113.10"); err != nil {
		t.Fatal(err)
	}
	if bodies := len(fake.list()); bodies != 1 {
		t.Fatalf("unchanged upsert created %d records", bodies)
	}
	if err := client.UpsertAddress(ctx, "live-token", "zone-example", "edge.example.com", "203.0.113.11"); err != nil {
		t.Fatal(err)
	}
	records = fake.list()
	if len(records) != 1 || records[0].Content != "203.0.113.11" {
		t.Fatalf("updated record: %+v", records)
	}
	if err := client.UpsertAddress(ctx, "live-token", "zone-example", "edge.example.com", "2001:db8::10"); err != nil {
		t.Fatal(err)
	}
	records = fake.list()
	if len(records) != 2 || records[1].Type != "AAAA" || records[1].Content != "2001:db8::10" {
		t.Fatalf("AAAA record: %+v", records)
	}
}

func TestCloudflareFailuresUseFixedCodesAndLeakNothing(t *testing.T) {
	fake, client := newTestCloudflare(t)
	ctx := context.Background()
	fake.mu.Lock()
	fake.reject = &fakeRejection{Status: http.StatusInternalServerError, Body: `{"success":false,"errors":[{"message":"secret-upstream-detail"}]}`}
	fake.mu.Unlock()
	err := secondError(client.CreateTXT(ctx, "live-token", "zone-example", "_acme-challenge.example.com", "value"))
	if code := failureCode(err); code != CodeCloudflareDNSRecordFailed {
		t.Fatalf("failure code = %q", code)
	}
	if strings.Contains(err.Error(), "secret-upstream-detail") {
		t.Fatalf("upstream text leaked: %v", err)
	}
	fake.mu.Lock()
	fake.reject = &fakeRejection{Status: http.StatusForbidden, Body: `{"success":false}`}
	fake.mu.Unlock()
	if code := failureCode(secondError(client.Zone(ctx, "live-token", "example.com"))); code != CodeCloudflareTokenInvalid {
		t.Fatalf("forbidden zone code = %q", code)
	}
}

// secondError drops the successful value of an operation whose error carries the
// code under test.
func secondError[T any](_ T, err error) error { return err }
