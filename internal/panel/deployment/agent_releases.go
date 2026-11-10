package deployment

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"proxycontrol/pkg/contracts/release"
)

var (
	ErrAgentReleaseLookup = errors.New("agent release lookup failed")
	ErrAgentReleaseImport = errors.New("agent release import failed")
	versionFormalPattern  = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

const (
	agentReleaseCacheTTL      = 10 * time.Minute
	agentReleaseTimeout       = 30 * time.Second
	agentReleaseListLimit     = 8 << 20
	agentReleaseFileLimit     = 64 << 20
	agentReleaseManifestLimit = 64 << 10
	agentReleaseKeep          = 5
	agentReleaseDefaultAPI    = "https://api.github.com/repos/jingniao/qui"
	agentReleaseUserAgent     = "qui-panel-agent-releases"
)

type AgentReleaseStore interface {
	SaveRelease(ctx context.Context, signed release.Signed, key ed25519.PublicKey, author, ip string) (release.CatalogueEntry, error)
}

type AgentReleaseVersion struct {
	Version       string   `json:"version"`
	PublishedAt   string   `json:"publishedAt"`
	Architectures []string `json:"architectures"`
	Imported      bool     `json:"imported"`
}

type cachedAgentReleases struct {
	items []AgentReleaseVersion
	at    time.Time
}

type AgentReleases struct {
	Directory string
	Store     AgentReleaseStore
	Keys      []release.KeyPin
	Client    *http.Client
	API       string
	Now       func() time.Time

	mu    sync.Mutex
	cache cachedAgentReleases
}

func (a *AgentReleases) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}

func (a *AgentReleases) api() string {
	if a.API != "" {
		return strings.TrimRight(a.API, "/")
	}
	return agentReleaseDefaultAPI
}

func (a *AgentReleases) httpClient() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{
		Timeout: agentReleaseTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if !release.CoreRedirectHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to host %q is not permitted", req.URL.Hostname())
			}
			return nil
		},
	}
}

type ghReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type ghRelease struct {
	TagName     string           `json:"tag_name"`
	PublishedAt string           `json:"published_at"`
	Draft       bool             `json:"draft"`
	Prerelease  bool             `json:"prerelease"`
	Assets      []ghReleaseAsset `json:"assets"`
}

func parseFormalVersion(version string) (seq int64, ok bool) {
	matches := versionFormalPattern.FindStringSubmatch(version)
	if len(matches) != 4 {
		return 0, false
	}
	major, _ := strconv.ParseInt(matches[1], 10, 64)
	minor, _ := strconv.ParseInt(matches[2], 10, 64)
	patch, _ := strconv.ParseInt(matches[3], 10, 64)
	return major*1000000 + minor*1000 + patch, true
}

func (a *AgentReleases) isVersionImported(version string, seq int64, archs []string) bool {
	if a.Directory == "" || len(archs) == 0 {
		return false
	}
	for _, arch := range archs {
		manifestPath := filepath.Join(a.Directory, "proxy-agent", "linux", arch, fmt.Sprintf("%d", seq), "manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return false
		}
		var signed release.Signed
		if err := json.Unmarshal(data, &signed); err != nil {
			return false
		}
		artifactPath := filepath.Join(a.Directory, "artifacts", signed.Manifest.SHA256+".bin")
		if info, err := os.Stat(artifactPath); err != nil || info.Size() != signed.Manifest.Bytes {
			return false
		}
	}
	return true
}

func (a *AgentReleases) fetchReleases(ctx context.Context) ([]ghRelease, error) {
	reqURL := a.api()
	if !strings.HasSuffix(reqURL, "/releases") {
		reqURL += "/releases"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", agentReleaseUserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, agentReleaseListLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > agentReleaseListLimit {
		return nil, errors.New("release list too large")
	}

	var releases []ghRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (a *AgentReleases) List(ctx context.Context, refresh bool) ([]AgentReleaseVersion, error) {
	if !refresh {
		a.mu.Lock()
		if len(a.cache.items) > 0 && a.now().Sub(a.cache.at) < agentReleaseCacheTTL {
			items := append([]AgentReleaseVersion(nil), a.cache.items...)
			a.mu.Unlock()
			return items, nil
		}
		a.mu.Unlock()
	}

	releases, err := a.fetchReleases(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentReleaseLookup, err)
	}

	var result []AgentReleaseVersion
	for _, rel := range releases {
		if rel.Draft || rel.Prerelease {
			continue
		}
		ver := strings.TrimPrefix(rel.TagName, "v")
		seq, ok := parseFormalVersion(ver)
		if !ok {
			continue
		}

		assetsByName := make(map[string]ghReleaseAsset, len(rel.Assets))
		for _, asset := range rel.Assets {
			assetsByName[asset.Name] = asset
		}

		var supportedArchs []string
		for _, arch := range []string{"amd64", "arm64"} {
			manifestName := fmt.Sprintf("proxy-agent-%s-linux-%s.manifest.json", ver, arch)
			binName := fmt.Sprintf("proxy-agent-%s-linux-%s.bin", ver, arch)
			if _, hasManifest := assetsByName[manifestName]; hasManifest {
				if _, hasBin := assetsByName[binName]; hasBin {
					supportedArchs = append(supportedArchs, arch)
				}
			}
		}

		if len(supportedArchs) == 0 {
			continue
		}

		imported := a.isVersionImported(ver, seq, supportedArchs)
		result = append(result, AgentReleaseVersion{
			Version:       ver,
			PublishedAt:   rel.PublishedAt,
			Architectures: supportedArchs,
			Imported:      imported,
		})
		if len(result) >= agentReleaseKeep {
			break
		}
	}

	a.mu.Lock()
	a.cache = cachedAgentReleases{items: result, at: a.now()}
	a.mu.Unlock()

	return result, nil
}

