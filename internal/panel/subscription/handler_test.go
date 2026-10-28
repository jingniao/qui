package subscription

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"proxycontrol/internal/panel/auth"
	"proxycontrol/internal/panel/subscription/format"
	"proxycontrol/internal/panel/subscription/format/singbox"
	"proxycontrol/internal/panel/subscription/format/uri"
	"proxycontrol/internal/panel/subscription/proxy"
	"proxycontrol/internal/panel/subscription/source"
	"proxycontrol/internal/panel/user"
)

// fixtureSettings is the Owner's subscription policy as a test would set it.
type fixtureSettings struct {
	policy format.Policy
	err    error
}

func (s fixtureSettings) SubscriptionPolicy(context.Context) (format.Policy, error) {
	return s.policy, s.err
}

// fixtureTraffic is the recorded upload/download split the header reports.
type fixtureTraffic struct {
	upload, download, samples int64
}

func (t fixtureTraffic) AccountUsage(context.Context, string) (int64, int64, int64, error) {
	return t.upload, t.download, t.samples, nil
}

// testExporter is a format that exists only in a test: adding it to the registry
// is the whole change needed for a new format to be served (acceptance A1.2).
type testExporter struct{}

func (testExporter) ID() string          { return "fixture" }
func (testExporter) Aliases() []string   { return []string{"fixture-alias"} }
func (testExporter) ContentType() string { return "text/x-fixture; charset=utf-8" }
func (testExporter) FileName() string    { return "qui-fixture.txt" }
func (testExporter) UserAgents() []string {
	return []string{"FixtureClient/", "!FixtureLegacy"}
}
func (testExporter) UsesRules() bool { return false }

func (testExporter) Render(profile format.Profile) (format.Output, error) {
	names := make([]string, 0, len(profile.Proxies))
	for _, endpoint := range profile.Proxies {
		names = append(names, endpoint.Name)
	}
	return format.Output{Body: []byte("fixture:" + strings.Join(names, "|"))}, nil
}

// testSource is a source that exists only in a test.
type testSource struct {
	id       string
	proxies  []proxy.Proxy
	warnings []proxy.Warning
	err      error
	optional bool
}

func (s testSource) ID() string     { return s.id }
func (s testSource) Optional() bool { return s.optional }
func (s testSource) Proxies(context.Context, source.Viewer) ([]proxy.Proxy, []proxy.Warning, error) {
	return s.proxies, s.warnings, s.err
}

func testEndpoint(name string) proxy.Proxy {
	return proxy.Proxy{Origin: proxy.Origin{Source: "test", Ref: name, Node: proxy.Ref{ID: "test-node", Name: "Test"}}, Name: name, Server: "192.0.2.55", Port: 1080, Protocol: "socks5", Credential: proxy.Credential{Username: "u", Password: "p"}}
}

