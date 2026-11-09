package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"proxycontrol/pkg/contracts/release"
)

// The core release lookup fills the add/upgrade core dialogs: for one component
// and source it lists the newest stable releases and, per platform, the official
// asset URL with the digest the vendor published for it. It is a convenience for
// the Owner, not a new trust path: the dialog still submits version, URL and
// digest through POST /api/v1/core-updates, the panel validates them exactly as
// before (release.CoreAssetURL, release.CorePinnedTarget) and the Agent refuses
// an archive whose SHA-256 differs. The digest is never computed here; it is read
// from where the Owner would otherwise copy it:
//
//   - Xray:              the <asset>.dgst file the project publishes beside every asset;
//   - official sing-box: the sha256 GitHub shows for the asset on the release page;
//   - qui sing-box:      the SHA256SUMS file of our own release;
//   - GOST:              the reviewed pin in pkg/contracts/release, without a request.
//
// When two published values exist for one asset (Xray and qui also carry the
// GitHub asset digest) they must agree, otherwise the asset gets no digest. An
// asset without a usable digest is still listed with sha256 empty, so the Owner
// sees the URL and pastes a digest by hand, as before this lookup existed.

var ErrCoreReleaseLookup = errors.New("core release lookup failed")
var ErrCoreReleaseSource = errors.New("unknown core release component or source")

const (
	coreReleaseTimeout   = 15 * time.Second
	coreReleaseCacheTTL  = 10 * time.Minute
	coreReleaseListLimit = 8 << 20
	coreReleaseFileLimit = 64 << 10
	coreReleaseKeep      = 5
	coreReleaseUserAgent = "qui-panel-core-releases"
	coreReleaseAPIBase   = "https://api.github.com"
	coreReleaseFileBase  = "https://github.com"
)

