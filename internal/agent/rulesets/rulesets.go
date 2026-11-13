// Package rulesets keeps the node-native rule set files (v1.5.4 item 5) the
// Xray and sing-box renderers point at. The Agent downloads them itself while it
// applies a revision, so the cores never fetch anything: new content is picked
// up at the next apply, the last good copy survives a failed refresh, and a
// revision that needs a file that was never downloaded fails with
// ErrUnavailable (result code rule_set_unavailable).
package rulesets

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"proxycontrol/pkg/contracts/desiredstate"
)

// Code is the fixed apply result code of a revision whose rule sets cannot be
// provided.
const Code = "rule_set_unavailable"

// DirName is the directory below a core's runtime directory holding the files.
const DirName = "rulesets"

// AssetEnvironmentName is the variable Xray resolves ext: file names against.
const AssetEnvironmentName = "XRAY_LOCATION_ASSET"

const (
	maxRedirects = 3
	// A geoip.dat is about 20 MiB; give a slow link time to finish.
	downloadTimeout = 3 * time.Minute
)

// ErrUnavailable reports a rule set file that is neither cached nor
// downloadable.
var ErrUnavailable = errors.New("rule set file unavailable")

var fileNamePattern = regexp.MustCompile(`^[0-9a-f]{16}\.(srs|dat)$`)

// Dir is the rule set directory of a core whose runtime directory is runtime.
func Dir(runtime string) string { return filepath.Join(runtime, DirName) }

// FileName names the cached copy of address: the first 16 hex digits of its
// SHA-256 plus .srs (sing-box) or .dat (Xray).
func FileName(address string, singBox bool) string {
	sum := sha256.Sum256([]byte(address))
	extension := ".dat"
	if singBox {
		extension = ".srs"
	}
	return hex.EncodeToString(sum[:8]) + extension
}

// Need is one file a core needs.
type Need struct {
	URL            string
	SingBox        bool
	RefreshSeconds int
	// IDs are the rule set sources that use the file (logging only).
	IDs []string
}

// Needs lists the distinct files an enabled policy of state that applies to
// this node needs for core ("sing-box" or "xray"). A file shared by several rule
// sets is listed once with the shortest refresh interval.
func Needs(coreName string, state desiredstate.State) []Need {
	singBox := coreName == desiredstate.CoreSingBox
	byURL := map[string]*Need{}
	order := []string{}
	for _, policy := range state.Policies {
		if !policy.Enabled || len(policy.Scope.NodeIDs) > 0 && !contains(policy.Scope.NodeIDs, state.NodeID) {
			continue
		}
		for _, set := range policy.Match.RuleSets {
			address := set.XrayURL
			if singBox {
				address = set.SingBoxURL
			}
			need, exists := byURL[address]
			if !exists {
				need = &Need{URL: address, SingBox: singBox, RefreshSeconds: set.RefreshSeconds}
				byURL[address] = need
				order = append(order, address)
			}
			if set.RefreshSeconds < need.RefreshSeconds {
				need.RefreshSeconds = set.RefreshSeconds
			}
			need.IDs = append(need.IDs, set.ID)
		}
	}
	result := make([]Need, 0, len(order))
	for _, address := range order {
		result = append(result, *byURL[address])
	}
	return result
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// AssetEnvironment returns environment with XRAY_LOCATION_ASSET set to the rule
// set directory of runtime; an inherited value is replaced.
func AssetEnvironment(environment []string, runtime string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, AssetEnvironmentName+"=") {
			result = append(result, entry)
		}
	}
	if absolute, err := filepath.Abs(Dir(runtime)); err == nil {
		return append(result, AssetEnvironmentName+"="+absolute)
	}
	return append(result, AssetEnvironmentName+"="+Dir(runtime))
}

// Fetcher downloads rule set files. The zero value is ready to use; the fields
// are test seams.
type Fetcher struct {
	// Client replaces the HTTPS client. The default refuses redirects to hosts
	// outside the allowlist.
	Client *http.Client
	// AllowHost and AllowURL replace the contract allowlist checks.
	AllowHost func(host string) bool
	AllowURL  func(address string, singBox bool) bool
	Now       func() time.Time
	Logger    *slog.Logger
}

func (f *Fetcher) now() time.Time {
	if f != nil && f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *Fetcher) logger() *slog.Logger {
	if f != nil && f.Logger != nil {
		return f.Logger
	}
	return slog.Default()
}

func (f *Fetcher) allowHost(host string) bool {
	if f != nil && f.AllowHost != nil {
		return f.AllowHost(host)
	}
	return desiredstate.RuleSetHostAllowed(host)
}

