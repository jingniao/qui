package deployment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"proxycontrol/pkg/contracts/release"
)

const (
	digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// fakeGitHub serves the release list of each repository under /repos/<repo>/releases
// and release files under /<repo>/releases/download/<tag>/<name>.
type fakeGitHub struct {
	lists map[string]string
	files map[string]string
	hits  atomic.Int64
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.hits.Add(1)
	if strings.HasPrefix(r.URL.Path, "/repos/") && strings.HasSuffix(r.URL.Path, "/releases") {
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/"), "/releases")
		body, ok := f.lists[repo]
		if !ok {
			http.Error(w, "upstream secret text", 500)
			return
		}
		fmt.Fprint(w, body)
		return
	}
	body, ok := f.files[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	fmt.Fprint(w, body)
}

func newReleaseService(t *testing.T, fake *fakeGitHub) *CoreReleaseService {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return &CoreReleaseService{APIBase: server.URL, FileBase: server.URL, Client: server.Client()}
}

func TestCoreReleasesXrayReadsDgstAndRefusesDisagreement(t *testing.T) {
	fake := &fakeGitHub{
		lists: map[string]string{"XTLS/Xray-core": `[
			{"tag_name":"v26.11.1","prerelease":true,"assets":[{"name":"Xray-linux-64.zip"}]},
			{"tag_name":"v26.10.31","published_at":"2026-10-31T00:00:00Z","assets":[
				{"name":"Xray-linux-64.zip","digest":"sha256:` + digestA + `"},
				{"name":"Xray-linux-arm64-v8a.zip","digest":"sha256:` + digestC + `"},
				{"name":"Xray-windows-64.zip"}]},
			{"tag_name":"v26.10.0-beta","assets":[{"name":"Xray-linux-64.zip"}]},
			{"tag_name":"v26.3.27","draft":true,"assets":[{"name":"Xray-linux-64.zip"}]}
		]`},
		files: map[string]string{
			"/XTLS/Xray-core/releases/download/v26.10.31/Xray-linux-64.zip.dgst":        "MD5= 00\nSHA1= 11\nSHA2-256= " + strings.ToUpper(digestA) + "\nSHA2-512= 22\n",
			"/XTLS/Xray-core/releases/download/v26.10.31/Xray-linux-arm64-v8a.zip.dgst": "SHA2-256= " + digestB + "\n",
		},
	}
	list, err := newReleaseService(t, fake).List(context.Background(), "xray", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if list.Component != "xray" || list.Source != "official" || len(list.Items) != 1 || list.Items[0].Version != "26.10.31" {
		t.Fatalf("list = %+v", list)
	}
	assets := list.Items[0].Assets
	if len(assets) != 2 {
		t.Fatalf("assets = %+v", assets)
	}
	amd := assets[0]
	if amd.Platform != "linux/amd64" || amd.SHA256 != digestA || amd.DigestSource != "dgst" || amd.URL != "https://github.com/XTLS/Xray-core/releases/download/v26.10.31/Xray-linux-64.zip" {
		t.Fatalf("amd64 = %+v", amd)
	}
	// The .dgst file and the GitHub digest disagree: no digest, the Owner decides.
	arm := assets[1]
	if arm.Platform != "linux/arm64" || arm.SHA256 != "" || arm.DigestSource != "" {
		t.Fatalf("arm64 = %+v", arm)
	}
	for _, asset := range assets {
		if _, _, err := release.CoreAssetURL("xray", asset.URL); err != nil {
			t.Fatalf("%s fails the URL policy: %v", asset.URL, err)
		}
	}
}

func TestCoreReleasesSingboxOfficialUsesGitHubDigestAndMusl(t *testing.T) {
	fake := &fakeGitHub{lists: map[string]string{"SagerNet/sing-box": `[
		{"tag_name":"v1.15.0-alpha.1","prerelease":true,"assets":[]},
		{"tag_name":"v1.14.4","published_at":"2026-10-01T00:00:00Z","assets":[
			{"name":"sing-box-1.14.4-linux-amd64.tar.gz","digest":"sha256:` + digestA + `"},
			{"name":"sing-box-1.14.4-linux-amd64-musl.tar.gz","digest":"sha256:` + digestB + `"},
			{"name":"sing-box-1.14.4-linux-arm64.tar.gz","digest":"md5:1234"},
			{"name":"sing-box-1.14.4-windows-amd64.zip","digest":"sha256:` + digestC + `"}]}
	]`}}
	list, err := newReleaseService(t, fake).List(context.Background(), "sing-box", "official", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Version != "1.14.4" {
		t.Fatalf("list = %+v", list)
	}
	got := map[string]CoreReleaseAsset{}
	for _, asset := range list.Items[0].Assets {
		got[asset.Platform+"/"+asset.Libc] = asset
	}
	if len(got) != 3 || got["linux/amd64/"].SHA256 != digestA || got["linux/amd64/musl"].SHA256 != digestB || got["linux/amd64/musl"].DigestSource != "github" {
		t.Fatalf("assets = %+v", got)
	}
	if arm := got["linux/arm64/"]; arm.SHA256 != "" || arm.URL == "" {
		t.Fatalf("a non-sha256 digest must leave the field empty: %+v", arm)
	}
}

func TestCoreReleasesQuiReadsSHA256SUMSAndDeduplicatesBuilds(t *testing.T) {
	fake := &fakeGitHub{
		lists: map[string]string{"jingniao/qui": `[
			{"tag_name":"v1.4.6-rc.1","prerelease":true,"assets":[{"name":"sing-box-1.14.5-qui.1-linux-amd64.tar.gz"}]},
			{"tag_name":"v1.4.5","assets":[
				{"name":"proxy-control-1.4.5-linux-amd64.tar.gz"},
				{"name":"sing-box-1.14.4-qui.1-linux-amd64.tar.gz","digest":"sha256:` + digestA + `"},
				{"name":"sing-box-1.14.4-qui.1-linux-arm64.tar.gz","digest":"sha256:` + digestC + `"}]},
			{"tag_name":"v1.4.4","assets":[{"name":"sing-box-1.14.4-qui.1-linux-amd64.tar.gz"}]}
		]`},
		files: map[string]string{
			"/jingniao/qui/releases/download/v1.4.5/SHA256SUMS": digestC + "  proxy-control-1.4.5-linux-amd64.tar.gz\n" + digestA + "  sing-box-1.14.4-qui.1-linux-amd64.tar.gz\n" + digestB + "  sing-box-1.14.4-qui.1-linux-arm64.tar.gz\n",
		},
	}
	list, err := newReleaseService(t, fake).List(context.Background(), "sing-box", "qui", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Version != "1.14.4" || len(list.Items[0].Assets) != 2 {
		t.Fatalf("list = %+v", list)
	}
	amd, arm := list.Items[0].Assets[0], list.Items[0].Assets[1]
	if amd.SHA256 != digestA || amd.DigestSource != "sha256sums" || amd.URL != "https://github.com/jingniao/qui/releases/download/v1.4.5/sing-box-1.14.4-qui.1-linux-amd64.tar.gz" {
		t.Fatalf("amd64 = %+v", amd)
	}
	if arm.SHA256 != "" {
		t.Fatalf("SHA256SUMS and the GitHub digest disagree, so no digest: %+v", arm)
	}
}

func TestCoreReleasesGostIsThePinWithoutARequest(t *testing.T) {
	fake := &fakeGitHub{}
	list, err := newReleaseService(t, fake).List(context.Background(), "gost", "official", false)
	if err != nil {
		t.Fatal(err)
	}
	if fake.hits.Load() != 0 || len(list.Items) != 1 || list.Items[0].Assets[0].SHA256 != release.GostArchiveSHA256 || !release.CorePinnedTarget("gost", list.Items[0].Version, list.Items[0].Assets[0].SHA256) {
		t.Fatalf("list = %+v, hits %d", list, fake.hits.Load())
	}
}

func TestCoreReleasesFailuresAndCache(t *testing.T) {
	fake := &fakeGitHub{lists: map[string]string{"SagerNet/sing-box": `[{"tag_name":"v1.14.4","assets":[{"name":"sing-box-1.14.4-linux-amd64.tar.gz","digest":"sha256:` + digestA + `"}]}]`}}
	service := newReleaseService(t, fake)
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	if _, err := service.List(context.Background(), "xray", "official", false); !errors.Is(err, ErrCoreReleaseLookup) {
		t.Fatalf("an upstream error must be the fixed lookup error, got %v", err)
	}
	for _, input := range [][2]string{{"mihomo", "official"}, {"xray", "qui"}, {"gost", "qui"}, {"", ""}} {
		if _, err := service.List(context.Background(), input[0], input[1], false); !errors.Is(err, ErrCoreReleaseSource) {
			t.Fatalf("%v: %v", input, err)
		}
	}
	before := fake.hits.Load()
	for range 2 {
		if _, err := service.List(context.Background(), "sing-box", "official", false); err != nil {
			t.Fatal(err)
		}
	}
	if fake.hits.Load() != before+1 {
		t.Fatalf("the second read within ten minutes must come from the cache")
	}
	if _, err := service.List(context.Background(), "sing-box", "official", true); err != nil || fake.hits.Load() != before+2 {
		t.Fatalf("refresh must ask again: %v", err)
	}
	now = now.Add(11 * time.Minute)
	if _, err := service.List(context.Background(), "sing-box", "official", false); err != nil || fake.hits.Load() != before+3 {
		t.Fatalf("an expired cache must ask again: %v", err)
	}
}

func TestCoreReleasesHTTPErrorsAreFixed(t *testing.T) {
	fake := &fakeGitHub{lists: map[string]string{}}
	handler := &CoreUpdateHandler{Releases: newReleaseService(t, fake)}
	for _, tt := range []struct {
		query, code string
		status      int
	}{{"component=xray&source=official", "core_release_lookup_failed", 502}, {"component=mihomo", "invalid_request", 400}} {
		w := httptest.NewRecorder()
		handler.coreReleases(w, httptest.NewRequest("GET", "/api/v1/core-releases?"+tt.query, nil))
		if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.code) || strings.Contains(w.Body.String(), "upstream secret text") {
			t.Fatalf("%s: %d %s", tt.query, w.Code, w.Body.String())
		}
	}
}