func discardHandler(service *Service) *Handler {
	return &Handler{Service: service, Auth: &auth.Handler{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
}

func getSubscription(handler *Handler, token, query, userAgent string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", "/sub/"+token+query, nil)
	request.SetPathValue("token", token)
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	recorder := httptest.NewRecorder()
	handler.content(recorder, request)
	return recorder
}

// A1.2: a test-only source and a test-only exporter work end to end through the
// HTTP handler with no change to the service, the handler or the shipped formats.
func TestTestOnlySourceAndExporterWorkEndToEnd(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.publish(t, f.nodeIDs[0])
	_, plaintext, err := f.service.Rotate(ctx, f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.service.Sources.Register(testSource{id: "fixture-source", proxies: []proxy.Proxy{testEndpoint("imported")}})
	f.service.Formats.Register(testExporter{})

	recorder := getSubscription(discardHandler(f.service), plaintext, "?format=fixture-alias", "")
	if recorder.Code != 200 {
		t.Fatalf("fixture format: %d %s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/x-fixture; charset=utf-8" {
		t.Fatalf("content type %q", contentType)
	}
	if disposition := recorder.Header().Get("Content-Disposition"); disposition != "attachment; filename*=UTF-8''qui-fixture.txt" {
		t.Fatalf("content disposition %q", disposition)
	}
	body := recorder.Body.String()
	if !strings.HasPrefix(body, "fixture:") {
		t.Fatalf("fixture exporter not used: %q", body)
	}
	// Both sources contribute: the published nodes and the test source.
	if !strings.Contains(body, "imported") || !strings.Contains(body, "Node A · socks5") {
		t.Fatalf("not every source reached the exporter: %q", body)
	}
	if len(f.service.formats().Info()) != 6 {
		t.Fatalf("registry list: %+v", f.service.formats().Info())
	}
}

// A1.2: a failing optional source contributes nothing plus a warning, and the
// other sources still render.
func TestFailingSourceKeepsTheRestAndWarns(t *testing.T) {
	f := setup(t)
	f.publish(t, f.nodeIDs[0])
	working := testSource{id: "working", proxies: []proxy.Proxy{testEndpoint("kept")}}
	failing := testSource{id: "import", err: os.ErrDeadlineExceeded, optional: true}
	registry := source.NewRegistry(failing, working)
	proxies, warnings, err := registry.Collect(context.Background(), source.Viewer{})
	if err != nil {
		t.Fatalf("an optional source failure must not fail the subscription: %v", err)
	}
	if len(proxies) != 1 || proxies[0].Name != "kept" {
		t.Fatalf("other sources were lost: %+v", proxies)
	}
	if len(warnings) != 1 || warnings[0].Stage != "source" || warnings[0].Ref != "import" {
		t.Fatalf("warnings: %+v", warnings)
	}

	// A primary source stays fatal: an empty but successful subscription would make
	// every client wipe its server list.
	primary := testSource{id: "published", err: os.ErrDeadlineExceeded}
	if _, _, err := source.NewRegistry(primary, working).Collect(context.Background(), source.Viewer{}); err == nil {
		t.Fatal("a non-optional source failure must be reported")
	}
	// The same through the service, so the failure reaches the HTTP layer.
	f.service.Sources = source.NewRegistry(testSource{id: "published", err: os.ErrDeadlineExceeded})
	_, plaintext, err := f.service.Rotate(context.Background(), f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recorder := getSubscription(discardHandler(f.service), plaintext, "", ""); recorder.Code == 200 {
		t.Fatalf("primary source failure answered 200: %s", recorder.Body.String())
	}
}

// A1.2: a name collision keeps the earlier source and reports the dropped proxy.
func TestSourceCollisionKeepsTheFirstSource(t *testing.T) {
	first := testSource{id: "published", proxies: []proxy.Proxy{testEndpoint("Node A · socks5")}}
	second := testSource{id: "import", proxies: []proxy.Proxy{testEndpoint("Node A · socks5")}, optional: true}
	proxies, warnings, err := source.NewRegistry(first, second).Collect(context.Background(), source.Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(proxies) != 1 || proxies[0].Origin.Source != "test" || len(warnings) != 1 || !strings.Contains(warnings[0].Message, "duplicate") {
		t.Fatalf("collision handling: %+v %+v", proxies, warnings)
	}
}

// A1.3: `?format=` wins over the User-Agent, the Owner switch turns detection off,
// an unknown User-Agent falls back to the default and an unknown format is 400.
//
// The shipped User-Agent table is empty (docs/v1.1-phase0-findings.md A.7), so the
// mechanism is exercised with the test exporter's patterns instead of guessed
// client strings.
func TestFormatResolutionOrder(t *testing.T) {
	f := setup(t)
	f.publish(t, f.nodeIDs[0])
	_, plaintext, err := f.service.Rotate(context.Background(), f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.service.Formats = format.NewRegistry(uri.URI{}, uri.Base64{}, singbox.Exporter{}, testExporter{})
	handler := discardHandler(f.service)

	for _, testCase := range []struct {
		name        string
		query       string
		userAgent   string
		policy      format.Policy
		status      int
		contentType string
	}{
		{name: "explicit format", query: "?format=base64", userAgent: "FixtureClient/1.0", policy: format.Policy{Default: "uri", DetectUserAgent: true, UpdateHours: 12}, status: 200, contentType: "text/plain; charset=utf-8"},
		{name: "explicit format beats default", query: "?format=sing-box", policy: format.Policy{Default: "uri"}, status: 200, contentType: "application/json; charset=utf-8"},
		{name: "detected user agent", query: "", userAgent: "FixtureClient/2.1", policy: format.Policy{Default: "uri", DetectUserAgent: true}, status: 200, contentType: "text/x-fixture; charset=utf-8"},
		{name: "detection off", query: "", userAgent: "FixtureClient/2.1", policy: format.Policy{Default: "base64", DetectUserAgent: false}, status: 200, contentType: "text/plain; charset=utf-8"},
		{name: "guard pattern blocks the match", query: "", userAgent: "FixtureClient/2.1 FixtureLegacy", policy: format.Policy{Default: "uri", DetectUserAgent: true}, status: 200, contentType: "text/plain; charset=utf-8"},
		{name: "unknown user agent", query: "", userAgent: "SomeOtherClient/1.0", policy: format.Policy{Default: "sing-box", DetectUserAgent: true}, status: 200, contentType: "application/json; charset=utf-8"},
		{name: "unknown format", query: "?format=nope", status: 400},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			f.service.Settings = fixtureSettings{policy: testCase.policy}
			recorder := getSubscription(handler, plaintext, testCase.query, testCase.userAgent)
			if recorder.Code != testCase.status {
				t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
			}
			if testCase.status != 200 {
				return
			}
			if contentType := recorder.Header().Get("Content-Type"); contentType != testCase.contentType {
				t.Fatalf("content type %q, want %q", contentType, testCase.contentType)
			}
		})
	}
}

// A1.4: `subscription-userinfo` is correct for limited and unlimited users, with
// and without an expiry, and its upload/download add up to the used traffic.
func TestSubscriptionUserInfoHeader(t *testing.T) {
	expiry := "2027-01-02T03:04:05.000000000Z"
	expiresAt, err := time.Parse(timestampLayout, expiry)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name    string
		account user.User
		traffic fixtureTraffic
		want    string
	}{
		{
			name:    "limited with expiry",
			account: user.User{ID: "u", TrafficLimit: 1000, UsedTraffic: 300, ExpiresAt: expiry},
			traffic: fixtureTraffic{upload: 100, download: 200, samples: 2},
			want:    "upload=100; download=200; total=1000; expire=" + strconv.FormatInt(expiresAt.Unix(), 10),
		},
		{
			name:    "unlimited without expiry",
			account: user.User{ID: "u", TrafficLimit: 0, UsedTraffic: 0},
			want:    "upload=0; download=0; total=0; expire=0",
		},
		{
			name:    "an uneven ratio keeps the total",
			account: user.User{ID: "u", TrafficLimit: 5000, UsedTraffic: 301},
			traffic: fixtureTraffic{upload: 1, download: 2, samples: 3},
			want:    "upload=100; download=201; total=5000; expire=0",
		},
		{
			// 200 GB at 1:3: the product of two byte counts overflows int64.
			name:    "real byte counts do not overflow",
			account: user.User{ID: "u", TrafficLimit: 1 << 40, UsedTraffic: 200_000_000_000},
			traffic: fixtureTraffic{upload: 50_000_000_000, download: 150_000_000_000, samples: 30},
			want:    "upload=50000000000; download=150000000000; total=1099511627776; expire=0",
		},
		{
			name:    "no recorded traffic reports no split",
			account: user.User{ID: "u", TrafficLimit: 5000, UsedTraffic: 301},
			traffic: fixtureTraffic{upload: 7, download: 9, samples: 0},
			want:    "upload=0; download=0; total=5000; expire=0",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service := &Service{Traffic: testCase.traffic}
			if header := service.usage(context.Background(), testCase.account).Header(); header != testCase.want {
				t.Fatalf("header %q, want %q", header, testCase.want)
			}
		})
	}
}

// The handler sends the header, the update interval and the download name the
// format declares.
func TestSubscriptionHeadersAreSent(t *testing.T) {
	f := setup(t)
	f.publish(t, f.nodeIDs[0])
	_, plaintext, err := f.service.Rotate(context.Background(), f.u.ID, f.device.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.service.Settings = fixtureSettings{policy: format.Policy{Default: "uri", UpdateHours: 6}}
	recorder := getSubscription(discardHandler(f.service), plaintext, "", "")
	if recorder.Code != 200 {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("profile-update-interval"); got != "6" {
		t.Fatalf("profile-update-interval %q", got)
	}
	if got := recorder.Header().Get("subscription-userinfo"); got != "upload=0; download=0; total=0; expire=0" {
		t.Fatalf("subscription-userinfo %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); got != "attachment; filename*=UTF-8''qui-uri.txt" {
		t.Fatalf("content disposition %q", got)
	}
}

// `GET /api/v1/subscription-formats` lists the registry, so the admin UI stops
// hard-coding the formats.
func TestFormatsEndpointListsTheRegistry(t *testing.T) {
	handler := &Handler{Service: &Service{}}
	recorder := httptest.NewRecorder()
	handler.formats(recorder, httptest.NewRequest("GET", "/api/v1/subscription-formats", nil))
	if recorder.Code != 200 {
		t.Fatalf("%d %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Items []struct {
			ID          string   `json:"id"`
			Aliases     []string `json:"aliases"`
			ContentType string   `json:"contentType"`
			FileName    string   `json:"fileName"`
			UsesRules   bool     `json:"usesRules"`
		} `json:"items"`
		Default string `json:"default"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range body.Items {
		ids = append(ids, item.ID)
		if item.ContentType == "" || item.FileName == "" {
			t.Fatalf("format %s is not described: %+v", item.ID, item)
		}
	}
	if strings.Join(ids, ",") != "uri,base64,sing-box,clash,clash-legacy" {
		t.Fatalf("format list %v", ids)
	}
	if body.Default != "uri" {
		t.Fatalf("default format %q", body.Default)
	}
}