// CoreReleaseAsset is one downloadable archive of a release for one platform.
// Libc is "musl" for the official sing-box build that runs on Alpine and empty
// for every asset that does not depend on the C library.
type CoreReleaseAsset struct {
	Platform     string `json:"platform"`
	Libc         string `json:"libc"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	DigestSource string `json:"digestSource"`
}

type CoreRelease struct {
	Version     string             `json:"version"`
	PublishedAt string             `json:"publishedAt"`
	Assets      []CoreReleaseAsset `json:"assets"`
}

type CoreReleaseList struct {
	Component string        `json:"component"`
	Source    string        `json:"source"`
	Items     []CoreRelease `json:"items"`
}

// CoreReleaseService answers the lookup. The zero value is the production
// wiring; tests point APIBase and FileBase at a local server. The asset URLs it
// returns are always the official github.com download URLs, whatever FileBase
// is, because they are validated by release.CoreAssetURL.
type CoreReleaseService struct {
	APIBase  string
	FileBase string
	Client   *http.Client
	Now      func() time.Time

	mu    sync.Mutex
	cache map[string]cachedCoreReleases
}

type cachedCoreReleases struct {
	list CoreReleaseList
	at   time.Time
}

type coreReleaseSource struct {
	component string
	source    string
	repo      string
	// assets maps the release tag to the asset names this source offers, each
	// with its platform and libc.
	assets func(version string) []coreReleaseAssetName
	// version reads the core version from a release; "" skips the release.
	version func(tag string, assetNames []string) string
	digest  string // "dgst", "github" or "sums"
}

type coreReleaseAssetName struct {
	name, platform, libc string
}

var stableTagPattern = regexp.MustCompile(`^v([0-9]+\.[0-9]+\.[0-9]+)$`)
var quiSingboxAssetPattern = regexp.MustCompile(`^sing-box-([0-9]+\.[0-9]+\.[0-9]+)-qui\.([1-9][0-9]*)-linux-(amd64|arm64)\.tar\.gz$`)
var hexDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func stableTagVersion(tag string, _ []string) string {
	if match := stableTagPattern.FindStringSubmatch(tag); match != nil {
		return match[1]
	}
	return ""
}

var coreReleaseSources = map[string]coreReleaseSource{
	"xray/official": {
		component: "xray", source: "official", repo: "XTLS/Xray-core", digest: "dgst",
		version: stableTagVersion,
		assets: func(string) []coreReleaseAssetName {
			return []coreReleaseAssetName{{"Xray-linux-64.zip", "linux/amd64", ""}, {"Xray-linux-arm64-v8a.zip", "linux/arm64", ""}}
		},
	},
	"sing-box/official": {
		component: "sing-box", source: "official", repo: "SagerNet/sing-box", digest: "github",
		version: stableTagVersion,
		assets: func(version string) []coreReleaseAssetName {
			base := "sing-box-" + version + "-linux-"
			return []coreReleaseAssetName{
				{base + "amd64.tar.gz", "linux/amd64", ""}, {base + "arm64.tar.gz", "linux/arm64", ""},
				{base + "amd64-musl.tar.gz", "linux/amd64", "musl"}, {base + "arm64-musl.tar.gz", "linux/arm64", "musl"},
			}
		},
	},
	// Our own release carries the reproducible qui sing-box build. The panel
	// version in the tag is not the core version: that one is in the asset name,
	// and many panel releases ship the same build.
	"sing-box/qui": {
		component: "sing-box", source: "qui", repo: "jingniao/qui", digest: "sums",
		version: func(tag string, names []string) string {
			if stableTagPattern.FindStringSubmatch(tag) == nil {
				return ""
			}
			for _, name := range names {
				if match := quiSingboxAssetPattern.FindStringSubmatch(name); match != nil {
					return match[1]
				}
			}
			return ""
		},
		assets: nil, // read from the release itself, see quiAssets
	},
}

func (s *CoreReleaseService) apiBase() string {
	if s.APIBase != "" {
		return strings.TrimRight(s.APIBase, "/")
	}
	return coreReleaseAPIBase
}

func (s *CoreReleaseService) fileBase() string {
	if s.FileBase != "" {
		return strings.TrimRight(s.FileBase, "/")
	}
	return coreReleaseFileBase
}

func (s *CoreReleaseService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// client follows at most three redirects and only to the hosts a GitHub release
// download may redirect to, so a digest file is never fetched from elsewhere.
func (s *CoreReleaseService) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: coreReleaseTimeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 || request.URL.Scheme != "https" || !release.CoreRedirectHost(request.URL.Hostname()) {
			return errors.New("core release redirect refused")
		}
		return nil
	}}
}

// List answers the releases of one component and source, from the cache while it
// is younger than ten minutes unless refresh is set. A failed lookup is never
// cached.
func (s *CoreReleaseService) List(ctx context.Context, component, source string, refresh bool) (CoreReleaseList, error) {
	if source == "" {
		source = "official"
	}
	if component == "gost" && source == "official" {
		return gostReleaseList(), nil
	}
	key := component + "/" + source
	spec, ok := coreReleaseSources[key]
	if !ok {
		return CoreReleaseList{}, ErrCoreReleaseSource
	}
	if !refresh {
		s.mu.Lock()
		cached, found := s.cache[key]
		s.mu.Unlock()
		if found && s.now().Sub(cached.at) < coreReleaseCacheTTL {
			return cached.list, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, coreReleaseTimeout*2)
	defer cancel()
	list, err := s.fetch(ctx, spec)
	if err != nil {
		return CoreReleaseList{}, ErrCoreReleaseLookup
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]cachedCoreReleases{}
	}
	s.cache[key] = cachedCoreReleases{list: list, at: s.now()}
	s.mu.Unlock()
	return list, nil
}

func gostReleaseList() CoreReleaseList {
	return CoreReleaseList{Component: "gost", Source: "official", Items: []CoreRelease{{
		Version: release.GostVersion,
		Assets: []CoreReleaseAsset{{
			Platform: "linux/amd64", URL: "https://github.com/go-gost/gost/releases/download/" + release.GostTag + "/" + release.GostAsset,
			SHA256: release.GostArchiveSHA256, DigestSource: "pinned",
		}},
	}}}
}

type githubReleaseAsset struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	PublishedAt string               `json:"published_at"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	Assets      []githubReleaseAsset `json:"assets"`
}