func (f *Fetcher) allowURL(address string, singBox bool) bool {
	if f != nil && f.AllowURL != nil {
		return f.AllowURL(address, singBox)
	}
	return desiredstate.RuleSetURLAllowed(address, singBox)
}

func (f *Fetcher) client() *http.Client {
	if f != nil && f.Client != nil {
		return f.Client
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DisableCompression: true, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxResponseHeaderBytes: 16 << 10, MaxIdleConns: 2, MaxConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: transport, Timeout: downloadTimeout}
}

// Ensure makes every needed file exist below Dir(runtime). A cached file older
// than its refresh interval is downloaded again; when that fails the cached copy
// stays in use (WARN with the source id only). A missing file that cannot be
// downloaded fails the call with ErrUnavailable. Files are written atomically.
func (f *Fetcher) Ensure(ctx context.Context, runtime string, needs []Need) error {
	if len(needs) == 0 {
		return nil
	}
	directory := Dir(runtime)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	for _, need := range needs {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(directory, FileName(need.URL, need.SingBox))
		info, statErr := os.Stat(target)
		cached := statErr == nil && info.Mode().IsRegular() && info.Size() > 0
		if cached && f.now().Sub(info.ModTime()) < time.Duration(need.RefreshSeconds)*time.Second {
			continue
		}
		err := f.download(ctx, directory, target, need)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if cached {
			f.logger().Warn("rule set refresh failed; keeping the cached file", "rule_sets", strings.Join(need.IDs, ","))
			continue
		}
		return fmt.Errorf("%w: %s", ErrUnavailable, strings.Join(need.IDs, ","))
	}
	return nil
}

func (f *Fetcher) download(ctx context.Context, directory, target string, need Need) error {
	if !f.allowURL(need.URL, need.SingBox) {
		return errors.New("rule set URL not allowed")
	}
	parsed, err := url.Parse(need.URL)
	if err != nil || parsed.Scheme != "https" || !f.allowHost(parsed.Hostname()) {
		return errors.New("rule set URL not allowed")
	}
	client := *f.client()
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects || request.URL.Scheme != "https" || request.URL.User != nil || !f.allowHost(request.URL.Hostname()) {
			return errors.New("rule set redirect refused")
		}
		request.Header.Del("Cookie")
		request.Header.Del("Authorization")
		request.Header.Del("Referer")
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, need.URL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		return errors.New("rule set download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("rule set download failed")
	}
	temporary, err := os.CreateTemp(directory, ".tmp-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	head := make([]byte, 3)
	written, err := io.Copy(temporary, io.LimitReader(response.Body, desiredstate.MaxRuleSetFileBytes+1))
	if err != nil {
		temporary.Close()
		return errors.New("rule set download failed")
	}
	if written > desiredstate.MaxRuleSetFileBytes {
		temporary.Close()
		return errors.New("rule set file too large")
	}
	if _, err := temporary.ReadAt(head, 0); err != nil || !plausible(head, need.SingBox) {
		temporary.Close()
		return errors.New("rule set file is not a rule set")
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// The modification time is the fetch time the refresh interval counts from.
	if err := os.Chtimes(name, f.now(), f.now()); err != nil {
		return err
	}
	return os.Rename(name, target)
}

// plausible is a cheap shape check so an error page never replaces a good copy:
// a sing-box binary rule set starts with the magic "SRS", an Xray geodata file
// is a protobuf list whose first field is a length-delimited entry (tag 0x0a).
func plausible(head []byte, singBox bool) bool {
	if singBox {
		return string(head) == "SRS"
	}
	return len(head) > 0 && head[0] == 0x0a
}

// Cleanup deletes the files in Dir(runtime) that are not in keep (file names),
// and stale temporary files. Best effort.
func Cleanup(runtime string, keep map[string]bool) {
	entries, err := os.ReadDir(Dir(runtime))
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || keep[name] {
			continue
		}
		if fileNamePattern.MatchString(name) || strings.HasPrefix(name, ".tmp-") {
			os.Remove(filepath.Join(Dir(runtime), name))
		}
	}
}

// Keep returns the file names core needs for any of states.
func Keep(coreName string, states ...desiredstate.State) map[string]bool {
	keep := map[string]bool{}
	for _, state := range states {
		for _, need := range Needs(coreName, state) {
			keep[FileName(need.URL, need.SingBox)] = true
		}
	}
	return keep
}

// SortedNames is a deterministic listing helper for tests and logs.
func SortedNames(keep map[string]bool) []string {
	names := make([]string, 0, len(keep))
	for name := range keep {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