func (a *AgentReleases) downloadAsset(ctx context.Context, u string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, err
	}
	if !parsed.IsAbs() {
		baseParsed, err := url.Parse(a.api())
		if err != nil {
			return nil, err
		}
		u = baseParsed.ResolveReference(parsed).String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", agentReleaseUserAgent)

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("download exceeded size limit")
	}
	return data, nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(dir, "tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(perm); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmpName, path)
}

func (a *AgentReleases) Import(ctx context.Context, version, actor, ip string) ([]release.CatalogueEntry, error) {
	seq, ok := parseFormalVersion(version)
	if !ok {
		return nil, fmt.Errorf("%w: invalid version format", ErrAgentReleaseImport)
	}

	releases, err := a.fetchReleases(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentReleaseImport, err)
	}

	var targetRel *ghRelease
	for i := range releases {
		rel := &releases[i]
		if strings.TrimPrefix(rel.TagName, "v") == version && !rel.Draft && !rel.Prerelease {
			targetRel = rel
			break
		}
	}
	if targetRel == nil {
		return nil, fmt.Errorf("%w: release %s not found", ErrAgentReleaseImport, version)
	}

	assetsByName := make(map[string]ghReleaseAsset, len(targetRel.Assets))
	for _, asset := range targetRel.Assets {
		assetsByName[asset.Name] = asset
	}

	var archsToImport []string
	for _, arch := range []string{"amd64", "arm64"} {
		manifestName := fmt.Sprintf("proxy-agent-%s-linux-%s.manifest.json", version, arch)
		binName := fmt.Sprintf("proxy-agent-%s-linux-%s.bin", version, arch)
		if _, hasManifest := assetsByName[manifestName]; hasManifest {
			if _, hasBin := assetsByName[binName]; hasBin {
				archsToImport = append(archsToImport, arch)
			}
		}
	}
	if len(archsToImport) == 0 {
		return nil, fmt.Errorf("%w: no supported architecture assets for release %s", ErrAgentReleaseImport, version)
	}

	pins := release.EffectiveKeyPins(a.Keys)
	trustedKeys, err := release.PinnedKeys(pins)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid trusted keys: %v", ErrAgentReleaseImport, err)
	}

	var entries []release.CatalogueEntry
	for _, arch := range archsToImport {
		manifestAsset := assetsByName[fmt.Sprintf("proxy-agent-%s-linux-%s.manifest.json", version, arch)]
		binAsset := assetsByName[fmt.Sprintf("proxy-agent-%s-linux-%s.bin", version, arch)]

		manifestURL := manifestAsset.BrowserDownloadURL
		if manifestURL == "" {
			manifestURL = manifestAsset.Name
		}
		manifestBytes, err := a.downloadAsset(ctx, manifestURL, agentReleaseManifestLimit)
		if err != nil {
			return nil, fmt.Errorf("%w: download manifest failed: %v", ErrAgentReleaseImport, err)
		}

		signed, err := release.Decode(manifestBytes)
		if err != nil {
			return nil, fmt.Errorf("%w: decode manifest failed: %v", ErrAgentReleaseImport, err)
		}

		policy := release.Policy{
			Component:       "proxy-agent",
			OS:              "linux",
			Architecture:    arch,
			MinimumSequence: 0,
			Now:             a.now(),
			Keys:            trustedKeys,
		}
		if err := signed.Verify(policy); err != nil {
			return nil, fmt.Errorf("%w: signature verification failed: %v", ErrAgentReleaseImport, err)
		}
		if signed.Manifest.Version != version || signed.Manifest.Sequence != seq {
			return nil, fmt.Errorf("%w: manifest version or sequence does not match selected release", ErrAgentReleaseImport)
		}

		binURL := binAsset.BrowserDownloadURL
		if binURL == "" {
			binURL = binAsset.Name
		}
		binBytes, err := a.downloadAsset(ctx, binURL, agentReleaseFileLimit)
		if err != nil {
			return nil, fmt.Errorf("%w: download binary failed: %v", ErrAgentReleaseImport, err)
		}

		hash := sha256.Sum256(binBytes)
		digest := hex.EncodeToString(hash[:])
		if digest != signed.Manifest.SHA256 || int64(len(binBytes)) != signed.Manifest.Bytes {
			return nil, fmt.Errorf("%w: digest mismatch", ErrAgentReleaseImport)
		}

		if a.Directory != "" {
			artifactPath := filepath.Join(a.Directory, "artifacts", digest+".bin")
			if err := atomicWriteFile(artifactPath, binBytes, 0644); err != nil {
				return nil, fmt.Errorf("%w: write artifact failed: %v", ErrAgentReleaseImport, err)
			}

			manifestPath := filepath.Join(a.Directory, "proxy-agent", "linux", arch, fmt.Sprintf("%d", seq), "manifest.json")
			if err := atomicWriteFile(manifestPath, manifestBytes, 0644); err != nil {
				return nil, fmt.Errorf("%w: write manifest failed: %v", ErrAgentReleaseImport, err)
			}
		}

		if a.Store != nil {
			trustedKey := trustedKeys[signed.KeyID]
			entry, err := a.Store.SaveRelease(ctx, signed, trustedKey.PublicKey, actor, ip)
			if err != nil {
				return nil, fmt.Errorf("%w: store save release failed: %v", ErrAgentReleaseImport, err)
			}
			entries = append(entries, entry)
		}
	}

	a.mu.Lock()
	a.cache = cachedAgentReleases{}
	a.mu.Unlock()

	return entries, nil
}