func (s *CoreReleaseService) fetch(ctx context.Context, spec coreReleaseSource) (CoreReleaseList, error) {
	data, err := s.get(ctx, s.apiBase()+"/repos/"+spec.repo+"/releases?per_page=20", coreReleaseListLimit, "application/vnd.github+json")
	if err != nil {
		return CoreReleaseList{}, err
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return CoreReleaseList{}, err
	}
	list := CoreReleaseList{Component: spec.component, Source: spec.source, Items: []CoreRelease{}}
	seen := map[string]bool{}
	for _, item := range releases {
		if len(list.Items) >= coreReleaseKeep {
			break
		}
		if item.Draft || item.Prerelease {
			continue
		}
		names := make([]string, 0, len(item.Assets))
		published := map[string]string{}
		for _, asset := range item.Assets {
			names = append(names, asset.Name)
			published[asset.Name] = githubDigest(asset.Digest)
		}
		version := spec.version(item.TagName, names)
		if version == "" || seen[version] {
			continue
		}
		wanted := spec.assets
		if spec.source == "qui" {
			wanted = func(string) []coreReleaseAssetName { return quiAssets(version, names) }
		}
		var sums map[string]string
		if spec.digest == "sums" {
			sums = s.sha256sums(ctx, spec.repo, item.TagName)
		}
		entry := CoreRelease{Version: version, PublishedAt: item.PublishedAt, Assets: []CoreReleaseAsset{}}
		for _, want := range wanted(version) {
			github, present := published[want.name]
			if !present {
				continue
			}
			assetURL := "https://github.com/" + spec.repo + "/releases/download/" + item.TagName + "/" + want.name
			if _, _, err := release.CoreAssetURL(spec.component, assetURL); err != nil {
				continue
			}
			asset := CoreReleaseAsset{Platform: want.platform, Libc: want.libc, URL: assetURL}
			switch spec.digest {
			case "github":
				if github != "" {
					asset.SHA256, asset.DigestSource = github, "github"
				}
			case "dgst":
				if digest := s.dgst(ctx, spec.repo, item.TagName, want.name); digest != "" && (github == "" || github == digest) {
					asset.SHA256, asset.DigestSource = digest, "dgst"
				}
			case "sums":
				if digest := sums[want.name]; digest != "" && (github == "" || github == digest) {
					asset.SHA256, asset.DigestSource = digest, "sha256sums"
				}
			}
			entry.Assets = append(entry.Assets, asset)
		}
		if len(entry.Assets) == 0 {
			continue
		}
		seen[version] = true
		list.Items = append(list.Items, entry)
	}
	return list, nil
}

// quiAssets lists the qui sing-box archives of one release that carry the given
// core version, one per architecture.
func quiAssets(version string, names []string) []coreReleaseAssetName {
	assets := []coreReleaseAssetName{}
	for _, name := range names {
		match := quiSingboxAssetPattern.FindStringSubmatch(name)
		if match == nil || match[1] != version {
			continue
		}
		assets = append(assets, coreReleaseAssetName{name: name, platform: "linux/" + match[3]})
	}
	return assets
}

// githubDigest reads GitHub's "sha256:<hex>" asset digest; anything else is "".
func githubDigest(value string) string {
	digest, ok := strings.CutPrefix(value, "sha256:")
	digest = strings.ToLower(digest)
	if !ok || !hexDigestPattern.MatchString(digest) {
		return ""
	}
	return digest
}

// dgst reads the SHA2-256 line of an Xray <asset>.dgst file. Any failure answers
// "", which leaves the digest to the Owner.
func (s *CoreReleaseService) dgst(ctx context.Context, repo, tag, asset string) string {
	data, err := s.get(ctx, s.fileBase()+"/"+repo+"/releases/download/"+url.PathEscape(tag)+"/"+url.PathEscape(asset)+".dgst", coreReleaseFileLimit, "")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		label := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(name), "-", ""), "_", ""))
		if label != "SHA2256" && label != "SHA256" {
			continue
		}
		digest := strings.ToLower(strings.TrimSpace(value))
		if hexDigestPattern.MatchString(digest) {
			return digest
		}
		return ""
	}
	return ""
}

// sha256sums reads a release's SHA256SUMS ("<hex>  <name>" per line). A missing
// or unreadable file answers an empty map.
func (s *CoreReleaseService) sha256sums(ctx context.Context, repo, tag string) map[string]string {
	sums := map[string]string{}
	data, err := s.get(ctx, s.fileBase()+"/"+repo+"/releases/download/"+url.PathEscape(tag)+"/SHA256SUMS", coreReleaseFileLimit, "")
	if err != nil {
		return sums
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		digest := strings.ToLower(fields[0])
		if hexDigestPattern.MatchString(digest) {
			sums[strings.TrimPrefix(fields[1], "*")] = digest
		}
	}
	return sums
}

func (s *CoreReleaseService) get(ctx context.Context, target string, limit int64, accept string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	request.Header.Set("User-Agent", coreReleaseUserAgent)
	response, err := s.client().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("unexpected status")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response too large")
	}
	return data, nil
}
